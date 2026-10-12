# ValidationMicroservices-pdf-extractext

Orquestador del sistema `pdf-extractext`. Recibe un PDF, lo manda al MS **Extract**, calcula el
checksum de su contenido, delega la persistencia al MS **Persistence** y la auditoría al MS
**Audit Log**.

## Ejecución local

En dev local **el Extract y el orquestador usan ambos el puerto 8080**: `defaultPort` del orquestador
es `8080` y el Extract nuevo también escucha en `8080`. Por eso hay que setear `PORT` explícitamente
para el orquestador; no se cambia el default porque en producción los puertos los asigna el
orquestador de infraestructura (mired/Traefik).

| Variable | Default | Notas |
|----------|---------|-------|
| `PORT` | `8080` | **En local, override obligatorio** si el Extract ya está en 8080. |
| `EXTRACT_BASE_URL` | `http://localhost:8080` | El Extract nuevo escucha en 8080, **no** en 8081. |
| `PERSISTENCE_BASE_URL` | `http://localhost:8000` | Puerto real de Persistence (`:8000`). |
| `AUDIT_LOG_BASE_URL` | `http://localhost:8083` | Puerto real de AUDA (`:8083`). |
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

> Cada MS valida su propio `SERVICE_API_TOKEN`, así que hay **dos secretos independientes**: el
> orquestador necesita `PERSISTENCE_API_TOKEN` y `AUDIT_LOG_API_TOKEN`, ambos sin default.

```bash
PORT=8099 EXTRACT_BASE_URL=http://127.0.0.1:8080 \
  AUDIT_LOG_BASE_URL=http://127.0.0.1:8083 AUDIT_LOG_API_TOKEN=audit-secret \
  PERSISTENCE_BASE_URL=http://127.0.0.1:8000 PERSISTENCE_API_TOKEN=persistence-secret \
  go run ./cmd/orchestrator
```

En Docker Compose las URLs van por nombre de servicio (`http://app:8080`), no por `localhost`.
Las variables `LOG_LEVEL`, `LOG_FORMAT`, `SHUTDOWN_TIMEOUT` y los límites de tamaño se
inyectan explícitamente al contenedor; los tokens no tienen valores por defecto.

## Logging

El orquestador emite logs estructurados a **stdout** (JSON por defecto; `LOG_FORMAT=text`
para desarrollo). El nivel se controla con `LOG_LEVEL`.

Cada request recibe un `request_id` (del header `X-Request-ID` o generado) que se propaga por
`context` y aparece en todas las líneas de esa request: access log, rechazos de validación,
errores upstream y logs de negocio. Así una falla se reconstruye filtrando por un solo id.

- **Access log** (`internal/server`): método, path, `status`, `bytes`, `remote_addr` y
  `duration_ms` por request. Nivel según el status: `5xx` ⇒ ERROR, `4xx` ⇒ WARN, resto ⇒ INFO.
  Los endpoints `healthz`/`readyz` no se loguean.
- **Handlers**: registran cada rechazo (413/400/415/422) y cada fallo upstream (502) con su
  status y el error real.
- **Capa de red** (`internal/httpclient`): éxito ⇒ DEBUG, status de error ⇒ WARN, fallo de
  transporte ⇒ ERROR, con `method`, `path`, `upstream`, `status` y `duration_ms`.

> Nunca se loguean tokens (`Authorization`) ni bodies (PDF/texto).

## Apagado seguro

El orquestador escucha `SIGINT`/`SIGTERM` y apaga en dos pasos con un único
presupuesto `SHUTDOWN_TIMEOUT` (configurable, default `35s`):

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
| `GET` | `/healthz`, `/readyz` | |

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
(`PORT=8099`, porque los dos default a 8080). El orquestador necesita además
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