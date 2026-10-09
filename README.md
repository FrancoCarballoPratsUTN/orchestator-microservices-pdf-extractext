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
| `HTTP_TIMEOUT` | `35s` | **Debe superar los 30s de deadline interno del Extract.** Con menos, cortamos nosotros primero y devolvemos un error opaco en lugar del `503` real y tipado que devuelve el Extract. |
| `MAX_PDF_SIZE_BYTES` | `15MB` | Más estricto que los 50MB del Extract, así que dispara primero el nuestro. |
| `MAX_PDF_PAGES` | `1000` | Corta PDFs patológicos antes de llamar al Extract. `0` = sin límite. |

> Cada MS valida su propio `SERVICE_API_TOKEN`, así que hay **dos secretos independientes**: el
> orquestador necesita `PERSISTENCE_API_TOKEN` y `AUDIT_LOG_API_TOKEN`, ambos sin default.

```bash
PORT=8099 EXTRACT_BASE_URL=http://127.0.0.1:8080 \
  AUDIT_LOG_BASE_URL=http://127.0.0.1:8083 AUDIT_LOG_API_TOKEN=audit-secret \
  PERSISTENCE_BASE_URL=http://127.0.0.1:8000 PERSISTENCE_API_TOKEN=persistence-secret \
  go run ./cmd/orchestrator
```

En Docker Compose las URLs van por nombre de servicio (`http://app:8080`), no por `localhost`.

## Contrato del Extract

`POST /extract` con el PDF binario crudo responde:

```json
{ "content": "...", "page_count": 16 }
```

> `text` es el nombre del campo de nuestra API de salida y contiene el `content` del Extract tal
> cual: el Extract ya entrega el texto formateado y el orquestador no lo vuelve a convertir. El
> checksum es el SHA-256 de ese texto, preservando el invariante `checksum == SHA-256(text persistido)`.

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
| `POST` | `/api/v1/pdfs/extract` | `application/pdf` crudo. `422` si el PDF no tiene capa de texto. |
| `POST` | `/api/v1/texts` | Delegado a Persistence. |
| `GET` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `PUT` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `DELETE` | `/api/v1/texts/{checksum}` | Delegado a Persistence. |
| `GET` | `/api/v1/audit/logs` | Delegado a Audit Log. |
| `GET` | `/healthz`, `/readyz` | |

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
(`PORT=8099`, porque los dos default a 8080). El spike de k6 re-hashea cada
respuesta para comprobar el invariante `checksum == SHA-256(bytes del PDF)`
request por request, que es lo que un generador de carga que sólo mira códigos
de estado no puede detectar.

Detalle, resultados medidos y el techo de capacidad (~33 req/s con el corpus
completo, degradando por cola y sin errores) en
**[tests/stress/README.md](tests/stress/README.md)**.