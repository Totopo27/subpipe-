# 04: Models Catalog and Supervisor Resilience

**What to build:**  
Endpoint `GET /v1/models` para exponer el catálogo de modelos Gemini 3.8, monitor de vida del subproceso (vigilancia y reconexión automática si el proceso se cierra) y soporte de aislamiento de perfiles mediante directorios de configuración independientes.

**Blocked by:**  
03: Streaming SSE Engine

**Status:** ready-for-agent

## Acceptance criteria

- [ ] `GET /v1/models` devuelve la unión prefijada de `backend.Registry.Models` (`antigravity/gemini-3.8-pro`, `antigravity/gemini-3.8-flash`).
- [ ] El contrato `backend.Backend` gana `Health`; watchdog y flags de perfil son internos de cada backend.
- [ ] Los modelos del backend `antigravity` salen de la opción de configuración `model` que `agy acp` devuelve en `session/new`.
- [ ] `GET /healthz` retorna estado del gateway y del proceso `agy acp` (vivo, inicializado, autenticado).
- [ ] Bucle de vigilancia (watchdog): si `agy acp` termina o deja de responder, lo relanza automáticamente.
- [ ] Flag o variable de entorno para aislar perfiles de usuario (`-gemini_dir` / `-config_dir`), permitiendo levantar múltiples instancias de `subpipe` para cuentas distintas sin interferencias.
