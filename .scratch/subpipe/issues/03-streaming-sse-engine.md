# 03: Streaming SSE Engine

**What to build:**  
Soporte completo para peticiones con `stream: true` en `POST /v1/chat/completions`. Consume el flujo de eventos de `StreamAgentStateUpdates` de `language_server.exe` y lo retransmite como un flujo `text/event-stream` SSE (`chat.completion.chunk`) con `data: [DONE]` al finalizar.

**Blocked by:**  
02: Tracer-Bullet Chat Completion (Unary)

**Status:** ready-for-agent

## Acceptance criteria

- [ ] Soporte para cabeceras SSE (`Content-Type: text/event-stream`, `Cache-Control: no-cache`).
- [ ] Conexión y lectura continua del stream de `StreamAgentStateUpdates`.
- [ ] Extracción y envío progresivo de fragmentos de texto (*chunks*) según se van generando en el modelo.
- [ ] Cierre limpio del flujo SSE emitiendo el marcador `data: [DONE]`.
- [ ] Manejo de cancelaciones de cliente (desconexión del cliente aborta el stream upstream sin colgar el gateway).
- [ ] Verificable conectando OpenCode o Aider directamente en modo streaming.
