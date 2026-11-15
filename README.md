# subpipe

Micro-gateway local que traduce peticiones HTTP compatibles con OpenAI (`/v1/chat/completions`) hacia el cliente oficial instalado de cada proveedor. Cada proveedor es un **backend** independiente (ver [ADR 0002](docs/adr/0002-vendor-backend-modules.md)); el primero es Google Antigravity, al que subpipe habla lanzando `agy acp` (Agent Client Protocol: JSON-RPC sobre stdin/stdout del proceso hijo, ver [ADR 0001](docs/adr/0001-persistent-subprocess-go-bridge.md)).

> **Aviso:** las FAQ de Antigravity indican que envolver `agy` en aplicaciones de terceros no está soportado en planes de consumo. Usar subpipe con una cuenta personal es un riesgo que asume quien lo usa.

Diseñado para utilizar Gemini 3.8 / Claude desde **OpenCode**, **Claude Code**, **Cursor** u otros agentes, eliminando de raíz las suspensiones `403 TOS_VIOLATION` provocadas por la emulación de cabeceras en proxies HTTP tradicionales.

---

## Por qué existe

Los proxies inversos convencionales intentan engañar a `cloudcode-pa.googleapis.com` emulando las llamadas de red del cliente oficial. Esta estrategia falla de forma recurrente por cuatro motivos:

1. **Desfase de Changelist (`cl`):** Antigravity actualiza con frecuencia su versión (ej. `2.29.0`) exigiendo el changelist exacto en el `User-Agent`. Si el proxy no lo tiene compilado, emite cabeceras truncadas detectables por el WAF.
2. **Filtraciones de nombres de agentes:** Las firmas de OpenCode o Claude Code viajan en los prompts en texto plano.
3. **Session Hopping:** Balancear turnos de una misma tarea entre cuentas distintas destruye la afinidad y delata el uso de proxy compartido.
4. **Sockets efímeros:** Abrir y cerrar conexiones TCP en cada turno en vez de mantener un pool HTTP/1.1 persistente.

`subpipe` resuelve esto cambiando el transporte: **no realiza peticiones web externas**. Delega toda la comunicación con Google al binario oficial instalado en la máquina, el cual gestiona sus propios certificados TLS legítimos, cabeceras y changelists válidos.

---

## Arquitectura

```
[ OpenCode / Claude Code / Cursor ]
                │
                │  HTTP POST /v1/chat/completions (OpenAI JSON)
                ▼
      ┌──────────────────┐
      │     subpipe      │  (127.0.0.1:9099)
      │  Traductor local │
      └─────────┬────────┘
                │
                │  backend antigravity: ACP v1
                │  (JSON-RPC por stdin/stdout)
                ▼
   ┌────────────────────────┐
   │        agy acp         │  (Subproceso oficial Google)
   └────────────┬───────────┘
                │
                │  HTTPS nativo oficial (TLS + cl de Google)
                ▼
 [ cloudcode-pa.googleapis.com ]
```

---

## Requisitos previos

1. **Go 1.22+** (o superior).
2. **Antigravity CLI (`agy`)** instalado en el sistema y con sesión iniciada una vez:
   - **Instalación oficial:** Consulta [antigravity.google/download](https://antigravity.google/download#antigravity-cli).
     - **Windows (PowerShell):** `irm https://antigravity.google/cli/install.ps1 | iex`
     - **macOS / Linux:** `curl -fsSL https://antigravity.google/cli/install.sh | bash`
   - **Autenticación:** Iniciar sesión con la cuenta de Google ejecutando `agy` interactivo o `agy acp --login` (o teniendo la app de escritorio de Antigravity iniciada). `subpipe` nunca toca credenciales.

---

## Arranque rápido

### 1. Ejecución directa
```bash
go run ./cmd/subpipe
```
Por defecto, `subpipe` escucha en `http://127.0.0.1:9099/v1` y localiza automáticamente `agy` en el `PATH` del sistema.

### 2. Compilar el binario ejecutable
```bash
# Linux / macOS
go build -o subpipe ./cmd/subpipe
./subpipe

# Windows PowerShell
go build -o subpipe.exe ./cmd/subpipe
.\subpipe.exe
```

---

## Parámetros de configuración

| Flag | Variable de Entorno | Default | Descripción |
| --- | --- | --- | --- |
| `-addr` | — | `127.0.0.1:9099` | Dirección y puerto de escucha HTTP del gateway. |
| `-agy` | — | `agy` | Ruta al binario ejecutable de `agy` (admite rutas con espacios). |
| `-workdir` | — | `<temp>/subpipe-antigravity` | Directorio de trabajo local para las sesiones de `agy`. |
| `-config-dir` | `GEMINI_DIR` o `AGY_CONFIG_DIR` | (vacío / default del sistema) | Directorio de perfil/configuración para aislar credenciales en entornos multi-cuenta. |
| `-watchdog` | — | `true` | Reinicia automáticamente el subproceso hijo si termina inesperadamente. |

Ejemplo con parámetros personalizados:
```bash
# Ejemplo en Linux / macOS
./subpipe -addr 127.0.0.1:9100 -workdir /tmp/subpipe-work -config-dir ~/.antigravity/perfil2

# Ejemplo en Windows
.\subpipe.exe -addr 127.0.0.1:9100 -config-dir "C:\Perfiles\Cuenta2"
```

---

## Endpoints disponibles

`subpipe` implementa un subconjunto de la API de OpenAI compatible con chat de texto, no la especificación completa:

* **`POST /v1/chat/completions`**: Modo unario (`stream: false`) y modo streaming SSE (`stream: true`).
* **`GET /v1/models`**: Lista el catálogo dinámico de modelos prefijados (`antigravity/gemini-3.8-flash`, etc.).
* **`GET /healthz`**: Reporte de salud del gateway y del subproceso (`alive`, `initialized`, `authenticated`).

### Alcance de compatibilidad

- Chat admite `model`, `messages` y `stream`; `stream` omitido equivale a `false`.
- Los roles admitidos son `system`, `developer` (normalizado a `system`), `user` y `assistant`. El contenido puede ser texto o una lista de partes de tipo `text`.
- No se admiten herramientas del cliente ni el ciclo `tool_calls` → resultado con rol `tool`. La presencia de `tools` o `tool_choice` devuelve HTTP 400 con `error.type: "invalid_request_error"` y `error.param` indicando el campo, antes de invocar al Backend o abrir un stream SSE. Esto incluye `tools: []`, valores `null` y `tool_choice: "none"`; omite ambos campos para usar chat de texto. Si aparecen ambos, se informa primero `tools`.
- El rol `tool` y las partes de contenido no textual también se rechazan. Otros campos desconocidos continúan ignorándose; su aceptación no implica que se apliquen sus opciones.

Ejemplo de error para una petición que incluye `tools`:

```json
{"error":{"message":"tools is not supported","type":"invalid_request_error","param":"tools","code":null}}
```

Consulta [las consideraciones para futuras herramientas](docs/tool-integration-considerations.md) para lo que queda fuera de este alcance.

### Pruebas rápidas con `curl`:

```bash
# 1. Comprobar salud
curl http://127.0.0.1:9099/healthz

# 2. Consultar catálogo de modelos disponibles
curl http://127.0.0.1:9099/v1/models

# 3. Chat unario estándar
curl http://127.0.0.1:9099/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"antigravity/gemini-3.8-flash","messages":[{"role":"user","content":"Hola"}]}'

# 4. Streaming SSE en tiempo real
curl -N http://127.0.0.1:9099/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"antigravity/gemini-3.8-flash","stream":true,"messages":[{"role":"user","content":"Hola"}]}'
```

---

## Modos de integración

### 1. Conexión directa (Recomendado)
Apunta un cliente compatible con OpenAI al puerto local de `subpipe` usando únicamente el subconjunto de chat de texto descrito arriba. Las integraciones siguientes no habilitan herramientas del cliente:

* **OpenCode:**
  ```json
  {
    "provider": {
      "name": "subpipe",
      "baseUrl": "http://127.0.0.1:9099/v1",
      "model": "antigravity/gemini-3.8-flash"
    }
  }
  ```
* **Aider:**
  ```bash
  aider --openai-api-base http://127.0.0.1:9099/v1 --model openai/antigravity/gemini-3.8-flash
  ```
* **Cursor / Windsurf:**
  En los ajustes de proveedor OpenAI, sobrescribir *Base URL* con `http://127.0.0.1:9099/v1`.

### 2. Como Upstream de CPAM / CLIProxyAPI
Si utilizas CPAM y quieres conservar su panel web, métricas y gestión de cuotas:

1. Mantén `subpipe` corriendo en `127.0.0.1:9099`.
2. En el `config.yaml` de CPAM, añade a `subpipe` como un canal OpenAI compatible:
   ```yaml
   openai-compatibility:
     - name: "subpipe-antigravity"
       base-url: "http://host.docker.internal:9099/v1"
       api-key: "local-dev"
       models:
         - name: "antigravity/gemini-3.8-pro"
         - name: "antigravity/gemini-3.8-flash"
   ```
CPAM gestionará la interfaz, pero delegará la salida a `subpipe`, evitando la conexión directa del proxy a Google.

---

## Manejo multicuenta con aislamiento de perfiles

Para operar con varias cuentas sin que se mezclen sesiones ni se disparen bloqueos:

* **Perfiles independientes:** Configurar variables de entorno (`GEMINI_DIR` / `AGY_CONFIG_DIR`) o el flag `-config-dir` por cada cuenta (ej. `~/antigravity_profiles/cuenta_1`, `~/antigravity_profiles/cuenta_2`).
* **Multiplexores de terminal (ej. Herdr):** Asignar un perfil exclusivo a cada espacio de trabajo. Cada tarea mantiene afinidad física de sesión con su cuenta asignada.
* **Git Worktrees:** Para trabajar concurrentemente en el mismo repositorio desde dos cuentas distintas sin colisiones en disco.

---

## Documentación técnica detallada

Para un análisis a fondo de la evidencia forense, matrices comparativas y el plan de implementación, consulta el informe completo:
* [informe_propuesta_puente_hibrido_antigravity.md](informe_propuesta_puente_hibrido_antigravity.md)
