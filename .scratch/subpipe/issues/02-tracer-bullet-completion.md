# 02: Tracer-Bullet Chat Completion (Unary)

**What to build:**  
Servidor HTTP en Go escuchando en `127.0.0.1:9099` que implementa `POST /v1/chat/completions` en modo unario (`stream: false`). El backend `antigravity` lanza `agy acp` como hijo persistente, abre una sesión ACP por petición, envía el historial como prompt y devuelve el texto generado como un objeto JSON estándar `chat.completion`. Se construye y prueba contra un agente ACP falso; no hay prueba en vivo con la cuenta del usuario.

**Blocked by:**  
None. (El ticket 01 —supervisor de `language_server.exe`— quedó reemplazado por la revisión (2) del ADR 0001.)

**Status:** ready-for-agent

## Acceptance criteria

- [ ] Servidor HTTP escuchando en el puerto configurable (default `127.0.0.1:9099`).
- [ ] Endpoint `POST /v1/chat/completions` acepta payload estándar con `model`, `messages` (contenido string o partes `text`) y `stream: false`.
- [ ] `model` se resuelve por prefijo con `backend.Registry` (`antigravity/gemini-3.8-pro`); prefijo desconocido → 400 con error estilo OpenAI. El handler HTTP (`internal/server`) solo conoce `backend.Backend`.
- [ ] `stream: true` → 400 explícito hasta el ticket 03.
- [ ] Lo siguiente vive en `internal/backend/antigravity` (implementa `backend.Backend`):
  - [ ] Lanza `agy acp` (comando configurable) una vez y lo reutiliza; si el hijo muere, se relanza en la siguiente petición.
  - [ ] `initialize` con `protocolVersion: 1` y sin capacidades `fs`/`terminal`.
  - [ ] `session/new {cwd, mcpServers: []}` por petición, en un directorio de trabajo propio de subpipe.
  - [ ] Selecciona el modelo con `session/set_config_option` sobre la opción de categoría `model`; modelo que el agente no ofrece → 400.
  - [ ] `session/prompt` con el historial aplanado; concatena el texto de `agent_message_chunk` hasta el `stopReason`.
  - [ ] Rechaza `session/request_permission` y responde `-32601` a cualquier otra petición del agente.
  - [ ] Si el contexto de la petición se cancela, envía `session/cancel`.
  - [ ] `Authentication required` (-32000) se reporta con un mensaje que indica iniciar sesión con `agy`.
- [ ] Empaqueta la respuesta en el schema estándar de OpenAI (`id`, `object`, `created`, `model`, `choices[0].message.content`, `finish_reason` mapeado desde `stopReason`).
- [ ] Verificable con `go test ./...` contra el agente falso, y con `curl` contra `127.0.0.1:9099/v1/chat/completions` cuando `agy` esté instalado y con sesión iniciada.

## Comments

**2026-11-10 — reescrito para `agy acp`.** La versión anterior usaba `StartCascade` / `SendUserCascadeMessage` de `language_server.exe`. Ver ADR 0001, revisión (2), y `odd/tasks/backend-modules.md` (L10).

Limitación conocida: cada petición abre una sesión nueva y envía el historial completo como texto. Reutilizar sesiones por conversación (como pi-antigravity-bridge, que envía solo el último mensaje) queda para más adelante.
