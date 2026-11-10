# subpipe

Micro-gateway local que traduce peticiones HTTP compatibles con OpenAI (`/v1/chat/completions`) hacia el cliente oficial instalado de cada proveedor. Cada proveedor es un **backend** independiente (ver [ADR 0002](docs/adr/0002-vendor-backend-modules.md)); el primero es Google Antigravity, al que subpipe habla por su RPC local (ConnectRPC/JSON sobre `http://127.0.0.1:<httpPort>`, autenticado con `x-codeium-csrf-token`) en `language_server.exe`.

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
                │  backend antigravity: ConnectRPC/JSON sobre
                │  http://127.0.0.1:<httpPort> (x-codeium-csrf-token)
                ▼
   ┌────────────────────────┐
   │  language_server.exe   │  (Subproceso oficial Google)
   └────────────┬───────────┘
                │
                │  HTTPS nativo oficial (TLS + cl de Google)
                ▼
 [ cloudcode-pa.googleapis.com ]
```

---

## Modos de integración

### 1. Conexión directa (Recomendado)
Apunta tu cliente de desarrollo directamente al puerto de `subpipe`:

* **OpenCode:**
  ```json
  {
    "provider": {
      "name": "subpipe",
      "baseUrl": "http://127.0.0.1:9099/v1",
      "model": "antigravity/gemini-3.8-pro"
    }
  }
  ```
* **Aider:**
  ```bash
  aider --openai-api-base http://127.0.0.1:9099/v1 --model openai/antigravity/gemini-3.8-pro
  ```
* **Cursor / Windsurf:**
  En ajustes de proveedor OpenAI, sobrescribir *Base URL* con `http://127.0.0.1:9099/v1`.

### 2. Como Upstream de CPAM / CLIProxyAPI
Si utilizas CPAM y quieres conservar su panel web, métricas y gestión de cuotas:

1. Mantén `subpipe` corriendo en Windows en `127.0.0.1:9099`.
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

* **Perfiles independientes:** Configurar variables de entorno (`USERPROFILE` o `-config_dir`) por cada cuenta (ej. `~/antigravity_profiles/cuenta_1`, `~/antigravity_profiles/cuenta_2`).
* **Multiplexores de terminal (ej. Herdr):** Asignar un perfil exclusivo a cada espacio de trabajo. Cada tarea mantiene afinidad física de sesión con su cuenta asignada.
* **Git Worktrees:** Para trabajar concurrentemente en el mismo repositorio desde dos cuentas distintas sin colisiones en disco.

---

## Documentación detallada

Para un análisis a fondo de la evidencia forense, matrices comparativas y el plan de implementación, consulta el informe completo:
* [informe_propuesta_puente_hibrido_antigravity.md](file:///D:/DocumentosDiscoD/subpipe/informe_propuesta_puente_hibrido_antigravity.md)
