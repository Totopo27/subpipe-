# External Repositories Reference (Google Gemini Ecosystem)

Este documento registra los repositorios de referencia analizados para `subpipe`, su estatus oficial, nivel de confianza y aplicabilidad técnica para nuestras siguientes sesiones.

---

## 1. `google-gemini/gemini-cli`
- **URL**: [https://github.com/google-gemini/gemini-cli](https://github.com/google-gemini/gemini-cli)
- **Organización**: `google-gemini` (organización oficial de Google AI / Gemini API).
- **Licencia**: Apache-2.0.
- **¿Es oficial y de fiar?**: **SÍ, 100% oficial de Google**. Tiene +108k estrellas, releases automatizados en npm (`@google/gemini-cli`) y código firmado por ingenieros de Google.
- **Naturaleza del proyecto**:
  - CLI/TUI interactivo para terminal desarrollado en TypeScript/Node.js (usando React Ink).
  - Diseñado para **Gemini Code Assist Enterprise/Standard**.
  - **Importante (migración oficial junio 2026)**: El README aclara que el acceso individual gratuito y planes Google AI Pro/Ultra migraron oficialmente a **Antigravity CLI** (`agy`).
- **Qué pistas nos da para `subpipe`**:
  - En `packages/core/src/code_assist/server.ts` se confirma el endpoint oficial de Code Assist: `https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent` con `alt=sse`.
  - Muestra cómo manejan la telemetría, `traceId`, `trajectoryId` (que mapea a `sessionId`), y la estructura de los chunks SSE.
  - Implementa el parser de streaming SSE (`data: ...`) y cómo empaquetan las respuestas de Vertex/CodeAssist a objetos compatibles.

---

## 2. `google-gemini/gemini-api-cli`
- **URL**: [https://github.com/google-gemini/gemini-api-cli](https://github.com/google-gemini/gemini-api-cli)
- **Organización**: `google-gemini`.
- **Licencia**: Apache-2.0.
- **¿Es oficial y de fiar?**: **SÍ, 100% oficial de Google**. Es el CLI oficial stateless de la Gemini API / Interactions API.
- **Naturaleza del proyecto**:
  - Binario único compilado en **Go** (Go 1.26+) utilizando Speakeasy como generador de SDK subyacente.
  - Diseñado específicamente para que agentes de código (Codex, Claude Code, Antigravity, OpenClaw) ejecuten comandos sin estado en la terminal.
  - Apunta directamente a `https://generativelanguage.googleapis.com` mediante API keys estándar de Google AI Studio (`GEMINI_API_KEY`).
- **Qué pistas nos da para `subpipe`**:
  - Al estar escrito en **Go**, es una referencia directa para nuestra arquitectura:
    - Manejo de transporte HTTP acotado en Go (`newPhaseBoundedTransport` en `internal/client/client.go`).
    - Guardas de redirección segura entre orígenes (`SameOriginRedirect`).
    - Manejo idiomático de streaming en Go con cancelación de contextos (`context.Context`).
    - Comportamiento ante timeouts y retries exponenciales en Go.
  - Ojo: este CLI usa API keys hacia la API pública (`generativelanguage.googleapis.com`), mientras que `subpipe` hace puente hacia el subproceso `language_server.exe` de Antigravity (ConnectRPC sobre HTTP local). La referencia de `gemini-api-cli` es para patrones de cliente HTTP, streaming y diseño CLI en Go, no para el protocolo interno Cascade.

---

## Resumen comparativo

| Característica | `gemini-cli` | `gemini-api-cli` | `subpipe` (nuestro proyecto) |
| :--- | :--- | :--- | :--- |
| **Lenguaje** | TypeScript / Node.js | **Go** | **Go** |
| **Destino de red** | `cloudcode-pa.googleapis.com` (Code Assist) | `generativelanguage.googleapis.com` (Gemini API) | `127.0.0.1:<httpPort>` (`language_server.exe`) |
| **Autenticación** | OAuth2 / Google Cloud Project | API Key / Access Token | `x-codeium-csrf-token` local |
| **Utilidad para subpipe** | Formato de eventos SSE, chunks y telemetría CodeAssist | Patrones de cliente HTTP en Go, manejo de retries y streaming | Micro-gateway local compatible con OpenAI API |
