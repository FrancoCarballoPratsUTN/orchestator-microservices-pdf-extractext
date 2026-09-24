# Todo — Orchestrator & Validator Microservice (`ValidationMicroservices-pdf-extractext`)

Plan y descomposición de la SDD Fase 2/3. El plan de diseño completo está en `tasks/plan.md`.

## Fase 2: Planning y Diseño — COMPLETADA

- [x] Diseño de la API (API Contract) — `tasks/plan.md` §1
- [x] Estructura de directorios (3 capas Go) — `tasks/plan.md` §2
- [x] Entidades y DTOs (structs) — `tasks/plan.md` §3
- [x] Interfaces (contratos internos de Service y Client) — `tasks/plan.md` §4
- [x] Flujo de datos (secuencia Ingesta PDF + Auditoría) — `tasks/plan.md` §5
- [x] `.gitignore` adaptado a Go
- [x] Plan revisado y aprobado por el humano

---

## Checkpoint: Fase 2 cerrada
- [x] Sin implementación de lógica de negocio generada (Fase 3 pendiente)
- [x] Open questions resueltas y/o confirmadas por el humano

---

## Fase 3: Implementación (backlog — NO iniciar sin aprobación)

### Task 1: Scaffold del proyecto Go — COMPLETADA

**Descripción:** Inicializar `go.mod` (chi v5, mongodb driver NO, solo REST), cargar configuración desde entorno (puertos, base URLs de Extract/Persistence/AuditLog, timeouts, límite de tamaño), util de checksum (SHA-256 hex) y `httpclient` compartido con parsing RFC 9457.

**Criterios de aceptación:**
- [x] `go build ./...` y `go vet ./...` limpios
- [x] Config se lee de env con defaults sanos
- [x] `checksum.Of(text)` devuelve 64 caracteres hex

**Verificación:** `go build ./... && go vet ./... && go test ./...` — OK. `go test -race` y cobertura: checksum 100 %, config 94.7 %, httpclient 91.7 %, server 93.3 %. Smoke test: `/healthz` 200, ruta desconocida 404, shutdown graceful por SIGTERM.

**Dependencias:** None

**Archivos:** `go.mod`, `go.sum`, `internal/config/config.go`, `internal/checksum/checksum.go`, `internal/httpclient/problem.go`, `internal/httpclient/client.go`, `internal/server/{server,routes}.go`, `cmd/orchestrator/main.go` (+ `_test.go` de cada paquete). Módulo: `validationmicroservices-pdf-extractext`, chi/v5.

**Tamaño:** S

### Task 2: Cliente REST Extract — COMPLETADA

**Descripción:** Implementar `clients/extract.Client` (contrato en `tasks/plan.md` §4.2): `POST /extract` con PDF binario, parseo a `dto.ExtractedDocument`, traducción de errores a tipados, timeout configurable.

**Criterios de aceptación:**
- [x] Envía body binario crudo (sin base64) con `Content-Type: application/pdf`
- [x] Respuesta 200 parseada a `ExtractedDocument`
- [x] Error no-2xx traducido a error de dominio (RFC 9457) vía `httpclient.Problem`

**Verificación:** `go test ./internal/clients/...` (contract test con `httptest.Server`) — OK. Cobertura del paquete: 100 %. `go build ./... && go vet ./...` limpios.

**Dependencias:** Task 1

**Archivos:** `internal/clients/extract/client.go`, `internal/clients/extract/client_test.go`

**Tamaño:** S

### Task 3: Slice vertical — Ingesta de PDF (end-to-end) — COMPLETADA

**Descripción:** `services.PDFService` (validar firma `%PDF-`, delegar a Extract, calcular checksum) + `handlers.PDFHandler` (`POST /api/v1/pdfs/extract`) + wiring del router.

**Criterios de aceptación:**
- [x] `curl -T pdf --header 'Content-Type: application/pdf' /api/v1/pdfs/extract` → `200 {checksum,text,page_count}`
- [x] PDF inválido → `400` Problem JSON
- [x] Body sin `Content-Type: application/pdf` → `415`

**Verificación:** `go test ./internal/...` (handler tests con `httptest` + mock de `extract.Client`) — OK. `go build ./... && go vet ./...` limpios, `go test -race ./...` en verde. Cobertura: services 100 %, handlers 91.4 %, server 93.8 %.

**Nota de diseño:** la dependencia del servicio hacia el MS Extract se declara como interfaz en el consumidor (`services.ExtractClient`, convención Go/DIP). El handler traduce `ErrInvalidPDF` → 400 y cualquier fallo del client → 502. La composición (composition root) vive en `cmd/orchestrator/main.go` (plan §2), montando la ruta en `internal/server/routes.go`.

**Dependencias:** Task 1, 2

**Archivos:** `internal/services/pdf_service.go`, `internal/services/pdf_service_test.go`, `internal/handlers/pdf_handler.go`, `internal/handlers/pdf_handler_test.go`

**Tamaño:** M

### Checkpoint: Tras Tasks 1-3
- [ ] App compila y arranca
- [ ] Flujo de ingesta funciona con un MS Extract mockeado
- [ ] Revisión con el humano

### Task 4: Cliente REST Audit Log + emisión asíncrona + servicio — COMPLETADA

**Descripción:** Implementar `clients/auditlog.Client` (`Emit`, `ListAll`, `ListByChecksum`), `services.AuditService` (`LogAsync` fire-and-forget con `context.WithTimeout` + reintento simple + log de fallo; `FetchLogs` proxy) y conectar la emisión dentro de `PDFService` tras el extract.

**Criterios de aceptación:**
- [x] `LogAsync` no bloquea ni propaga errores al llamador
- [x] Tras un extract exitoso se emite `{action:"pdf.extract", checksum, performed_at}` al MS Audit Log (mock verifica invocación)
- [x] `fetch by checksum` y `list all` funcionan contra contract test

**Verificación:** `go test ./internal/...` — OK, con `-race` en verde. Cobertura: services 100 %, clients/auditlog 92.3 %.

**Nota de diseño:** contrato validado contra el MS Audit Log real (FastAPI): `POST /audit/logs` → 201, `GET /audit/logs?skip&limit` y `GET /audit/logs/checksum/{checksum}` devuelven **array** `{action, entity_type, checksum, details, performed_at, _id}`. El client decodifica el array crudo y el servicio lo envuelve en `{logs: [...]}` (plan §1.3). La goroutine fire-and-forget deriva su context de `context.WithoutCancel` (sobrevive a la cancelación del request) con timeout + `maxEmitAttempts=2` y log `Warn` ante fallo.

**Dependencias:** Task 1, 3

**Archivos:** `internal/clients/auditlog/client.go`, `internal/services/audit_service.go`, `internal/services/audit_service_test.go`

**Tamaño:** M

### Task 5: Endpoint proxy de lectura de auditoría — COMPLETADA

**Descripción:** `handlers.AuditHandler` con `GET /api/v1/audit/logs` (query `checksum`, `skip`, `limit`) → `AuditService.FetchLogs` → `dto.AuditLogsResponse`.

**Criterios de aceptación:**
- [x] Sin `checksum` → rutea a `ListAll`
- [x] Con `checksum` → rutea a `ListByChecksum`
- [x] Respuesta envuelta en `{ "logs": [...] }`

**Verificación:** `go test ./internal/handlers/...` — OK (cobertura handlers 95.3 %, server 94.1 %; `-race` en verde).

**Dependencias:** Task 4

**Archivos:** `internal/handlers/audit_handler.go`, `internal/handlers/audit_handler_test.go`

**Tamaño:** S

### Checkpoint: Tras Tasks 4-5
- [ ] Auditoría end-to-end funcionando (mock del MS Audit Log)
- [ ] Review de contrato de auditoría con el humano

### Task 6: Slice vertical — Create de texto — COMPLETADA

**Descripción:** `clients/persistence.Client.Create` + `TextService.Create` + `handlers.TextHandler` (`POST /api/v1/texts`) + emisión de auditoría `text.create`.

**Criterios de aceptación:**
- [x] `POST /api/v1/texts` → `201 {message:"OK", checksum}`
- [x] Checksum vacío → `400`
- [x] Error 409 de Persistence (checksum duplicado) propagado al cliente

**Verificación:** `go test ./internal/...` — OK, con `-race` en verde. Cobertura: services 100 %, handlers 91.2 %, clients/persistence 80 %.

**Dependencias:** Task 1, 4

**Archivos:** `internal/clients/persistence/client.go`, `internal/services/text_service.go`, `internal/handlers/text_handler.go` + tests

**Tamaño:** M

### Task 7: Update (inmutabilidad) y Delete

**Descripción:** `persistence.Client.Update/Delete` + `TextService.Update` (rechazar modificación de `text`/`checksum` antes de delegar) y `Delete` + handlers `PUT`/`DELETE` + auditoría `text.update`/`text.delete`.

**Criterios de aceptación:**
- [ ] `PUT` con `text` o `checksum` en el body → `400` (regla de inmutabilidad, sin tocar Persistence)
- [ ] `PUT` válido → `200` con registro actualizado; `DELETE` → `200 {message:"OK"}`
- [ ] Checksum inexistente → `404`

**Verificación:** `go test ./internal/...`

**Dependencias:** Task 6

**Archivos:** `internal/services/text_service.go`, `internal/clients/persistence/client.go`, `internal/handlers/text_handler.go` + tests

**Tamaño:** M

### Task 8: Find by checksum (read)

**Descripción:** `persistence.Client.FindByChecksum` + `TextService.FindByChecksum` + handler `GET /api/v1/texts/{checksum}`. **Sin auditoría** (excluida).

**Criterios de aceptación:**
- [ ] `GET /api/v1/texts/{checksum}` → `200` con el `Text` almacenado
- [ ] No se emite evento de auditoría en esta operación (el mock verifica 0 llamadas a `Emit`)
- [ ] Inexistente → `404`

**Verificación:** `go test ./internal/...`

**Dependencias:** Task 6

**Archivos:** `internal/services/text_service.go`, `internal/clients/persistence/client.go`, `internal/handlers/text_handler.go` + tests

**Tamaño:** S

### Checkpoint: Tras Tasks 6-8
- [ ] CRUD completo funcionando contra mock de Persistence
- [ ] Regla de inmutabilidad cubierta por tests
- [ ] Revisión con el humano

### Task 9: Middleware y endpoints operacionales

**Descripción:** `/healthz`, `/readyz`, middleware de request-id, recoverer, content-type enforcement, CORS básico y estructura final del router.

**Criterios de aceptación:**
- [ ] `/healthz` y `/readyz` responden `200`
- [ ] Panic en handler → 500 Problem JSON (no crash)
- [ ] Todas las rutas devuelven los content-types correctos

**Verificación:** `go test ./internal/... && go vet ./...`

**Dependencias:** Task 5, 8

**Archivos:** `internal/handlers/handlers.go`, `internal/handlers/router.go` (o `internal/server/`), + tests

**Tamaño:** M

### Task 10: Cobertura de tests final

**Descripción:** Contract tests de los 3 clients, tests de integración ligera (compose de los MS reales o mocks HTTP), gap de coverage en flujo `extract → persistence → audit`.

**Criterios de aceptación:**
- [ ] `go test ./...` en verde
- [ ] Cobertura objetivo ≥ 70 % en `internal/services` y `internal/handlers`

**Verificación:** `go test ./... -cover`

**Dependencias:** todas las anteriores

**Archivos:** varios `_test.go`

**Tamaño:** M

### Checkpoint: Tras Task 10 — Listo para revisión de Fase 3
- [ ] Todos los criterios de aceptación cumplidos
- [ ] `go build`, `go vet`, `go test` limpios
- [ ] Prueba manual end-to-end con PDF real
- [ ] Revisión final con el humano

---

## Notas
- Comandos de verificación del repo: `go build ./...`, `go vet ./...`, `go test ./...` (aún no configurado lint; se puede añadir golangci-lint en Task 9 si se aprueba).
- Antes de cada checkpoint, resolver las Open Questions de `tasks/plan.md`.