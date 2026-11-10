# ValidationMicroservices-pdf-extractext

Orquestador del sistema `pdf-extractext`. Recibe un PDF, lo manda al MS **Extract**, calcula el
checksum de su contenido, delega la persistencia al MS **Persistence** y la auditoría al MS
**Audit Log**.

## Requisitos

- **Go 1.26.10** o compatible (ver `go.mod`).
- **Docker Engine + Docker Compose v2** para el modo contenedor (`make up`).
- Para el modo Docker: red externa `mired` y el **Traefik compartido** del
  sistema `pdf-extractext` (contenedor `traefik`) levantados.
- Los microservicios **Extract**, **Persistence** y **Audit Log** accesibles,
  ya sea en local (`:8080`, `:8082`, `:8083`) o detrás de Traefik.
- Los dos tokens de servicio: `AUDIT_LOG_API_TOKEN` y `PERSISTENCE_API_TOKEN`.

## Cómo ejecutar

El `Makefile` es el punto de entrada: `make` (sin argumentos) lista todos los
atajos.

| Target | Qué hace |
|--------|----------|
| `make` / `make help` | Lista los atajos disponibles. |
| `make build` | Compila el binario en `bin/orchestrator`. |
| `make run` | Levanta el servicio en local con `go run` en `:8099`. |
| `make smoke` | Compila, levanta en `:8099` y corre healthz + matriz de validaciones + extracción real si el Extract responde. |
| `make up` | Construye la imagen y levanta el stack con Docker Compose. |
| `make ps` | Estado de los contenedores del stack. |
| `make logs` | Sigue los logs del stack. |
| `make down` | Baja el stack. |
| `make test` | `go vet ./...` + `go test -count=1 ./...`. |
| `make test-race` | Tests con el detector de carreras (como CI). |
| `make cover` | Cobertura en `coverage.out`. |
| `make vet` / `make fmt` | Análisis estático / formateo. |
| `make clean` | Borra `bin/`, `coverage.out` y logs locales. |

### Opción A — local con `go run` (desarrollo)

`make run` levanta el binario en el host y usa por defecto esta configuración.
**No toma `PORT` ni las URLs de `.env`**, para no chocar con lo que publica
Compose; los tokens sí se leen del entorno o de `.env`:

| Variable | Default local |
|----------|---------------|
| `PORT` | `8099` |
| `EXTRACT_BASE_URL` | `http://127.0.0.1:8080` |
| `PERSISTENCE_BASE_URL` | `http://127.0.0.1:8082` |
| `AUDIT_LOG_BASE_URL` | `http://127.0.0.1:8083` |
| `EXTRACT_HEALTH_URL` | `<EXTRACT_BASE_URL>/healthz` |
| `PERSISTENCE_HEALTH_URL` | `<PERSISTENCE_BASE_URL>/readyz` |
| `AUDIT_LOG_HEALTH_URL` | `<AUDIT_LOG_BASE_URL>/readyz` |

Cualquier valor se puede pisar desde la línea de comandos:

```bash
make run PORT=9000 EXTRACT_BASE_URL=http://127.0.0.1:9090
```

Para probar el flujo completo de una vez usá `make smoke`: valida `400/415/422`
sin dependencias y, si el Extract responde en `127.0.0.1:8080`, también hace una
extracción real y verifica `checksum == SHA-256(bytes del PDF)`.

### Opción B — Docker Compose (`make up`)

1. Copiá `.env.example` a `.env` y completá `AUDIT_LOG_API_TOKEN` y
   `PERSISTENCE_API_TOKEN`. Son obligatorios: Compose no arranca sin ellos.
2. Verificá que existan la red `mired` y el Traefik compartido:

   ```bash
   docker network inspect mired >/dev/null 2>&1 || docker network create mired
   docker start traefik   # o levantá el Traefik desde el repo de deploy
   ```

3. Verificá la CA del Traefik compartido:
   `~/Documents/Services/traefik/certs/rootCA.pem` (symlink a la raíz de
   mkcert). Si usás otra ruta, ajustá `TRAEFIK_CA_FILE`.
4. Levantá el stack:

   ```bash
   make up
   make ps
   ```

El listener **interno** del contenedor es siempre `8080`; `ORCHESTRATOR_PORT`
es el puerto publicado en el host (el `.env.example` usa `8081` para no chocar
con el Extract en `8080`). El contenedor resuelve
`<servicio>.universidad.localhost` contra la IP de `traefik` en `mired` y valida
el TLS con la raíz de mkcert de `~/Documents/Services/traefik/certs/rootCA.pem`
(`SSL_CERT_FILE`). `make logs` sigue los logs y `make down` baja el stack.

> `/readyz` sólo devuelve `200` si Extract, Persistence y Audit Log están vivos
> detrás de ese Traefik. Si están caídos, el orquestador igual arranca y
> `/healthz` responde `200`, pero `/readyz` devuelve `503`.

### Ejecución manual (sin Makefile)

Pasá **todas** las variables requeridas: el arranque aborta si falta una health
URL, un timeout, un límite o un token.

```bash
set -a
source ./.env          # sólo para tomar los tokens
set +a

PORT=8099 \
  EXTRACT_BASE_URL=http://127.0.0.1:8080 \
  EXTRACT_HEALTH_URL=http://127.0.0.1:8080/healthz \
  PERSISTENCE_BASE_URL=http://127.0.0.1:8082 \
  PERSISTENCE_HEALTH_URL=http://127.0.0.1:8082/readyz \
  AUDIT_LOG_BASE_URL=http://127.0.0.1:8083 \
  AUDIT_LOG_HEALTH_URL=http://127.0.0.1:8083/readyz \
  go run ./cmd/orchestrator
```

También podés compilar con `make build` y ejecutar `./bin/orchestrator`.

### Verificar que está arriba

```bash
# Modo local (make run / make smoke)
curl -fsS http://127.0.0.1:8099/healthz
curl -sS  http://127.0.0.1:8099/readyz

# Modo Docker (usa ORCHESTRATOR_PORT del .env, por ejemplo 8081)
curl -fsS http://127.0.0.1:8081/healthz

# Extracción de prueba
curl -f -X POST -H 'Content-Type: application/pdf' \
  --data-binary @tests/stress/pdfs/2020-Scrum-Guide-Spanish-Latin-South-American.pdf \
  http://127.0.0.1:8099/api/v1/pdfs/extract
```

### Problemas comunes

| Síntoma | Causa y solución |
|---------|------------------|
| `... must be set and non-empty` / `... must be set` | Falta una variable requerida. Con `make run`/`make smoke` ya hay defaults; si usás `go run` manual, pasá las health URLs y los timeouts. |
| `AUDIT_LOG_API_TOKEN must be set...` al usar `make up` | Faltan los tokens en `.env` (Compose los exige con `:?`). |
| `listen tcp :8099: bind: address already in use` | El puerto local está ocupado: `make run PORT=9000` (o `make smoke PORT=9000`). |
| `invalid IP address in add-host: "invalid IP"` | El contenedor `traefik` está detenido o no está en `mired`. Hacé `docker start traefik` o pasá `make up TRAEFIK_EW_HOST=<ip>`. |
| `failed to bind host port 0.0.0.0:8080` en `make up` | `ORCHESTRATOR_PORT` choca con otro proceso (por ejemplo el Extract). Cambialo en `.env` (`8081`, `8090`, etc.). |
| `x509: certificate signed by unknown authority` | `TRAEFIK_CA_FILE` no apunta a la CA de mkcert correcta. |
| `/readyz` devuelve `503` | Algún upstream no responde o no está registrado detrás de Traefik. Revisá `make logs` y los logs del servicio caído. |

## Configuración

El contrato de variables está en `.env.example`; copialo como `.env` para
Docker Compose y completá los dos tokens. `.env` está excluido de Git. La
precedencia es: línea de comandos `>` entorno `>` `.env` (tokens y
`TRAEFIK_EW_HOST` en `make`) `>` defaults del `Makefile`. Compose lee `.env`
completo.

| Variable | Ejemplo en `.env.example` | Notas |
|----------|--------------------------|-------|
| `PORT` | `8099` | Listener en ejecución local (`make run`); Compose fija el listener interno en `8080`. |
| `ORCHESTRATOR_PORT` | `8081` | Puerto publicado por Compose en el host. Elegir uno libre (8080 suele ser del Extract). |
| `EXTRACT_BASE_URL` | `https://extract.universidad.localhost` | E-W por el Traefik compartido; nunca DNS directo al contenedor. |
| `PERSISTENCE_BASE_URL` | `https://persistence.universidad.localhost` | E-W por el Traefik compartido. |
| `AUDIT_LOG_BASE_URL` | `https://audit.universidad.localhost` | E-W por el Traefik compartido. |
| `EXTRACT_HEALTH_URL` | `https://extract.universidad.localhost/healthz` | Sonda de salud de Extract por el mismo borde. |
| `PERSISTENCE_HEALTH_URL` | `https://persistence.universidad.localhost/readyz` | Sonda de readiness de Persistence. |
| `AUDIT_LOG_HEALTH_URL` | `https://audit.universidad.localhost/readyz` | Sonda de readiness de Audit Log. |
| `TRAEFIK_CA_FILE` | `${HOME}/Documents/Services/traefik/certs/rootCA.pem` | CA raíz de mkcert (trust anchor) que firma el cert del Traefik compartido; se exporta como `SSL_CERT_FILE`. No es el cert hoja. |
| `CONSUL_ENABLED` | `false` | `false` ⇒ toda la configuración sale del entorno. `true` ⇒ primero KV de Consul, entorno como fallback. |
| `CONSUL_HTTP_ADDR` | `https://consul:8501` | **Requerido con Consul habilitado.** |
| `CONSUL_HTTP_TOKEN` | *(sin default)* | **Requerido con Consul habilitado.** |
| `CONSUL_KV_PREFIX` | `pdf-extractext/dev/orchestrator` | **Requerido con Consul habilitado.** |
| `CONSUL_SERVICE_NAME` | `orchestrator` | Nombre con el que se registra el servicio en Consul. |
| `AUDIT_LOG_API_TOKEN` | *(sin default)* | **Requerido.** Bearer del orquestador hacia Audit Log. El arranque falla si falta. |
| `PERSISTENCE_API_TOKEN` | *(sin default)* | **Requerido.** Bearer del orquestador hacia Persistence. El arranque falla si falta. |
| `SHUTDOWN_TIMEOUT` | `35s` | Deadline para el apagado (drena `http.Server.Shutdown` y `auditService.Close`). Un timeout agotado causa salida con código 1. |
| `HTTP_TIMEOUT` | `35s` | **Debe superar los 30s de deadline interno del Extract.** Con menos, cortamos nosotros primero y devolvemos un error opaco en lugar del `503` real y tipado que devuelve el Extract. |
| `STOP_GRACE_PERIOD` | `45s` | Grace period de Compose; debe ser mayor que `SHUTDOWN_TIMEOUT`. |
| `MAX_PDF_SIZE_BYTES` | `15MB` | Más estricto que los 50MB del Extract, así que dispara primero el nuestro. |
| `MAX_TEXT_BODY_BYTES` | `15MB` | Límite del cuerpo JSON para creación de textos. |
| `MAX_PDF_PAGES` | `1000` | Corta PDFs patológicos antes de llamar al Extract. `0` = sin límite. |
| `LOG_LEVEL` | `info` | Nivel mínimo de log: `debug`, `info`, `warn` o `error`. Un valor inválido aborta el arranque. |
| `LOG_FORMAT` | `json` | Formato de log: `json` o `text`. Un valor inválido aborta el arranque. |
| `HEALTH_CHECK_TIMEOUT` | `2s` | Deadline por dependencia sondeada en `/readyz`. |
| `LOG_MAX_VALUE_LENGTH` | `256` | Límite en caracteres para valores externos en logs. |
| `LOG_MAX_STACK_LENGTH` | `4096` | Límite en caracteres para stacks de panic. |

Cada valor operativo debe estar en Consul KV o en el entorno. Con Consul habilitado,
KV tiene prioridad y el entorno sirve como fallback. La configuración incompleta o
inválida aborta el arranque, excepto `LOG_MAX_STACK_LENGTH`, que usa el default
seguro de 4096 caracteres cuando falta.

Con `CONSUL_ENABLED=true`, `LOG_MAX_VALUE_LENGTH` debe existir y ser positivo bajo
`<CONSUL_KV_PREFIX>`. `LOG_MAX_STACK_LENGTH` es opcional; si se configura, debe ser
positivo.

### Preflight de Consul y rollback

Antes de desplegar, verificar que el prefijo de Consul contenga valores no vacíos
para estas claves requeridas (los tokens no deben imprimirse al validar):

```text
PORT, EXTRACT_BASE_URL, PERSISTENCE_BASE_URL, AUDIT_LOG_BASE_URL,
EXTRACT_HEALTH_URL, PERSISTENCE_HEALTH_URL, AUDIT_LOG_HEALTH_URL,
HTTP_TIMEOUT, SHUTDOWN_TIMEOUT, MAX_PDF_SIZE_BYTES, MAX_TEXT_BODY_BYTES,
MAX_PDF_PAGES,
HEALTH_CHECK_TIMEOUT, LOG_MAX_VALUE_LENGTH, AUDIT_LOG_API_TOKEN,
PERSISTENCE_API_TOKEN
```

Provisionar y validar el KV antes de desplegar esta versión; Consul prevalece sobre
las variables de entorno, incluso si el valor KV está vacío. Desplegar luego el
orquestador y comprobar `/healthz`, `/readyz` y extracción. Para rollback, volver
a la imagen anterior: sus claves nuevas no cambian el comportamiento porque la
versión anterior no las lee. Mantener los dos tokens configurados y válidos para
la versión que quede activa.

> Cada MS valida su propio `SERVICE_API_TOKEN`, así que hay **dos secretos independientes**: el
> orquestador necesita `PERSISTENCE_API_TOKEN` y `AUDIT_LOG_API_TOKEN`, ambos sin default.

## CVE-2025-60876 y BusyBox `wget`

La imagen runtime usa Alpine, que incluye BusyBox; por eso un scanner puede reportar
CVE-2025-60876 aunque el binario de la aplicación sea Go. El healthcheck de Compose
invoca `wget` con una URL fija (`http://localhost:8080/healthz`): no construye esa URL
con el path, query ni headers de una petición entrante. En el uso actual, los endpoints
no proporcionan entrada a ese `wget`; esto describe la alcanzabilidad observada en el
código, no elimina la vulnerabilidad del paquete instalado.

La CVE afecta el manejo de caracteres de control en el *request-target* de BusyBox
`wget`, no la generación de headers de respuesta de los handlers Go. Mantener Alpine no
impide actualizar BusyBox cuando haya una versión corregida compatible con la rama usada.
[Detalle de la CVE](https://nvd.nist.gov/vuln/detail/CVE-2025-60876).

## Logging

El orquestador emite logs estructurados a **stdout** (JSON por defecto; `LOG_FORMAT=text`
para desarrollo). El nivel se controla con `LOG_LEVEL`.

Cada request conserva el `X-Request-ID` entrante o genera uno si falta. El ID vuelve en la
respuesta, se propaga a los servicios upstream y aparece en los logs de acceso, handlers,
cliente HTTP y servicios de negocio para correlacionar una operación entre microservicios.

- La observabilidad del servicio es **sólo logging**: no hay métricas, tracing ni endpoint
  de telemetría. El access log es deliberadamente básico.
- **Access log** (`internal/server`): método, path, `status` y
  `duration_ms` por request. Nivel según el status: `5xx` ⇒ ERROR, `4xx` ⇒ WARN, resto ⇒ INFO.
  Los endpoints `healthz`/`readyz` no se loguean. Una request que termina en panic también
  aparece, con su `500`.
- **Handlers**: registran cada rechazo (413/400/415/422) y cada fallo upstream (502) con su
  status y el error real.
- **Capa de red** (`internal/httpclient`): éxito ⇒ DEBUG, status de error ⇒ WARN, fallo de
  transporte ⇒ ERROR, con `method`, `path`, `upstream`, `status` y `duration_ms`.

> Nunca se loguean tokens (`Authorization`) ni bodies (PDF/texto).

`internal/logging` concentra la higiene de lo que llega al log:

- `SafeURL` deja la URL con esquema, host y ruta: sin usuario, sin contraseña, sin query
  string y sin fragmento. Advierte que conserva el path, por lo que la ruta no debe contener secretos.
- `SafeError` quita la URL que `net/http` envuelve alrededor de sus errores: el upstream y
  el path ya se registran como campos separados, y la URL puede llevar un token.
- `Truncate` acota en longitud los valores que vienen de fuera (`X-Filename`, path y método
  de la petición, mensaje de un panic) y los atributos `error` de log, para que un cliente o fallo
  no pueda escribir una línea de log enorme.

## Apagado seguro

El orquestador escucha `SIGINT`/`SIGTERM` y apaga en dos pasos con un único
presupuesto `SHUTDOWN_TIMEOUT` (configurado en Consul KV o en el entorno):

1. `http.Server.Shutdown` deja de aceptar conexiones y drena las requests en
   curso.
2. `auditService.Close` espera a que terminen las emisiones de auditoría
   asíncronas agendadas antes del shutdown. Al iniciar y al terminar este
   drenado se emiten logs (`"draining audit emissions"`, `"audit emissions drained"`).

Si el deadline se agota, el proceso registra los WARN correspondientes y
retorna un error (salida con código **1**), evitando colgarse indefinidamente.
El presupuesto es ajustable por entorno para que, en despliegues, `SHUTDOWN_TIMEOUT`
supera razonablemente a `HTTP_TIMEOUT` o se acorte según la política de restart
(Compose usa `stop_grace_period` por encima del deadline configurado).

## Contrato del Extract

`POST /extract` con el PDF binario crudo responde:

```json
{ "content": "...", "page_count": 16 }
```

> `text` es el nombre del campo de nuestra API de salida y contiene el `content` del Extract tal
> cual: el Extract ya entrega el texto formateado y el orquestador no lo vuelve a convertir. El
> checksum es el SHA-256 de los **bytes del PDF subido** (`checksum == SHA-256(bytes del PDF)`),
> que es la clave de dedup del sistema, no del texto.

## Contrato de Persistence

Ruta base `/texts` (**sin** `/api/v1`) y checksum como **query param** (`url.QueryEscape`):

| Operación | Método y ruta | Cuerpo | Respuesta |
|-----------|---------------|--------|-----------|
| Create | `POST /texts` | `{text, checksum, name?, metadata?}` | `201 {message, checksum}` + `Location` |
| Read | `GET /texts?checksum=<cs>` | — | `200 TextOut` |
| Update | `PUT /texts?checksum=<cs>` | `{name?, metadata?}` (≥1 cambio) | `200 TextOut` |
| Delete | `DELETE /texts?checksum=<cs>` | — | `200 {message:"OK", checksum}` |

`TextOut = {checksum, text, name, metadata, created_at, updated_at}` (RFC 3339, ms). En `PUT`,
`null` cuenta como campo ausente y `""`/`{}` como limpiar; sin cambios efectivos ⇒ `400`. Toda ruta
salvo `/health` y `/readyz` exige `Authorization: Bearer <SERVICE_API_TOKEN>` (de ahí
`PERSISTENCE_API_TOKEN`). Un `401` de Persistence es falla de cableado y se traduce a `502`.

## Endpoints

| Método | Ruta | |
|--------|------|---|
| `POST` | `/api/v1/pdfs/extract` | `application/pdf` crudo. `X-Filename` opcional: si viene, debe terminar en `.pdf`. Si el PDF (por sus bytes) ya fue extraído, responde desde el cache de dedup sin llamar al Extract ni auditar. |
| `POST` | `/api/v1/texts` | Delegado a Persistence. |
| `GET` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `PUT` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `DELETE` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `GET` | `/api/v1/audit/logs` | Delegado a Audit Log. |
| `GET` | `/healthz` | Liveness estático; también es el check HTTP predeterminado de Consul. |
| `GET` | `/readyz` | Readiness agregada de los upstreams; sirve para monitoreo y puede devolver `503` ante una degradación parcial. |

### Errores de `POST /api/v1/pdfs/extract`

Todos son `application/problem+json` (RFC 9457), emitidos por `internal/validation` vía
`StatusOf(err)`:

| Condición | Status |
|-----------|--------|
| `Content-Type` ≠ `application/pdf` | `415` |
| Body > `MAX_PDF_SIZE_BYTES` | `413` |
| `X-Filename` presente y no termina en `.pdf` | `415` |
| Sin firma `%PDF-` | `400` |
| PDF corrupto / estructura inválida | `400` |
| PDF encriptado o protegido con contraseña | `422` |
| `page_count` > `MAX_PDF_PAGES` | `422` |
| PDF válido pero sin texto extraíble (escaneado) | `422` |
| Extract no disponible o timeout | `502` |

`400` = "esto no es un PDF"; `422` = "es un PDF real que no se puede procesar en este sistema"
(encriptado, demasiadas páginas, sin capa de texto). La validación de encriptado solo **clasifica**
el PDF: nunca lo descifra ni pide contraseña.

## Tests

```bash
make test        # go vet ./... + go test -count=1 ./...
make test-race   # equivalente a CI, con detector de carreras
make cover       # cobertura en coverage.out
```

También se pueden correr directo:

```bash
go vet ./...
go test -race -count=1 ./...
```

## Stress y carga

```bash
k6 run scripts/k6/spike.js                          # 100 VUs
./scripts/vegeta/attack.sh                          # 50 req/s durante 30s
```

Ambas necesitan el Extract real en `:8080` y el orquestador en `:8099`
(`PORT=8099`, porque ambos ejemplos usan 8080). El orquestador necesita además
`PERSISTENCE_API_TOKEN` y `AUDIT_LOG_API_TOKEN` para arrancar (ver
[Cómo ejecutar](#cómo-ejecutar)), aunque el stress sólo pegue al endpoint de
extracción. El spike de k6 re-hashea los
**bytes del PDF** que envía y los compara con el `checksum` declarado, request por
request: el invariante `checksum == SHA-256(bytes del PDF)` es lo que un generador
de carga que sólo mira códigos de estado no puede detectar. El script de vegeta,
que no puede re-hashear cuerpos, re-verifica el invariante con un request por PDF
al terminar.

Para levantar y probar el orquestador de una vez: `make smoke` (healthz + matriz
de validaciones + extracción real, con el PDF cifrado generado al vuelo).

Detalle, resultados medidos y el techo de capacidad (~33 req/s con el corpus
completo, degradando por cola y sin errores) en
**[tests/stress/README.md](tests/stress/README.md)**.
