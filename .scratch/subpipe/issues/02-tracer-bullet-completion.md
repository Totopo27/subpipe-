# 02: Tracer-Bullet Chat Completion (Unary)

**What to build:**  
Servidor HTTP en Go escuchando en `127.0.0.1:9099` que implementa `POST /v1/chat/completions` en modo unario (`stream: false`). Toma los mensajes de OpenAI, inicia una trayectoria con `StartCascade` y despacha el mensaje con `SendUserCascadeMessage` en `language_server.exe`, devolviendo un objeto JSON estándar `chat.completion`.

**Blocked by:**  
01: Discovery and Process Supervisor

**Status:** ready-for-agent

## Acceptance criteria

- [ ] Servidor HTTP escuchando en el puerto configurable (default `127.0.0.1:9099`).
- [ ] Endpoint `POST /v1/chat/completions` acepta payload estándar con `model`, `messages` y `stream: false`.
- [ ] `model` se resuelve por prefijo con `backend.Registry` (`antigravity/gemini-3.8-pro`); prefijo desconocido → 400 con error estilo OpenAI. El handler HTTP (`internal/server`) solo conoce `backend.Backend`.
- [ ] Lo siguiente vive en `internal/backend/antigravity` (implementa `backend.Backend`):
- [ ] Genera un `cascade_id` (UUIDv4) para la conversación.
- [ ] Realiza llamada ConnectRPC a `StartCascade` con `source: 1` (`CASCADE_CLIENT`) y `trajectory_type: 4` (`CASCADE`).
- [ ] Realiza llamada ConnectRPC a `SendUserCascadeMessage` con el contenido del prompt.
- [ ] Empaqueta la respuesta generada en el schema estándar de OpenAI (`id`, `created`, `model`, `choices[0].message.content`).
- [ ] Verificable ejecutando una petición `curl` contra `127.0.0.1:9099/v1/chat/completions` recibiendo respuesta exitosa.
