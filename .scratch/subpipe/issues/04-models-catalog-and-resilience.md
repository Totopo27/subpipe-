# 04: Models Catalog and Supervisor Resilience

**What to build:**  
Endpoint `GET /v1/models` para exponer el catálogo de modelos Gemini 3.8, monitor de vida del subproceso (vigilancia y reconexión automática si el proceso se cierra) y soporte de aislamiento de perfiles mediante directorios de configuración independientes.

**Blocked by:**  
03: Streaming SSE Engine

**Status:** ready-for-agent

## Acceptance criteria

- [ ] `GET /v1/models` devuelve lista compatible con OpenAI (`gemini-3.8-pro`, `gemini-3.8-flash`).
- [ ] `GET /healthz` retorna estado del gateway y latencia de conexión con `language_server.exe`.
- [ ] Bucle de vigilancia (watchdog): si `language_server.exe` termina o el puerto deja de responder, intenta reconectar o relanzar automáticamente.
- [ ] Flag o variable de entorno para aislar perfiles de usuario (`-gemini_dir` / `-config_dir`), permitiendo levantar múltiples instancias de `subpipe` para cuentas distintas sin interferencias.
