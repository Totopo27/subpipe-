# Spec: subpipe Micro-Gateway

## Problem Statement

Desarrolladores que utilizan agentes de código (OpenCode, Claude Code, Cursor, Aider) y conectan sus tareas a modelos de Gemini 3.8 a través de proxies inversos tradicionales (como CPAMC o CLIProxyAPI) sufren suspensiones frecuentes con errores `403 TOS_VIOLATION`. 

Estas suspensiones son producto de la detección perimetral de Google, la cual identifica que el cliente HTTP es un proxy sintético debido a:
1. Desfase del changelist de Piper (`cl`) en la cabecera `User-Agent`.
2. Fugas de nombres de agentes en texto plano dentro de los mensajes del sistema.
3. Pérdida de afinidad de sesión al rotar cuentas entre turnos de una misma tarea (*session hopping*).
4. Negociaciones TLS efímeras en sockets TCP de corta duración en lugar de mantener conexiones persistentes con *keep-alive*.

## Solution

`subpipe` es un micro-gateway local desarrollado en Go que expone un endpoint HTTP compatible con la API de OpenAI (`/v1/chat/completions` y `/v1/models`). En lugar de realizar peticiones de red hacia los servidores de Google, `subpipe` se conecta a la instancia oficial de `language_server.exe` de Google Antigravity instalada en la máquina.

Toda la comunicación externa hacia `cloudcode-pa.googleapis.com` la realiza exclusivamente el binario oficial de Google, el cual conserva certificados TLS legítimos, cabeceras oficiales, changelist exacto y conexiones HTTP/1.1 persistentes.

## User Stories

1. As an OpenCode user, I want to set `subpipe` as an OpenAI provider base URL (`http://127.0.0.1:9099/v1`), so that my agent requests execute through Google's official client without WAF bans.
2. As a Cursor user, I want to configure the OpenAI API base override to point to `subpipe`, so that I can use Gemini 3.8 models within the editor.
3. As a developer using Aider, I want to execute chat completions with streaming SSE output, so that I see token generation in real time on my terminal.
4. As a CPAMC user, I want to configure `subpipe` as an upstream provider in `config.yaml`, so that I retain CPAMC's quota tracking and web dashboard while delegating network traffic safely.
5. As a developer working with multiple Google accounts, I want each workspace session to maintain strict affinity to a single subpipe profile, so that turn-by-turn prompts are never scattered across multiple accounts.
6. As a system administrator, I want `subpipe` to automatically discover an already-running `language_server.exe` process, so that it connects with zero startup latency without spawning duplicate processes.
7. As a developer starting clean, I want `subpipe` to launch and supervise `language_server.exe` if none is active, so that the service runs autonomously without manual IDE intervention.
8. As a client agent, I want `subpipe` to restart the language server if the process dies unexpectedly, so that long-running coding sessions do not crash permanently.
9. As an agent querying `/v1/models`, I want a valid list of Gemini 3.8 models, so that client model-selectors populate correctly.

## Implementation Decisions

### 0. Vendor Backend Modules (ADR 0002)
- Todo lo específico de un proveedor vive en `internal/backend/<name>` e implementa `backend.Backend`. El núcleo (ingress HTTP, formato OpenAI, SSE, enrutado) no conoce proveedores.
- Los ids de modelo son `<backend>/<model>` (`antigravity/gemini-3.8-pro`); el `Registry` resuelve el prefijo.
- Backend actual: `antigravity`. Previstos (no construidos): Claude Code CLI, Codex CLI — cada uno con su cliente oficial local.
- Las secciones 1, 3 y 4 describen el backend `antigravity`.

### 1. Architectural Seam and Communication Protocol (backend `antigravity`)
- `subpipe` se comunica con `language_server.exe` a través de su servidor RPC local (ConnectRPC / HTTP JSON) en `127.0.0.1:<httpPort>`, autenticado mediante la cabecera `x-codeium-csrf-token`.
- Los datos de conexión (puerto dinámico y CSRF token) se obtienen dinámicamente desde el archivo de descubrimiento oficial (`%USERPROFILE%\.gemini\antigravity\daemon\hub_<port>.json`) o se definen al lanzar el proceso en modo supervisado.

### 2. Ingress HTTP Module
- Expone un servidor HTTP estándar en Go en el puerto `127.0.0.1:9099`.
- Endpoints soportados:
  - `POST /v1/chat/completions`: Soporta payloads estándar de OpenAI con flag `stream: true` (SSE) y `stream: false` (JSON completo).
  - `GET /v1/models`: Retorna la unión de catálogos de todos los backends registrados, con prefijo (`antigravity/gemini-3.8-pro`, `antigravity/gemini-3.8-flash`).
  - `GET /healthz`: Chequeo de salud del gateway y estado de conexión con el subproceso.

### 3. Translation and Cascade Protocol Engine
- Mapea el historial de mensajes de OpenAI (`system`, `user`, `assistant`) hacia los llamados RPC de Antigravity:
  - Creación de trayectoria mediante `StartCascade` (usando un UUID determinista o aleatorio por sesión).
  - Envío de contexto y mensaje mediante `SendUserCascadeMessage` (`items: [{ text: content }]`).
  - Consumo de flujo de actualización de estado del agente mediante `StreamAgentStateUpdates`.
- Traduce los eventos recibidos hacia bloques SSE formateados bajo la especificación OpenAI:
  `data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."}}]}` seguido de `data: [DONE]`.

### 4. Process Supervisor and Lifecycle
- Implementa una interfaz de gestión del ciclo de vida (`Supervisor`):
  - Modo Adjunto (*Attach*): Detecta si existe un demonio activo en el directorio de runtime de Antigravity.
  - Modo Huésped (*Spawn*): Si no hay demonio activo, ejecuta `language_server.exe --standalone` como proceso hijo en segundo plano.
  - Mecanismo de *heartbeat* y reconexión automática si el proceso hijo finaliza.

## Testing Decisions

### Good Test Criteria
- Las pruebas deben validar el comportamiento observable a través de la interfaz HTTP (`/v1/chat/completions` y `/v1/models`), no el estado interno privado del traductor.
- Pruebas de integración sobre un mock del servidor ConnectRPC local para verificar la traducción exacta de mensajes y streaming SSE sin depender de credenciales vivas de Google.
- Pruebas unitarias de parsing del archivo `hub_*.json` y control del supervisor de procesos.

### Modules to Test
- `internal/server`: Validación de contratos HTTP y streaming chunked/SSE, contra un backend falso.
- `internal/backend`: Contrato y enrutado por prefijo (`Registry`).
- `internal/backend/antigravity`: Descubrimiento, CSRF, ciclo de vida del proceso (supervisor) y traducción a Cascade RPC contra un servidor ConnectRPC simulado.

## Out of Scope

- Emulación o modificación directa de cabeceras de red hacia Google (delegadas 100% al binario oficial).
- Rotación aleatoria de cuentas en una misma sesión (prohibida expresamente por política de afinidad).
- Interfaz gráfica web (se consume como servicio CLI en segundo plano o como upstream de CPAMC).

## Further Notes

- El binario oficial identificado en Windows es `C:\Users\ttoto\AppData\Local\Programs\antigravity\resources\bin\language_server.exe`.
- El changelist oficial verificado en este entorno es `1013060670` (versión `2.29.0`).
