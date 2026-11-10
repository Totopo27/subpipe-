# 03: Streaming SSE Engine

**What to build:**  
Soporte completo para peticiones con `stream: true` en `POST /v1/chat/completions`. Consume las notificaciones ACP `session/update` (`agent_message_chunk`) de `agy acp` y las retransmite como un flujo `text/event-stream` SSE (`chat.completion.chunk`) con `data: [DONE]` al finalizar.

**Blocked by:**  
02: Tracer-Bullet Chat Completion (Unary)

**Status:** ready-for-agent

## Acceptance criteria

- [ ] El contrato `backend.Backend` gana un método de streaming neutral; el formateo SSE vive en `internal/server` y la lectura de `session/update` en `internal/backend/antigravity`.
- [ ] Soporte para cabeceras SSE (`Content-Type: text/event-stream`, `Cache-Control: no-cache`).
- [ ] Reenvío de cada `agent_message_chunk` en cuanto llega, sin esperar al `stopReason`.
- [ ] Extracción y envío progresivo de fragmentos de texto (*chunks*) según se van generando en el modelo.
- [ ] Cierre limpio del flujo SSE emitiendo el marcador `data: [DONE]`.
- [ ] Manejo de cancelaciones de cliente (desconexión del cliente envía `session/cancel` sin colgar el gateway).
- [ ] Verificable conectando OpenCode o Aider directamente en modo streaming.
