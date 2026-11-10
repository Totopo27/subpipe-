# 01: Discovery and Process Supervisor

**What to build:**  
Módulo en Go que detecta la instancia activa de `language_server.exe` leyendo el archivo de descubrimiento oficial (`%USERPROFILE%\.gemini\antigravity\daemon\hub_<port>.json`) o levanta el subproceso oficial si no hay ninguno corriendo. Extrae el puerto dinámico de escucha HTTP y el `csrfToken` para autenticar peticiones locales.

**Blocked by:**  
None (can start immediately).

**Status:** resolved

## Acceptance criteria

- [x] Localiza automáticamente la ruta del binario oficial `language_server.exe` en `%LOCALAPPDATA%\Programs\antigravity\resources\bin\`.
- [x] Lee y parsea el archivo `hub_*.json` más reciente en `.gemini/antigravity/daemon/` para obtener PID, `httpPort` y `csrfToken`.
- [x] Valida conectividad básica contra `http://127.0.0.1:<httpPort>/exa.language_server_pb.LanguageServerService/GetServerConfiguration` usando la cabecera `x-codeium-csrf-token`.
- [x] Si no hay proceso activo, es capaz de lanzar `language_server.exe --standalone` en segundo plano y capturar su handshake.
- [x] Pruebas unitarias de parsing y verificación de salud del proceso.

## Comments

**2026-11-09 — implementación (`internal/supervisor`)**

- Hay varios `hub_*.json` huérfanos en el directorio daemon (procesos muertos). "El más reciente" no basta: `Connect` recorre los hubs de más nuevo a más viejo y se queda con el primero que responde 200 a `GetServerConfiguration`.
- Token inválido → HTTP 401; puerto muerto → error de conexión. Ambos se tratan como hub no sano.
- Spawn: subpipe elige puerto libre y genera el CSRF token, y los pasa con `--http_server_port` / `--csrf_token`. El "handshake" es esperar a que el ping responda 200. Flags copiados del lanzamiento real de Antigravity.exe (`--standalone --override_ide_name antigravity --subclient_type hub ...`) sin `--host_bridge_*`, más `--exit_on_stdin_close` para que el hijo muera si subpipe cierra stdin.
- Pruebas: el binario de test hace de `language_server.exe` falso (`SUBPIPE_FAKE_LS=1`) para probar spawn/stop de extremo a extremo. `TestLiveAttach` (opt-in `SUBPIPE_LIVE=1`) verificó attach contra el daemon real: pid 31564, puerto 57045, v2.29.0.
- Pendiente: spawn con el binario real no se probó. Sin el host bridge del IDE puede pedir login OAuth o comportarse distinto; hay que verificarlo cuando no haya un Antigravity abierto.
