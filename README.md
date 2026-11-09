# ValidationMicroservices-pdf-extractext

Orquestador del sistema `pdf-extractext`. Recibe un PDF, lo manda al MS **Extract**, calcula el
checksum de su contenido, delega la persistencia al MS **Persistence** y la auditoría al MS
**Audit Log**.

## Ejecución local

El contrato compartido de variables está en `.env.example`. Para Docker Compose,
copialo como `.env`, completa los dos tokens obligatorios y ajusta sólo los
valores necesarios para tu entorno. `.env` está excluido de Git.

En dev local, `go run` lee `PORT`; si Extract ya usa 8080, exportá `PORT=8099` para
el orquestador. En Docker Compose el contenedor siempre escucha en 8080 y
`ORCHESTRATOR_PORT` sólo cambia el puerto publicado en el host.

| Variable | Ejemplo en `.env.example` | Notas |
|----------|--------------------------|-------|
| `PORT` | `8080` | Listener en ejecución local; Compose fija el listener interno en 8080. |
| `ORCHESTRATOR_PORT` | `8080` | Puerto publicado por Compose en el host. |
| `EXTRACT_BASE_URL` | `https://extract.universidad.localhost` | E-W por el Traefik compartido; nunca DNS directo al contenedor. |
| `PERSISTENCE_BASE_URL` | `https://persistence.universidad.localhost` | E-W por el Traefik compartido. |
| `AUDIT_LOG_BASE_URL` | `https://audit.universidad.localhost` | E-W por el Traefik compartido. |
| `EXTRACT_HEALTH_URL` | `https://extract.universidad.localhost/healthz` | Sonda de salud de Extract por el mismo borde. |
| `PERSISTENCE_HEALTH_URL` | `https://persistence.universidad.localhost/readyz` | Sonda de readiness de Persistence. |
| `AUDIT_LOG_HEALTH_URL` | `https://audit.universidad.localhost/readyz` | Sonda de readiness de Audit Log. |
| `TRAEFIK_CA_FILE` | `../deploy/certs/rootCA.pem` | CA de mkcert montada y exportada como `SSL_CERT_FILE`. |
| `AUDIT_LOG_API_TOKEN` | *(sin default)* | **Requerido.** Bearer del orquestador hacia AUDA. El arranque falla si falta. |
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

```bash
set -a
source ./.env
set +a
PORT=8099 \
  EXTRACT_BASE_URL=http://127.0.0.1:8080 \
  PERSISTENCE_BASE_URL=http://127.0.0.1:8082 \
  AUDIT_LOG_BASE_URL=http://127.0.0.1:8083 \
  go run ./cmd/orchestrator
```

Ese bloque apunta a los puertos publicados por cada MS y sirve para `go run`
sin el borde compartido. En Docker Compose el contrato por defecto usa el
Traefik compartido de `Documents/Services`
(`https://<servicio>.universidad.localhost`, red externa `mired`); el compose
resuelve esos nombres a la IP de `traefik` en `mired` (`TRAEFIK_EW_HOST`) y
valida TLS con la CA de mkcert. Las variables
`LOG_LEVEL`, `LOG_FORMAT`, `SHUTDOWN_TIMEOUT` y los límites de tamaño se inyectan
explícitamente al contenedor; los tokens no tienen valores por defecto.

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
[Ejecución local](#ejecución-local)), aunque el stress sólo pegue al endpoint de
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
