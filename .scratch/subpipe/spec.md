# Spec: subpipe Micro-Gateway

## Problem Statement

Desarrolladores que utilizan agentes de código (OpenCode, Claude Code, Cursor, Aider) y conectan sus tareas a modelos de Gemini 3.8 a través de proxies inversos tradicionales (como CPAMC o CLIProxyAPI) sufren suspensiones frecuentes con errores `403 TOS_VIOLATION`. 

Estas suspensiones son producto de la detección perimetral de Google, la cual identifica que el cliente HTTP es un proxy sintético debido a:
1. Desfase del changelist de Piper (`cl`) en la cabecera `User-Agent`.
2. Fugas de nombres de agentes en texto plano dentro de los mensajes del sistema.
3. Pérdida de afinidad de sesión al rotar cuentas entre turnos de una misma tarea (*session hopping*).
4. Negociaciones TLS efímeras en sockets TCP de corta duración en lugar de mantener conexiones persistentes con *keep-alive*.

## Solution

`subpipe` es un micro-gateway local desarrollado en Go que expone un endpoint HTTP compatible con la API de OpenAI (`/v1/chat/completions` y `/v1/models`). En lugar de realizar peticiones de red hacia los servidores de Google, `subpipe` lanza el cliente oficial de Google Antigravity instalado en la máquina (`agy acp`) y le habla por el Agent Client Protocol.

Toda la comunicación externa hacia `cloudcode-pa.googleapis.com` la realiza exclusivamente el binario oficial de Google, el cual conserva certificados TLS legítimos, cabeceras oficiales, changelist exacto y conexiones HTTP/1.1 persistentes.

## User Stories

1. As an OpenCode user, I want to set `subpipe` as an OpenAI provider base URL (`http://127.0.0.1:9099/v1`), so that my agent requests execute through Google's official client without WAF bans.
2. As a Cursor user, I want to configure the OpenAI API base override to point to `subpipe`, so that I can use Gemini 3.8 models within the editor.
3. As a developer using Aider, I want to execute chat completions with streaming SSE output, so that I see token generation in real time on my terminal.
4. As a CPAMC user, I want to configure `subpipe` as an upstream provider in `config.yaml`, so that I retain CPAMC's quota tracking and web dashboard while delegating network traffic safely.
5. As a developer working with multiple Google accounts, I want each workspace session to maintain strict affinity to a single subpipe profile, so that turn-by-turn prompts are never scattered across multiple accounts.
6. (Dropped 2026-11-10: attaching to a running `language_server.exe` does not apply to `agy acp`, which subpipe always spawns itself. See ADR 0001.)
7. As a developer starting clean, I want `subpipe` to launch and supervise `agy acp`, so that the service runs autonomously without manual IDE intervention.
8. As a client agent, I want `subpipe` to restart `agy acp` if the process dies unexpectedly, so that long-running coding sessions do not crash permanently.
9. As an agent querying `/v1/models`, I want a valid list of Gemini 3.8 models, so that client model-selectors populate correctly.

## Implementation Decisions

### 0. Vendor Backend Modules (ADR 0002)
- Todo lo específico de un proveedor vive en `internal/backend/<name>` e implementa `backend.Backend`. El núcleo (ingress HTTP, formato OpenAI, SSE, enrutado) no conoce proveedores.
- Los ids de modelo son `<backend>/<model>` (`antigravity/gemini-3.8-pro`); el `Registry` resuelve el prefijo.
- Backend actual: `antigravity`. Previstos (no construidos): Claude Code CLI, Codex CLI — cada uno con su cliente oficial local.
- Las secciones 1, 3 y 4 describen el backend `antigravity`.

### 1. Architectural Seam and Communication Protocol (backend `antigravity`)
- `subpipe` lanza `agy acp` como proceso hijo persistente y le habla por ACP v1: JSON-RPC 2.0 delimitado por saltos de línea sobre `stdin`/`stdout` (`stderr` solo es log). Ver ADR 0001, revisión (2).
- No hay puerto, archivo de descubrimiento ni token local. La autenticación con Google la hace `agy` con las credenciales cacheadas por un `agy` interactivo o `agy acp --login`; subpipe nunca toca credenciales.

### 2. Ingress HTTP Module
- Expone un servidor HTTP estándar en Go en el puerto `127.0.0.1:9099`.
- Endpoints soportados:
  - `POST /v1/chat/completions`: Soporta payloads estándar de OpenAI con flag `stream: true` (SSE) y `stream: false` (JSON completo).
  - `GET /v1/models`: Retorna la unión de catálogos de todos los backends registrados, con prefijo (`antigravity/gemini-3.8-pro`, `antigravity/gemini-3.8-flash`).
  - `GET /healthz`: Chequeo de salud del gateway y estado de conexión con el subproceso.

### 3. Translation and ACP Engine (backend `antigravity`)
- Mapea el historial de mensajes de OpenAI (`system`, `user`, `assistant`) hacia ACP:
  - `initialize` una vez por proceso (versión de protocolo 1, sin capacidades `fs` ni `terminal`).
  - `session/new {cwd, mcpServers: []}` por conversación y `session/set_config_option` para el modelo.
  - `session/prompt` con el historial aplanado en bloques de texto.
  - Consumo de las notificaciones `session/update` (`agent_message_chunk`) hasta que `session/prompt` responde con `stopReason`. Las peticiones `session/request_permission` se rechazan; la cancelación envía `session/cancel`.
- Traduce los fragmentos recibidos hacia bloques SSE formateados bajo la especificación OpenAI:
  `data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."}}]}` seguido de `data: [DONE]`.

### 4. Process Supervisor and Lifecycle
- subpipe es dueño del proceso `agy acp`: lo lanza, lo cierra (cerrando `stdin` y matándolo tras un periodo de gracia) y lo relanza si muere (ticket 04).
- Ya no hay modo adjunto (*attach*) ni `language_server.exe --standalone`; ver ADR 0001, revisión (2).

## Testing Decisions

### Good Test Criteria
- Las pruebas deben validar el comportamiento observable a través de la interfaz HTTP (`/v1/chat/completions` y `/v1/models`), no el estado interno privado del traductor.
- Pruebas de integración contra un agente ACP falso (el binario de test relanzado como hijo) para verificar la traducción exacta de mensajes y streaming SSE sin depender de credenciales vivas de Google.

### Modules to Test
- `internal/server`: Validación de contratos HTTP y streaming chunked/SSE, contra un backend falso.
- `internal/backend`: Contrato y enrutado por prefijo (`Registry`).
- `internal/backend/antigravity`: Ciclo de vida de `agy acp` y traducción a ACP contra un agente ACP falso.

## Out of Scope

- Emulación o modificación directa de cabeceras de red hacia Google (delegadas 100% al binario oficial).
- Rotación aleatoria de cuentas en una misma sesión (prohibida expresamente por política de afinidad).
- Interfaz gráfica web (se consume como servicio CLI en segundo plano o como upstream de CPAMC).

## Further Notes

- El backend usa `agy` desde `PATH` (configurable). En esta máquina `agy` todavía no está instalado; el ticket 02 se construye contra un agente falso.
- Histórico: el binario `language_server.exe` (versión `2.29.0`, changelist `1013060670`) era el transporte anterior.
