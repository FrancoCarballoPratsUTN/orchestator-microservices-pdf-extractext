# Todo — Orchestrator & Validator Microservice (`ValidationMicroservices-pdf-extractext`)

Plan y descomposición de la SDD Fase 2/3, más la Fase 4 (handoff de autenticación con el MS
Audit Log — **desbloqueada**), la Fase 5 (contrato real del Extract + markdown + verificación de
texto), la Fase 6 (stress) y la Fase 7 (integración con el MS Persistence real). El plan de
diseño completo está en `tasks/plan.md`. Los issues del repo (`[ORC-XX]` + `[ORC-XX.Y]`) siguen
esta misma descomposición.

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
- [x] Revisión con el humano

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
- [x] Review de contrato de auditoría con el humano

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

### Task 7: Update (inmutabilidad) y Delete — COMPLETADA

**Descripción:** `persistence.Client.Update/Delete` + `TextService.Update` (rechazar modificación de `text`/`checksum` antes de delegar) y `Delete` + handlers `PUT`/`DELETE` + auditoría `text.update`/`text.delete`.

**Criterios de aceptación:**
- [x] `PUT` con `text` o `checksum` en el body → `400` (regla de inmutabilidad, sin tocar Persistence)
- [x] `PUT` válido → `200` con registro actualizado; `DELETE` → `200 {message:"OK"}`
- [x] Checksum inexistente → `404`

**Verificación:** `go test ./internal/...` — OK, con `-race` en verde. Cobertura: services 100 %, handlers 89.1 %, clients/persistence 83.3 %.

**Dependencias:** Task 6

**Archivos:** `internal/services/text_service.go`, `internal/clients/persistence/client.go`, `internal/handlers/text_handler.go` + tests

**Tamaño:** M

**Nota de diseño:** la regla de inmutabilidad se aplica en la capa de transporte (handler): el body se parsea como mapa y si contiene las claves `text`/`checksum` responde `400` **sin invocar al servicio** (nunca llega a Persistence). El servicio refuerza la inmutabilidad estructuralmente: solo delega `name`+`metadata` (el DTO `UpdateTextRequest` no expone `text`/`checksum`). `models.Text`, `OpTextUpdate`, `OpTextDelete` y `dto.UpdateTextRequest/DeleteTextResponse` agregados. El checksum de la ruta se extrae con `pathChecksum` (último segmento del path), desacoplado de chi (testeable directo).

### Task 8: Find by checksum (read) — COMPLETADA

**Descripción:** `persistence.Client.FindByChecksum` + `TextService.FindByChecksum` + handler `GET /api/v1/texts/{checksum}`. **Sin auditoría** (excluida).

**Criterios de aceptación:**
- [x] `GET /api/v1/texts/{checksum}` → `200` con el `Text` almacenado
- [x] No se emite evento de auditoría en esta operación (el mock verifica 0 llamadas a `LogAsync`)
- [x] Inexistente → `404`

**Verificación:** `go test ./internal/...` — OK, con `-race` en verde. Cobertura: services 100 %, handlers 89.5 %, clients/persistence 86.7 %.

**Dependencias:** Task 6

**Archivos:** `internal/services/text_service.go`, `internal/clients/persistence/client.go`, `internal/handlers/text_handler.go` + tests

**Tamaño:** S

**Nota de diseño:** `FindByChecksum` en el Service es una delegación pura a Persistence **sin** `LogAsync` (leer no es una acción auditable); los tests lo verifican exigiendo 0 eventos en `stubAudit`, incluso ante éxito. El handler reutiliza `pathChecksum` y `writeServiceError` (404 Problem propagada desde Persistence, transporte → 502).

### Checkpoint: Tras Tasks 6-8
- [x] CRUD completo funcionando contra mock de Persistence
- [x] Regla de inmutabilidad cubierta por tests
- [x] Revisión con el humano (decisión sobre el límite de body tomada en 24-sep-2026)

**E2E (24-sep-2026):** orquestador real + MS Extract real (`target/release/extract` en `127.0.0.1:8081`) + mocks de Audit Log (`/tmp/opencode/mock_auditlog.py`, 8083) y Persistence (`/tmp/opencode/mock_persistence.py`, 8082).
- `GET /healthz` y `/readyz` → 200; middleware visibles (CORS: `Access-Control-Allow-Origin: *`, `X-Request-ID` presente).
- `POST /api/v1/pdfs/extract` con `sample_pdf.pdf` (250 págs) → checksum `5670c10086292bbede561b71637288e1ba2566af4dd572b86f662cd2241ccd71`.
- `POST /api/v1/texts` → 201 `{message:"OK", checksum}`; `GET /api/v1/texts/{cs}` → 200 con `text`+`name`; `PUT` → 200 con `name` actualizado y `updated_at` seteado; `DELETE` → 200 `{message:"OK"}`; `GET` tras delete → 404.
- Inmutabilidad en vivo: `PUT` con `text` o `checksum` en el body → 400 `"text and checksum are immutable"` sin tocar Persistence.
- Auditoría completa para el checksum: `pdf.extract`, `text.create` (name: "libro redes"), `text.update` (name: "libro redes (2da ed)"), `text.delete`.
- **Hallazgo resuelto (decisión del humano):** el texto extraído real (980 KB crudos) genera un body JSON de ~1.06 MB → excedía `maxTextBodyBytes = 1<<20` (1 MB) y causaba `413`. Se subió el límite: `Config.MaxTextBodyBytes` con env `MAX_TEXT_BODY_BYTES`, **default = `MAX_PDF_SIZE_BYTES` (15 MB)** y `NewTextHandler(service, maxTextBodyBytes)` (simétrico a `PDFHandler`). Tests: repro acepta body ~1.5 MB con límite configurado de 2 MB (201) y rechaza > límite (413); config cubre default + override.

### Task 9: Middleware y endpoints operacionales — COMPLETADA

**Descripción:** `/healthz`, `/readyz`, middleware de request-id, recoverer, content-type enforcement, CORS básico y estructura final del router.

**Criterios de aceptación:**
- [x] `/healthz` y `/readyz` responden `200`
- [x] Panic en handler → 500 Problem JSON (no crash)
- [x] Todas las rutas devuelven los content-types correctos

**Verificación:** `go test ./internal/... && go vet ./...` — OK, con `-race` en verde. Cobertura: server 96.7 %, handlers 87.7 %.

**Dependencias:** Task 5, 8

**Archivos:** `internal/server/middleware.go`, `internal/server/routes.go`, `internal/httpapi/httpapi.go` + tests

**Tamaño:** M

**Nota de diseño (clean code/SOLID):** las preocupaciones transversales viven en middleware, no en los handlers: `withRecovery` (500 `application/problem+json` + log de panic/stack), `withRequestID` (echo de `X-Request-ID` entrante o generación; se propaga por contexto y en logs), `withCORS` (básico, sin dependencia externa) y `enforceContentType` — la política de content-type quedó **centralizada en el router** (`api.With(enforceContentType(...))`) y fue removida de `pdf_handler`/`text_handler` (un solo lugar de verdad, SRP/DRY). `/healthz` y `/readyz` comparten `statusEndpoint()` (evita duplicación). Los helpers de respuesta (`WriteJSON`/`WriteProblem`) viven ahora en `internal/httpapi` y reemplazan las 3 copias que había en `handlers`/`server`; la enforce íntegra se cubre con tests a nivel de router (415, preflight CORS, request-id, panic).

### Task 10: Cobertura de tests final — COMPLETADA

**Descripción:** Contract tests de los 3 clients, tests de integración ligera (compose de los MS reales o mocks HTTP), gap de coverage en flujo `extract → persistence → audit`.

**Criterios de aceptación:**
- [x] `go test ./...` en verde
- [x] Cobertura objetivo ≥ 70 % en `internal/services` y `internal/handlers`

**Verificación:** `go vet ./... && go test -race -count=1 ./...` — OK. Cobertura: services **100.0 %**, handlers **90.4 %**, server 96.7 %.

**Dependencias:** todas las anteriores

**Archivos:** `internal/server/integration_test.go` + `internal/config/*` y `internal/handlers/text_handler*` (límite de body)

**Tamaño:** M

**Nota de diseño (clean code/SOLID):** el test de integración ligera (`TestIntegrationExtractPersistAuditFlow`) monta **clientes + servicios + handlers + router reales** contra 3 mocks HTTP (`httptest.Server`: Extract `POST /extract`, Persistence `/texts` CRUD en memoria, Audit `POST+GET /audit/logs`) y cubre el gap `extract → persistence → audit`: extrae un PDF falso (`%PDF-...`), persiste el texto, actualiza, borra (con 404 post-delete) y verifica los 4 eventos de auditoría (`pdf.extract`, `text.create`, `text.update`, `text.delete`) con espera por poll del `LogAsync` asíncrono. Los mocks validan contratos reales (método, path, `application/problem+json` para 404/409/400) y son thread-safe (`sync.Mutex`) para tolerar el goroutine de auditoría.

### Checkpoint: Tras Task 10 — Listo para revisión de Fase 3
- [x] Todos los criterios de aceptación cumplidos
- [x] `go build`, `go vet`, `go test` limpios
- [x] Prueba manual end-to-end con PDF real
- [ ] Revisión final con el humano

---

## Fase 4: Handoff de autenticación con el MS Audit Log — DESBLOQUEADA (issues #16-#23)

> **Estado real verificado contra el código (07-10-2026): AUDA YA implementó el auth.**
> En `AuditLog/AuditLogMicroservice-pdf-extractext`, `app/config.py` declara
> `service_api_token: SecretStr` sin default, `app/main.py` registra `BearerAuthMiddleware`
> como middleware *outermost*, y `app/api/auth.py` deja abiertas sólo `/health` y `/readyz`
> respondiendo `401` con `WWW-Authenticate: Bearer`. El bloqueo que difería la fase **desapareció**.
>
> **Salvedad:** AUDA todavía **no cableó sus endpoints HTTP** (`POST/GET /audit/logs`): no existe
> `app/api/routers/audit_logs.py`, `main.py` sólo monta `health`, y sus tasks T10/T13 siguen sin
> cerrar. Por eso:
> - Las Tasks 11-17 (issues #16-#23) **se ejecutan ya**: token, header y diagnóstico de `401` no
>   dependen de las rutas de AUDA.
> - La **verificación E2E de auditoría** sigue bloqueada hasta que AUDA exponga las rutas. El
>   código del orquestador queda listo mientras tanto.
>
> **Decisión (humano, 07-10-2026): Fase 4 desbloqueada.**

El contrato previsto: **todas** las rutas de AUDA exigen `Authorization: Bearer
<SERVICE_API_TOKEN>` salvo `/health` y `/readyz`; el middleware de auth es *outermost* y la app
**no arranca sin `SERVICE_API_TOKEN`** (falla al cargar la configuración si está ausente, vacía
o sólo con espacios).

Cuando AUDA lo implemente, la consecuencia será que mientras Tasks 11-16 no estén hechas, **toda**
emisión de auditoría que este orquestador intente hacer se pierde en silencio. `LogAsync` es
fire-and-forget y no propaga el error al llamador (regla de diseño de la Task 4, que se mantiene).

> Diseño: `tasks/plan.md` §6.

### Task 11: `AUDIT_LOG_API_TOKEN` en config, sin default — #17 / ORC-08.1

**Descripción:** agregar `AuditLogAPIToken` a `internal/config/config.go` y leerlo en `fromEnv`
(`config.go:38-40`) **sin valor por defecto**, junto a `AuditLogBaseURL` (`config.go:24`).

**Criterios de aceptación:**
- [ ] `Config.AuditLogAPIToken string` presente junto a `AuditLogBaseURL`
- [ ] Leído en `fromEnv` **sin default**: ausente o vacío ⇒ `Load()` falla con un error que **nombra** `AUDIT_LOG_API_TOKEN`
- [ ] Token vacío o sólo con espacios tratado como ausente
- [ ] El token nunca aparece en logs, en `fmt.Sprintf("%v", cfg)` ni en mensajes de error

**Verificación:** `go test ./internal/config/...` — `TestLoadReturnsDefaultsWhenEnvironmentIsEmpty`
actualizado (ya no existe default para el token) y un caso nuevo de ausencia del token.

**Dependencias:** ninguna. Primero de la cadena.

**Archivos:** `internal/config/config.go`, `internal/config/config_test.go`

**Tamaño:** XS

### Task 12: `httpclient.New(baseURL, timeout, token)` + `Authorization: Bearer` — #18 / ORC-08.2

**Descripción:** `internal/httpclient/client.go:17` pasa a `(baseURL, timeout, token)` y `Do`
(`:24`) setea `Authorization: Bearer <token>` en toda request.

**Criterios de aceptación:**
- [ ] `New(baseURL string, timeout time.Duration, token string)`
- [ ] `Do` setea `Authorization: Bearer <token>` en cada request construida
- [ ] Token vacío ⇒ el header **no** se setea (no mandar `Bearer ` vacío)
- [ ] La regla de `:39` se mantiene: todo `>= 300` devuelve un body parseable (RFC 9457)

**Verificación:** `go test ./internal/httpclient/...` en verde.

**Dependencias:** Task 11

**Archivos:** `internal/httpclient/client.go`, `internal/httpclient/client_test.go`

**Tamaño:** S

### Task 13: Propagar el token en el composition root — #19 / ORC-08.3

**Descripción:** `cmd/orchestrator/main.go:35` es hoy
`auditlog.NewClient(cfg.AuditLogBaseURL, cfg.HTTPTimeout)`. Pasa el token.

**Criterios de aceptación:**
- [ ] `main.go:35` → `auditlog.NewClient(cfg.AuditLogBaseURL, cfg.HTTPTimeout, cfg.AuditLogAPIToken)`
- [ ] El composition root es el **único** que lee config: ningún paquete bajo `internal/` llama `os.Getenv`
- [ ] El arranque no loguea el token

**Verificación:** `go build ./... && go vet ./...`; grep de `os.Getenv` bajo `internal/` → sin resultados.

**Dependencias:** Task 11

**Archivos:** `cmd/orchestrator/main.go`

**Tamaño:** XS

### Task 14: `auditlog.NewClient` propaga el token al cliente HTTP — #20 / ORC-08.4

**Descripción:** `internal/clients/auditlog/client.go:25` es hoy
`func NewClient(baseURL string, timeout time.Duration) *Client`. Pasa el token a `httpclient.New` (`:26`),
cubriendo los 3 métodos: `Emit` (`:29`), `ListAll` (`:37`) y `ListByChecksum` (`:44`).

**Criterios de aceptación:**
- [ ] `NewClient(baseURL string, timeout time.Duration, token string)`
- [ ] El token viaja a `httpclient.New(baseURL, timeout, token)`
- [ ] `Emit`, `ListAll` y `ListByChecksum` mandan `Authorization: Bearer`
- [ ] El token no se guarda en logs ni en el `Stringer` del client

**Dependencias:** Task 12

**Archivos:** `internal/clients/auditlog/client.go`

**Tamaño:** XS

### Task 15: un `401` es error de cableado — `logger.Error`, sin reintento — #21 / ORC-08.5

**Descripción:** el bucle `emit` (`internal/services/audit_service.go:28-39`) reintenta
`maxEmitAttempts` veces ante cualquier fallo y termina en `logger.Warn`. Un `401` no es
transitorio: significa que el token está mal cableado o ausente.

**Criterios de aceptación:**
- [ ] Un `401` corta el reintento **inmediatamente** (1 solo intento)
- [ ] El `401` se reporta con `logger.Error` (no `logger.Warn`), incluyendo `action` y `checksum`
- [ ] `5xx`, timeout y errores de red **siguen** reintentando `maxEmitAttempts` veces con `logger.Warn`

**Verificación:** `go test ./internal/services/...` — test que verifica exactamente 1 llamada a
`client.Emit` ante `401`, y que un `500` sigue reintentando.

**Dependencias:** Task 14

**Archivos:** `internal/services/audit_service.go`, `internal/services/audit_service_test.go`

**Tamaño:** S

**Nota de diseño:** es un cambio de **diagnóstico**, no de contrato. `LogAsync` sigue siendo
fire-and-forget y no propaga el error al llamador (decisión de la Task 4). Lo que cambia es que
un `401` —que antes se perdía en un `Warn` tras 2 intentos— ahora queda como `Error` con 1 intento.

### Task 16: tests del cliente con el header + caso `401` — #22 / ORC-08.6

**Descripción:** los 5 tests existentes de `internal/clients/auditlog/client_test.go` usan
`NewClient(server.URL, 5*time.Second)` y deben pasar a la firma nueva.

**Criterios de aceptación:**
- [ ] `TestClientEmitPostsJSONEventToAuditLogs` (`client_test.go:31`, `NewClient` en `:69`)
- [ ] `TestClientEmitReturnsProblemOnNon2xx` (`:84`, `NewClient` en `:92`)
- [ ] `TestClientListAllRequestsSkipAndLimitAndDecodesLogs` (`:108`, `NewClient` en `:127`)
- [ ] `TestClientListByChecksumBuildsPathAndDecodesLogs` (`:152`, `NewClient` en `:162`)
- [ ] `TestClientListByChecksumReturnsProblemOnNon2xx` (`:174`, `NewClient` en `:182`)
- [ ] **Nuevo:** `401` sin token — el server mock responde `401` + Problem Details y se verifica que el header `Authorization` no viaja
- [ ] **Nuevo:** `emit` ante `401` hace exactamente 1 intento (Task 15)

**Verificación:** `go vet ./... && go test -race -count=1 ./...` en verde.

**Dependencias:** Tasks 12, 14, 15

**Archivos:** `internal/clients/auditlog/client_test.go`, `internal/services/audit_service_test.go`

**Tamaño:** M

### Task 17: *(opcional)* `ListByChecksum(skip, limit)` — #23 / ORC-08.7

**Descripción:** `internal/clients/auditlog/client.go:44` expone `ListByChecksum(ctx, checksum)`
sin paginación. AUDA **no** aplica `.limit()` a ese cursor por decisión **D12** de su `SPEC.md` §8.1,
así que hoy devuelve el array completo.

**Criterios de aceptación:**
- [ ] Firma → `ListByChecksum(ctx, checksum, skip, limit int)`
- [ ] Query string `?skip=<n>&limit=<n>` sobre `/audit/logs/checksum/{valor}`
- [ ] `dto.AuditQueryParams` (Skip/Limit) se propaga a este método, igual que a `ListAll`
- [ ] Verificar contra AUDA que `skip`/`limit` fuera de rango ⇒ `400`, **no** recorte silencioso

**Dependencias:** Task 14

**Archivos:** `internal/clients/auditlog/client.go`, `internal/clients/auditlog/client_test.go`, `internal/services/audit_service.go`

**Tamaño:** S

**Nota:** opcional por diseño. Sólo implementarlo si el volumen real por checksum lo justifica.
**No bloquea el despliegue.**

### Checkpoint: Handoff Audit Log — DESBLOQUEADO (listo para ejecutar)
- [ ] **Desbloqueado: AUDA ya exige el auth Bearer** (ver el aviso al inicio de la Fase 4)
- [ ] `AUDIT_LOG_API_TOKEN` sin default: el orquestador no arranca sin él, con un error que nombre la variable
- [ ] `Authorization: Bearer` presente en **todas** las requests de `auditlog.Client` (`Emit`, `ListAll`, `ListByChecksum`)
- [ ] El token no aparece en logs, ni en `fmt.Stringer`, ni en mensajes de error
- [ ] Un `401` del MS Audit Log ⇒ `logger.Error`, **sin reintento**
- [ ] Los 5 tests existentes de `client_test.go` pasan con el header
- [ ] Hay test para el `401` sin token
- [ ] `go build ./...`, `go vet ./...` y `go test -race -count=1 ./...` limpios
- [ ] E2E contra el MS Audit Log real: `POST /audit/logs` ⇒ `201` y el evento aparece en `GET /audit/logs`
- [x] Revisión con el humano

---

## Fase 5: Contrato real del Extract + markdown + verificación de texto — IMPLEMENTADA, EN REVISIÓN

> Tasks 18-27 hechas y verificadas contra el Extract real. Queda la revisión del humano y tres
> puntos de Task 27 marcados como NO hechos (ver abajo).

**Contexto (dos cambios de upstream, verificados contra el código):**

1. **El Extract cambió su contrato de forma incompatible.** El servicio (`Conversor/extract-markdown-microservice-pdf-extractext`)
   fue reescrito en Go y ahora devuelve `{content, page_count}`: **`text` → `content`, y `pages[]`
   + `duration_ms` fueron eliminados**. Nuestro `dto.ExtractedDocument` sigue pidiendo `json:"text"`,
   y como `encoding/json` **ignora los campos desconocidos**, hoy compila, pasa todos los tests y
   devuelve `Text == ""` **sin error** ⇒ `checksum.Of("")` para todos los PDFs. Es un fallo
   silencioso con datos corruptos, y por eso la Task 18 va primera.
2. **El requisito de markdown.** El Extract sólo cruza un `string` plano por su contrato: **no**
   envía tamaño de fuente, peso ni coordenadas, así que desde el orquestador **no se puede**
   detectar títulos ni estructura reales. Lo que sí se puede es corregir texto: des-hifenizar,
   unir párrafos, quitar headers/pies repetidos y promover títulos por mayúsculas.

**Decisiones tomadas por el humano (06-10-2026):**

| Decisión | Elección | Por qué |
|----------|----------|---------|
| ¿Dónde se convierte a markdown? | En el orquestador | El Extract no manda datos de layout. Su equipo puede exponerlos después (ver `plan.md` §7.1). |
| ¿Se toca el repo del Extract? | **No** | Trabajo de otro equipo. Acá sólo se deja escrito el contrato requerido. |
| ¿De qué se calcula el checksum? | **Del markdown** | Preserva el invariante `checksum == SHA-256(text persistido)`. |
| ¿Cómo se llama el campo? | **`text`, con markdown adentro** | Renombrarlo obligaría a revisar el invariante en todos los clientes. |
| ¿Transformaciones? | Las 4: des-hifenización, unwrap, headers/pies, títulos por mayúsculas | Todo lo que es determinable sin layout. |
| ¿Verificación de contenido? | **Sí**: si no hay texto ⇒ `422` | Un escaneado devolvería `SHA-256("")`, colisión de ID garantizada. |

**Orden de ejecución:** `18 → 19 → {20, 21} → 22 → 23 → {24, 25} → 26 → 27`. La 18 va primera
por ser el bug vivo; la 19 es la base de la que dependen 20 y 21 (que sí pueden ir en paralelo).

### Task 18: `ExtractedDocument` al contrato real del Extract

**Descripción:** `internal/dto/pdf.go` tiene `Text string \`json:"text"\``, `Pages []ExtractedPage`
y `DurationMs uint64`, pero el Extract manda `content` y nada más. Alinear el DTO con el contrato
real y borrar los campos que upstream ya no envía.

**Criterios de aceptación:**
- [x] `dto.ExtractedDocument` queda como `{ PageCount int \`json:"page_count"\`; Content string \`json:"content"\` }`
- [x] Se borra el tipo `dto.ExtractedPage` (upstream ya no manda `pages[]`)
- [x] `dto.ExtractedDocument.Pages` y `.DurationMs` desaparecen
- [x] `dto.ExtractPDFResponse` **NO se toca**: sigue `{checksum, page_count, text}` — es nuestro contrato de salida
- [x] `internal/clients/extract/client.go` **NO se toca**: sigue siendo un espejo fiel de upstream

**Verificación:**
- [x] `go build ./...` — falla en los sitios que hay que actualizar (§Tests), que es lo esperado
- [x] `grep -rn 'ExtractedPage\|DurationMs\|json:"pages"' internal/` → sin resultados
- [x] El fallo de build apunta **sólo** a tests y a `pdf_service.go`: confirma que ningún
      `internal/` de producción quedó con el contrato viejo

**Dependencias:** ninguna. **Primera de la cadena: es el bug silencioso que está vivo hoy.**

**Archivos:** `internal/dto/pdf.go`

**Tamaño:** XS

### Task 19: `internal/markdown` — núcleo (split de páginas + unwrap + des-hifenización)

**Descripción:** crear el paquete puro `internal/markdown` con el núcleo del pipeline: normalizar
saltos de línea, partir páginas por `\n\n`, des-hifenizar y unir párrafos. Sin títulos, sin
headers/pies y sin escapado todavía (esas son las Tasks 20 y 21).

**Criterios de aceptación:**
- [x] Paquete nuevo `internal/markdown`, sin dependencias ni I/O (igual que `internal/checksum`)
- [x] `func Convert(content string) string` exportada
- [x] Normaliza `\r\n` → `\n` antes de partir (pdf_oxide usa `\r\n` *dentro* de una página)
- [x] Parte por `\n\n` (el Extract une páginas con `\n\n` ⇒ delimitador **no ambiguo**) y descarta segmentos vacíos
- [x] Des-hifeniza **sólo** si la línea termina en `-` y la siguiente empieza en minúscula
- [x] Unwrap: une las líneas de un párrafo con un espacio simple; un salto en blanco separa párrafos
- [x] `Convert("")`, `Convert("   \n\n  ")` y `Convert` de un solo segmento devuelven `""` (nunca panic)
- [x] `Convert` es **determinista**: dos llamadas con la misma entrada dan el mismo string

**Verificación:**
- [x] `go test ./internal/markdown/...` en verde, con tests table-driven y **goldens** (string entrada → string esperado exacto)
- [x] Golden real: un fragmento con guiones de corte de línea de pdf_oxide, esperado written a mano
- [x] `go build ./... && go vet ./...`

**Dependencias:** ninguna. Es la base de las Tasks 20, 21 y 22.

**Archivos:** `internal/markdown/convert.go`, `internal/markdown/convert_test.go`

**Tamaño:** M

**Nota de diseño:** el orden de las etapas NO es libre. La des-hifenización tiene que ver el
"-\n" de la línea siguiente, así que va **antes** del unwrap: si el unwrap corriera primero, el
guion dejaría de estar pegado a un fin de línea y la señal se pierde.

### Task 20: `internal/markdown` — títulos por mayúsculas + escapado mínimo

**Descripción:** agregar la clasificación de líneas en mayúsculas (promovidas a `## `) y el
escapado de caracteres con sintaxis markdown. Ambas etapas van **antes** del unwrap de la Task 19,
porque después de unir párrafos ya no hay estructura de línea.

**Criterios de aceptación:**
- [x] Una línea se vuelve `## <texto>` si: tiene 3-120 caracteres, ≥2 letras, ≥60 % de sus letras en
      mayúscula, no termina en `.`/`,`/`;` y no tiene más de ~12 palabras
- [x] El texto del título se conserva **tal cual** (no se title-casea: rompería acentos y nombres
      propios en español — `ÍA`, `Ñ`, `JR`). Pregunta abierta 12 de `plan.md`
- [x] Una línea de sólo dígitos **no** es título (es el pie de página típico, no un encabezado)
- [x] Escape mínimo en cuerpo y títulos: `` \ ` `` , `*`, `_`, `[`, `]`, `<`, `>`, y al inicio de
      línea también `#`, `>`, `|`, `+`/`-` seguidos de espacio
- [x] El escape es una función aparte y testeable por separado (no inline en el pipeline)

**Verificación:**
- [x] `go test ./internal/markdown/...` en verde: casos que SÍ son título, casos que NO lo son
      (frase en minúsculas, URL, línea con punto final, sólo dígitos), y casos de escapado
- [x] Un golden donde el texto crudo tiene `C++`, `snake_case` y `*asteriscos*` → el markdown
      resultante no los interpreta como énfasis/lista

**Dependencias:** Task 19

**Archivos:** `internal/markdown/convert.go`, `internal/markdown/escape.go`, `internal/markdown/*_test.go`

**Tamaño:** S

**Nota de diseño:** el escapado es lo que evita que el markdown "destruya" texto técnico: sin él,
`snake_case` se renderiza en cursiva y un `*` suelto abre una lista. Es el motivo de que el
markdown crudo se vea más ruidoso que el texto original.

### Task 21: `internal/markdown` — headers/pies repetidos entre páginas

**Descripción:** quitar la primera y última línea de cada página cuando se repiten en la mayoría
de las páginas (números de página, títulos de cabecera). Es la única etapa que necesita mirar
**todas** las páginas antes de transformar una sola.

**Candidatos y criterio (DESVIADO del plan original, ver nota):**
- [x] Se comparan la 1ª y la última línea de cada segmento, normalizadas para el match
- [x] **Fail-seguro, sin umbral:** un borde sólo se borra si aparece en el mismo borde de **todos** los
      segmentos elegibles. El plan original pedía ≥60 % (`count*5 >= total*3`, mínimo 3 páginas) y se
      cambió tras los goldens de la Task 27: como el `"\n\n"` no marca páginas de forma fiable, un
      umbral porcentual habría permittede borrar contenido legítimo. Ante la duda no se borra nada.
- [x] **Mínimo 2 segmentos** para considerar repetición (el original pedía 3)
- [x] **Guard obligatorio:** si al quitar el header un segmento queda vacío, se restaura el segmento
      original. Sin este guard, un PDF cuya única línea sea el título se queda sin contenido
- [x] La lista de repetidos se ordena antes de aplicarse (determinismo: el checksum es el ID del sistema)

**Verificación:**
- [x] Golden: 5 páginas con `"Scrum Guide"` arriba y `"3"` abajo ⇒ esas dos líneas desaparecen
- [x] Test del guard: página única con el header repetido ⇒ **no** se borra nada
- [x] Test de abstain: header en sólo algunos segmentos ⇒ **no** se borra nada (es el caso que motivó el cambio)
- [x] Test de página cuyo contenido es sólo el header ⇒ el contenido **no** desaparece
- [x] Mutation testing: se-verifica-todas-las-páginas, se-borra-cualquiera y cambia-la-primera-línea se detectaron

**Dependencias:** Task 19

**Archivos:** `internal/markdown/convert.go`, `internal/markdown/convert_test.go`

**Tamaño:** S

### Task 22: Verificación de contenido — un PDF sin texto da `422`

**Descripción:** un PDF escaneado es un PDF válido: tiene páginas pero su contenido son imágenes, y
el Extract devuelve `content: ""` con `page_count > 0`. Sin verificación, el orquestador responde
`200` con `text: ""` y el checksum `SHA-256("")` — **compartido por todos los escaneados**. Es una
colisión de ID garantizada. Agregar la regla en la capa de servicio.

**Criterios de aceptación:**
- [x] `services.ErrNoExtractableText` declarado junto a `ErrInvalidPDF` en `internal/services/pdf_service.go`
- [x] Si `strings.TrimSpace(document.Content) == ""` ⇒ se devuelve ese error **antes** de convertir y
      **antes** de calcular el checksum
- [x] `page_count == 0` con contenido vacío también ⇒ mismo error
- [x] **No** se emite evento de auditoría cuando se rechaza (no hay documento que auditar)
- [x] `pdf_handler.go` mapea `ErrNoExtractableText` → **`422`** con un `detail` que explique que el
      PDF no tiene capa de texto (típicamente un escaneado)
- [x] `ErrInvalidPDF` sigue → `400`. Los dos errores quedan **distinguibles** por `errors.Is`
- [x] El chequeo va en la **capa de servicio**, no en el handler ni en el client

**Verificación:**
- [x] Test de service: `Content == ""` ⇒ `ErrNoExtractableText`; `Content == "  \n\n "` (sólo whitespace) ⇒ igual
- [x] Test de handler: `422` + `application/problem+json`; y `400` para un body sin `%PDF-`
- [x] Test que confirma que `stubAudit` **no** recibió ningún evento en el camino del `422`
- [x] `go test ./internal/services/... ./internal/handlers/...`

**Dependencias:** Task 18

**Archivos:** `internal/services/pdf_service.go`, `internal/services/pdf_service_test.go`, `internal/handlers/pdf_handler.go`, `internal/handlers/pdf_handler_test.go`

**Tamaño:** S

**Nota de diseño:** `422` y no `400` a propósito. `400` significa "esto no es un PDF"; `422`
significa "esto es un PDF perfectamente válido cuyo contenido no se puede procesar". Un escaneado
no es un request mal formado, así que `400` mentiría.

**Fuera de alcance (pregunta abierta 9 de `plan.md`):** un PDF con 1 página de texto entre 90
escaneadas **pasa** el chequeo global. El dato para detectarlo existe (`page_count` vs. cantidad de
segmentos con texto) pero el umbral es decisión de producto y **esta Fase no lo implementa**.

### Task 23: Cablear `markdown.Convert` y mover el checksum al markdown

**Descripción:** conectar el pipeline en `PDFService.IngestAndExtract`. Hoy el servicio calcula
`checksum.Of(document.Text)` sobre el texto crudo; debe calcularlo sobre el markdown, que es lo que
se devuelve y lo que el cliente persiste.

**Criterios de aceptación:**
- [x] `md := markdown.Convert(document.Content)` después de la verificación de contenido
- [x] `Checksum: models.Checksum(checksum.Of(md))` y `Text: md`
- [x] Se mantiene el invariante: `checksum == SHA-256(response.Text)` — hay un test que lo asserta
- [x] `page_count` se sigue copiando del Extract, sin transformar
- [x] La auditoría sigue emitiendo `Details: {page_count}` y el **mismo** checksum de la respuesta
- [x] `IngestAndExtract` no cambia de firma ni de interfaz

**Verificación:**
- [x] `go test ./internal/services/...` en verde. Ojo: los tests existentes assertan el checksum del
      **texto crudo** ⇒ hay que actualizarlos al markdown esperado
- [x] Test explícito: `checksum.Of(resp.Text) == resp.Checksum`
- [x] Un texto crudo feo (`"linea 1-\nlinea 2"`) produce un markdown más lindo **y** el checksum del markdown

**Dependencias:** Tasks 19, 20, 21

**Archivos:** `internal/services/pdf_service.go`, `internal/services/pdf_service_test.go`

**Tamaño:** S

### Task 24: Contract test del Extract con la forma real + regresión del decode silencioso

**Descripción:** los 6 tests de `internal/clients/extract/client_test.go` mockean la forma **old**
(`text`/`pages`/`duration_ms`) y por eso pasaban en verde mientras producción estaba rota. Actualizarlos
y agregar el test que habría atrapado el bug.

**Criterios de aceptación:**
- [x] Los fixtures usan `{"content":"...","page_count":N}`
- [x] Se eliminan las aserciones sobre `Pages` y `DurationMs`
- [x] **Nuevo test de regresión:** un servidor mock que responde **exactamente** la forma real del
      Extract debe producir `Content` **no vacío** y `PageCount` correcto. Este test es el que
      falla con el DTO viejo
- [x] Los tests que no dependan de la forma de la respuesta (raw PDF body, `application/pdf`,
      timeout, transport failure, Problem en non-2xx) quedan **sin tocar**
- [x] Ningún test del repo mockea `pages`/`duration_ms`/`text` como si fuera la respuesta del Extract

**Verificación:**
- [x] `go test ./internal/clients/extract/...` en verde
- [x] Se comprobó que el test de regresión **fallaría** con el DTO viejo (revertir el tag a
      `json:"text"` debe romperlo) — si no falla, el test no está probando nada
- [x] `grep -rn 'duration_ms\|"pages"\|page_number' internal/` → sin resultados

**Dependencias:** Task 18

**Archivos:** `internal/clients/extract/client_test.go`, `internal/server/integration_test.go`

**Tamaño:** S

### Task 25: Puertos y timeouts — Extract 8080, AuditLog 8081

> **Corregida por la Task 32 (#32):** el puerto real de AUDA es **`:8083`**, no `:8081` (ese `EXPOSE`
> quedó viejo). El `:8081` que fijó esta task se reemplaza en la Fase 7.

**Descripción:** hay tres valores inconsistentes de base URL y un timeout que corta antes que el
Extract. Alinearlos.

**Criterios de aceptación:**
- [x] `defaultExtractBaseURL` → `http://localhost:8080` (el Extract nuevo escucha en 8080, no 8081)
- [x] `defaultAuditLogBaseURL` → `http://localhost:8081` (coincide con el `EXPOSE 8081` del Dockerfile de AUDA)
- [x] `compose.yaml`: `EXTRACT_BASE_URL` → puerto **8080** (hoy dice 8000, que no existe)
- [x] `HTTP_TIMEOUT` → **35s**: el Extract tiene un deadline interno de 30s y devuelve un
      `503 "context deadline exceeded"` bien tipado. Con 10s cortamos nosotros primero y devolvemos
      un error opaco en vez del `503` real
- [x] `defaultMaxPDFSizeBytes` se queda en 15MB: es más estricto que los 50MB del Extract, así que el
      nuestro dispara primero y no hay que tocarlo

**Verificación:**
- [x] `go test ./internal/config/...` en verde (el test de defaults hay que actualizarlo)
- [x] `docker compose config` valida
- [x] Documentado que en **dev local** el Extract tomó el 8080 y el orquestador tiene
      `defaultPort = "8080"` ⇒ hay que setear `PORT` explícitamente para el orquestador (pregunta
      abierta 11). Se anota en el README, no se cambia el default

**Dependencias:** ninguna. Independiente del resto.

**Archivos:** `internal/config/config.go`, `internal/config/config_test.go`, `compose.yaml`

**Tamaño:** XS

### Task 26: Integration test al contrato nuevo

**Descripción:** `internal/server/integration_test.go` monta el stack real contra 3 mocks HTTP y hoy
mockea la forma vieja del Extract. Actualizarlo y reforzar las aserciones para que cubra la
regresión a nivel de stack completo.

**Criterios de aceptación:**
- [x] `extractMock` responde `{"content":"texto integrado del pdf","page_count":3}` (sin `pages`, sin `duration_ms`)
- [x] El flujo sigue cubriendo: extract → create → find → update → delete → 404 → 4 eventos de auditoría
- [x] **Aserción nueva:** el checksum de la respuesta es el SHA-256 **del markdown devuelto**, no del crudo
- [x] **Aserción nueva:** un Extract que devuelve `content: ""` ⇒ el router responde **`422`**, y
      `store.actions()` queda vacío (no se auditó nada)
- [x] `dto.ExtractedPage` y `DurationMs` desaparecen del mock

**Verificación:**
- [x] `go test ./internal/server/...` en verde
- [x] `go test -race -count=1 ./...` en verde (el `LogAsync` es una goroutine; el test debe ser race-clean)

**Dependencias:** Tasks 22, 23

**Archivos:** `internal/server/integration_test.go`

**Tamaño:** S

### Task 27: E2E contra el Extract real + validación de los goldens

**Descripción:** la Fase 5 no se da por buena hasta correr contra el Extract nuevo de verdad. Los
goldens de markdown se validan contra PDFs reales, no inventados: el corpus está en
`tests/stress/pdfs/` (Scrum Guide 16 págs, Essential Kanban 90 págs y 8.5MB, Filosofía Lean
42 págs, scrum_manager 62 págs).

**Criterios de aceptación:**
- [x] El Extract levanta en `:8080` (vía `docker compose`; corre en el contenedor `app`)
- [x] `POST /api/v1/pdfs/extract` con un PDF real ⇒ `200`, `text` no vacío, y **byte a byte igual al golden**
- [x] `checksum` == SHA-256 del `text` devuelto (verificado con `hashlib`, no a ojo)
- [x] Los goldens se ajustan con **salida real** del corpus. Confirmado que el criterio fail-seguro
      no borra contenido legítimo: `TestConvertNeverLosesWordsFromRealPDFs` pasa en los 4 PDFs
- [x] Markdown legible: sin `\r` ni NBSP residuales, párrafos unidos, títulos separados del cuerpo
- [ ] **NO hecho:** `GET /api/v1/audit/logs` con el evento `pdf.extract`. Requiere el mock de Audit Log
      levantado; en la corrida real la auditoría falló con `connection refused` y el request igual
      devolvió `200`, lo que **sí** verificó en producción el diseño fire-and-forget
- [x] **Escaneado real ⇒ `422`**, hecho después. El corpus no traía ninguno, así que se generó uno
      con PIL (imagen rasterizada, sin `/Font`, sólo `/Image`). El Extract real devolvió
      `{"content":"","page_count":1}` y el orquestador `422` con **cero** intentos de auditoría
      (0 menciones en el log, contra 1 en el PDF con texto). Detalle y snippet en `plan.md` §7.3.1
- [ ] **NO hecho:** orquestador real contra mocks de Audit Log y Persistence. La corrida usó las URLs
      reales sin esos mocks; el flujo de extract no invoca Persistence (el cliente persiste vía CRUD)

**Salida real (Scrum Guide, 16 págs):**
```
healthz HTTP 200
POST /api/v1/pdfs/extract -> HTTP 200   duration_ms=54
checksum declarado : f9408973cf649cf1f60bdfc15d99053621173e9c5179980982fc21f954c69fc9
sha256(text)       : f9408973cf649cf1f60bdfc15d99053621173e9c5179980982fc21f954c69fc9
coinciden          : True
igual al golden   : True      (34800 bytes)
NBSP residuales: 0    CRLF residuales: 0
```

**Hallazgo de esta task que cambió el código (no sólo los goldens):** el plan afirmaba que `"\n\n"`
era un delimitador de página inequívoco. Los 4 PDFs lo refutaron — en 3 de 4 casi todos los `"\n\n"`
son saltos de párrafo interno. Se añadió la separación de títulos pegados al cuerpo por ancho de
renglón; sin ella el Kanban (90 págs, cero renglones vacíos) salía como ~89 párrafos gigantes.
Detalle en `plan.md` §7.4.1.

**Verificación:**
- [x] `go build ./... && go vet ./... && go test -race -count=1 ./...` limpios al final
- [x] `go test -tags golden -run TestUpdateGoldens` regenera los `.md` bajo build tag, así que el
      flag no se puede colar en una corrida normal
- [x] Goldens byte a byte iguales a lo que devuelve el E2E real

**Dependencias:** Tasks 24, 25

**Archivos:** sólo tests/verificación. Si el E2E destapa un problema de conversión, se vuelve a la
Task 19/20/21 con un golden nuevo que lo cubra.

**Tamaño:** S

### Checkpoint: Extract contract + markdown
- [x] `dto.ExtractedDocument` calca el contrato real: `content` + `page_count`, sin `pages`, sin `duration_ms`
- [x] El test de regresión del decode silencioso existe **y falla** con el DTO viejo
- [x] `Convert` es determinista: misma entrada → mismo string (test explícito)
- [x] `Convert("")` no entra en panic
- [x] PDF sin texto ⇒ **`422`**, sin evento de auditoría emitido
- [x] PDF sin firma `%PDF-` ⇒ `400` (sigue igual)
- [x] `checksum == SHA-256(text devuelto)` en el test de service y en el de integración
- [x] Puertos alineados: Extract 8080, AuditLog 8081, `HTTP_TIMEOUT` 35s
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] E2E con PDF real: markdown legible y checksum correcto
- [x] `plan.md` y `todo.md` actualizados con lo que se discoverió en la implementación
- [x] Revisión con el humano

---

## Fase 6: Stress y pruebas de carga — IMPLEMENTADA

> **Nota (09-10-2026):** la capa de stress Go (`-tags stress`) que describe esta fase se eliminó
> en la Fase 8 (decisión del humano). Se conserva como registro histórico de lo que se midió; el
> stress vigente son `scripts/k6/spike.js` y `scripts/vegeta/attack.sh`.

Objetivo: aplicar carga real y comprobar que los invariantes aguantan, en el mismo
espíritu que las pruebas de carga del MS Extract (`scripts/vegeta/attack.sh` y
`scripts/k6/spike.js` de ese repo), pero atacando lo que **este** servicio puede
romper. El Extract es CPU-bound y su carga mide latencia; el orquestador es un
proxy de I/O, así que lo que se mide acá es corrección bajo concurrencia.

- [x] Tests Go bajo `-tags stress`, para que `go test ./...` no se vuelva lento
- [x] `checksum == SHA-256(text)` verificado en **cada** respuesta bajo 64 workers
- [x] El mock de Extract deriva la respuesta del body del request; con respuesta
      fija una contaminación entre requests pasaría inadvertida
- [x] Determinismo: 200 repeticiones del mismo PDF dan el mismo checksum
- [x] Los 7 endpoints ejercitados en paralelo, no sólo un camino caliente
- [x] Fuga de goroutines medida contra baseline post-warmup (keep-alive no se va)
- [x] 13 casos de límite y error, con carga en background, incluidos el borde
      exacto de 15MB (`maxSize` vs `maxSize+1`)
- [x] Traducción de fallos del Extract (400/500/503/501 → 502) y sin auditoría
- [x] Timeout del cliente → 502 con cuerpo, sin conexión colgada
- [x] PDF escaneado bajo carga → siempre 422 y cero auditoría
- [x] `scripts/k6/spike.js` re-hashea cada respuesta (métrica explícita, no `check`)
- [x] `scripts/vegeta/attack.sh` con el perfil de referencia del enunciado
- [x] Los dos tests con mutación para demostrar que fallan ante un bug real
- [x] Documentado en `tests/stress/README.md` con los resultados medidos

**Resultados:** spike de 100 VUs / 40 s → 1343 requests, `checksum_matches` 100%,
`extract_ok` 100%, `http_req_failed` 0%, p95 4.84 s. vegeta 30 s → 971 requests,
100% éxito. Techo medido **~33 req/s** con el corpus completo (3.3 GB / 30 s de
egreso ≈ 111 MB/s), degradando **por cola y sin errores**, no por colapso.

**Dos defectos de producción que encontraron, no los tests:**

1. **Espacios duros internos en el markdown.** El spike marcó 75% de respuestas
   con artefactos = exactamente 1 de los 4 documentos: `Essential Kanban` traía
   seis U+00A0 dentro de las líneas. `normalizeSpaces` sólo comparaba con `' '` y
   `'\t'`; ahora usa `unicode.IsSpace`. Fijado por
   `TestConvertOnRealCorpusHasNoUnicodeSpaces`.
2. **404 y 405 no eran problem documents.** chi devolvía texto plano o cuerpo
   vacío, así que un cliente parseaba dos formatos para el mismo tipo de error
   que el resto de la API sí emite como RFC 9457. `Routes` ahora define
   `NotFound` y `MethodNotAllowed`.

**Lo que NO mide este perfil:** capacidad real en números, porque el techo está
en el egreso de PDFs de 3.3 MB de promedio. Con un corpus de documentos chicos
el techo es otro y hace falta medirlo antes de afirmar un número. El Extract
tiene sus propias pruebas de carga; acá sólo se demuestra que el orquestador no
agrega degradación propia ni rompe el checksum.

---

## Fase 7: Integración con el MS Persistence real — EN CURSO (issues #24-#33)

> Persistence ya está implementado, pero con un contrato distinto al que asumió la Fase 3:
> rutas `?checksum=` (query param, no path), auth Bearer y puerto `:8000` (no `:8082`). El
> handoff de origen está en `persistence/PersistenceMicroservices-pdf-extractext/tasks/todo.md`
> § *Pendiente de tu integración (orquestador Go)*. Contrato real: `tasks/plan.md` §8.
>
> **Orden:** `{28, 32} → 29 → {30, 31} → 33 → 34 → {35, 36}`. La Task 28 (rutas) y la 32 (puertos)
> son independientes del token. La base `httpclient.New(baseURL, timeout, token)` es de la Fase 4
> (#18); la Task 29 reutiliza esa firma.

### Task 28: Rutas del client a `?checksum=` (query param) — #28 / ORC-09.4

**Descripción:** `clients/persistence` usa hoy `/texts/{checksum}` (path param), pero Persistence
expone `/texts?checksum=...`. Corregir `Update`, `Delete` y `FindByChecksum`.

**Criterios de aceptación:**
- [x] `Update` → `PUT /texts?checksum=<url.QueryEscape(cs)>`
- [x] `Delete` → `DELETE /texts?checksum=<url.QueryEscape(cs)>`
- [x] `FindByChecksum` → `GET /texts?checksum=<url.QueryEscape(cs)>`
- [x] Se usa `url.QueryEscape`, no `url.PathEscape`
- [x] `POST /texts` sin cambios

**Verificación:** contract tests que fijen path+query exactos y un checksum con caracteres especiales
(`go test ./internal/clients/persistence/...`).

**Dependencias:** ninguna.

**Archivos:** `internal/clients/persistence/client.go`, `internal/clients/persistence/client_test.go`

**Tamaño:** S

### Task 29: `PERSISTENCE_API_TOKEN` + propagación en `persistence.Client` — #25,#26,#27 / ORC-09.1-3

**Descripción:** Persistence exige `Authorization: Bearer <SERVICE_API_TOKEN>` en toda ruta salvo
`/health` y `/readyz`. Agregar el segundo secreto al orquestador (separado de `AUDIT_LOG_API_TOKEN`)
y propagarlo por el composition root al `httpclient` compartido.

**Criterios de aceptación:**
- [x] `Config.PersistenceAPIToken` **sin default** (ausente/vacío ⇒ `Load()` falla nombrando `PERSISTENCE_API_TOKEN`)
- [x] `persistence.NewClient(baseURL, timeout, token)` pasa el token a `httpclient.New`
- [x] `cmd/orchestrator/main.go` propaga `cfg.PersistenceAPIToken`
- [x] `Create`, `Update`, `Delete` y `FindByChecksum` mandan `Authorization: Bearer`
- [x] Token vacío ⇒ no se setea el header; el token nunca se loguea
- [x] `AUDIT_LOG_API_TOKEN` y `PERSISTENCE_API_TOKEN` son variables **separadas**

**Verificación:** mock que exige el header y responde `401` sin él; `grep -rn 'os.Getenv' internal/`
sin resultados.

**Dependencias:** **#18 (Fase 4)** para la firma `httpclient.New(..., token)`.

**Archivos:** `internal/config/config.go`, `internal/config/config_test.go`,
`internal/clients/persistence/client.go`, `cmd/orchestrator/main.go`

**Tamaño:** S

### Task 30: semántica de update parcial en `dto.UpdateTextRequest` — #29 / ORC-09.5

**Descripción:** `PUT /texts` de Persistence rechaza con `400 "no changes requested"` un body sin
cambios efectivos; además trata `null` como campo **ausente** y `""`/`{}` como **limpiar**. Hoy el
orquestador siempre manda ambos y como `string`, sin distinguir "no enviar" de "enviar vacío".

**Decisión adoptada:** modelar `Name *string` y `Metadata map[string]any` **sin `omitempty`**
(nil ⇒ `null` ⇒ sin cambio; `""`/`{}` ⇒ limpiar). Es simétrico y preserva ambas semánticas, cosa
que `omitempty` no logra (con `omitempty` un `""` se omite y no se puede limpiar).

**Criterios de aceptación:**
- [x] `name` es `*string`; `metadata` es `map[string]any`; sin `omitempty`
- [x] Un `PUT` que sólo cambia `metadata` manda `name: null` (no `""`)
- [x] `name:""` limpia (se serializa `""`, no se omite) y `metadata:{}` limpia

**Verificación:** `go test ./internal/clients/persistence/...` (`TestClientUpdateSerializesOmittedAndClearedFields`)
y `./internal/services/... ./internal/handlers/...`.

**Dependencias:** ninguna.

**Archivos:** `internal/dto/text.go`, tests de `handlers`/`services`/`persistence`

**Tamaño:** XS

### Task 31: un `401` de Persistence es error de cableado — #30 / ORC-09.6

**Descripción:** `text_handler.writeServiceError` reenvía el status del `Problem` de Persistence, así
que un `401` del MS se propagaría como `401` al cliente del orquestador. Un `401` es un error de
configuración (token mal cableado), no del caller.

**Criterios de aceptación:**
- [x] `401` de Persistence ⇒ `502` con detalle de configuración, **no** `401`
- [x] Diagnóstico cubierto por el middleware `withRequestLog` (loguea `method`, `path` —con el
      checksum— y `status`), por eso el handler no inyecta un logger propio (SRP)
- [x] `400`/`404`/`409` siguen propagándose igual

**Verificación:** `go test ./internal/handlers/...` con un mock que responde `401`
(`TestTextHandlerFindReturns502WhenPersistenceRejectsCredentials`).

**Dependencias:** Task 29.

**Archivos:** `internal/handlers/text_handler.go`, `internal/handlers/text_handler_test.go`

**Tamaño:** S

### Task 32: alinear base URLs/puertos — Persistence `:8000`, AUDA `:8083` — #32 / ORC-09.8

**Descripción:** los defaults apuntan a puertos inexistentes (`:8082` Persistence, `:8081` AUDA).

**Criterios de aceptación:**
- [x] `defaultPersistenceBaseURL` → `http://localhost:8000`
- [x] `defaultAuditLogBaseURL` → `http://localhost:8083`
- [x] `compose.yaml`: `PERSISTENCE_BASE_URL` → `8000`, `AUDIT_LOG_BASE_URL` → `8083`
- [x] `config_test.go` actualizado; `docker compose config` valida (con los tokens seteados)

**Verificación:** `go test ./internal/config/...`; `docker compose config`.

**Dependencias:** ninguna. Independiente.

**Archivos:** `internal/config/config.go`, `internal/config/config_test.go`, `compose.yaml`

**Tamaño:** XS

### Task 33: reescribir tests de `persistence/client_test.go` + caso `401` — #31 / ORC-09.7

**Descripción:** los tests actuales mockean las rutas viejas. Alinearlos al contrato real.

**Criterios de aceptación:**
- [x] Rutas de los tests a `?checksum=`
- [x] Header `Authorization` presente con token y ausente sin él
- [x] `401` del mock traducido a error tipado (`httpclient.Problem` + `IsUnauthorized`)
- [x] `integration_test.go` con el mock de Persistence en rutas nuevas + header (rechaza sin token)

**Verificación:** `go vet ./... && go test -race -count=1 ./...` en verde.

**Dependencias:** Tasks 28, 29, 31.

**Archivos:** `internal/clients/persistence/client_test.go`, `internal/server/integration_test.go`

**Tamaño:** M

### Task 34: E2E contra Extract + Persistence reales — #24 / ORC-09

**Descripción:** levantar el orquestador contra el Extract real (`:8080`) y Persistence real
(uvicorn `:8000` + Mongo + `SERVICE_API_TOKEN`) y ejercitar el CRUD delegado.

**Criterios de aceptación:**
- [x] `POST /api/v1/pdfs/extract` → `200` con checksum
- [x] `POST/GET/PUT/DELETE /api/v1/texts...` funcionan contra Persistence real (con token)
- [x] Sin `PERSISTENCE_API_TOKEN` el orquestador **no arranca** (`exit=1`, `ERROR ... must be set`)
- [x] Auditoría E2E **no** se verifica todavía (AUDA sin endpoints): anotarlo, no bloquear

**Verificación:** corrida manual documentada (2026-10-07).

**Corrida E2E (real):**
- Extract real detrás de Traefik (`127.0.0.1:8080`), Mongo desde
  `Documents/Services/mongodb` (red `mired`, `:27017`), Persistence `uvicorn :8000`
  (`MONGO_URI=mongodb://<user>:<pass>@localhost:27017/?authSource=admin`,
  `SERVICE_API_TOKEN=…`), orquestador `:8099` (`PERSISTENCE_API_TOKEN=…`).
- `POST /api/v1/pdfs/extract` con el PDF real → `200`, `page_count=16`, checksum
  `f9408973…c69fc9`; `checksum == SHA-256(text)`.
- `POST /api/v1/texts` → `201`; `GET` → `200` (documento completo desde Mongo).
- `PUT` sólo `name` (metadata `null`) preserva `metadata`; `PUT` sólo `metadata` preserva `name`;
  `PUT {}` → `400 "no changes requested"` (propagado de Persistence); `PUT {"name":""}` limpia.
- `DELETE` → `200`; `GET` posterior → `404`.
- Token inválido (`PERSISTENCE_API_TOKEN=WRONG_TOKEN`) → el orquestador responde `502`
  (no `401`) con `detail` de configuración; el `withRequestLog` registra
  `method`/`path` (con checksum)/`status`.

**Dependencias:** Tasks 28-33 + Extract real.

**Archivos:** sólo verificación / `tests/`

**Tamaño:** S

### Task 35: contrato de persistencia en `plan.md`/`README` — #33 / ORC-09.9

**Descripción:** documentar el contrato real (`?checksum=`, `TextOut`, puertos, dos tokens).

**Criterios de aceptación:**
- [x] `README.md`: tabla de variables con ambos tokens y puertos `8000`/`8083`
- [x] `tasks/plan.md` §8 y `tasks/todo.md` Fase 7 actualizados
- [x] Anotado que Persistence/AUDA exponen `/health` (no `/healthz`)

**Dependencias:** Tasks 28-34.

**Archivos:** `README.md`, `tasks/plan.md`, `tasks/todo.md`

**Tamaño:** S

### Task 36: *(opcional)* adaptar `tests/stress` al token — #24

**Descripción:** los tests bajo `-tags stress` y los mocks (`tests/stress/README.md`, k6/vegeta)
deben mandar/aceptar el header si se ejecutan contra los MS reales con auth.

**Criterios de aceptación:**
- [~] Los mocks de Persistence/Audit aceptan `Authorization` — **N/A**: los mocks Go de stress se
      eliminaron en la Fase 8; k6/vegeta pegan al endpoint de extracción del stack real
- [x] Documentado cómo correr el stress con tokens: `tests/stress/README.md` (tokens obligatorios
      para arrancar) y el hint de `scripts/vegeta/attack.sh` corregido (antes omitía los tokens y
      el comando no arrancaba)

**Dependencias:** Tasks 28-34.

**Tamaño:** S

### Checkpoint: Integración Persistence
- [x] `PERSISTENCE_API_TOKEN` sin default
- [x] `Authorization: Bearer` en todas las requests de `persistence.Client`
- [x] Rutas `?checksum=` con `url.QueryEscape`
- [x] Un `401` de Persistence no se reenvía como `401`
- [x] Defaults/puertos `:8000` (Persistence) y `:8083` (AUDA)
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] E2E Extract + Persistence reales documentado (Task 34)
- [x] Revisión con el humano

---

## Notas
- Comandos de verificación del repo: `go build ./...`, `go vet ./...`, `go test ./...` (aún no configurado lint; se puede añadir golangci-lint en Task 9 si se aprueba).
- Antes de cada checkpoint, resolver las Open Questions de `tasks/plan.md`.
- La Fase 4 (Tasks 11-17, issues #16-#23) **ya se puede implementar y verificar en tests**: AUDA
  expone el auth (`SERVICE_API_TOKEN`). Lo que sigue bloqueado es el **E2E de auditoría**, porque
  AUDA todavía no cablea sus endpoints `/audit/logs` (ver §6 de `tasks/plan.md`).
- La Fase 7 (Tasks 28-36, issue #24) integra el **contrato real de Persistence** (rutas
  `?checksum=`, auth Bearer, puerto `:8000`) y corrige el puerto de AUDA (`:8083`). Es bloqueante
  del CRUD contra el servicio real.
- La Fase 5 es la única que arregla un **fallo silencioso con datos corruptos**: hoy, con el
  Extract nuevo desplegado, el orquestador devolvería `200` con `text: ""` y el mismo checksum
  para todos los PDFs, sin ningún error en los logs.

---

## Fase 8: Validaciones centralizadas del PDF + dedup + baja del markdown — PLANIFICADA (08-10-2026, issues #34-#41)

> **Agregada al plan existente sin tocar las tareas sin cerrar de las Fases 4-7.** Diseño completo
> en `tasks/plan.md` §9. **Malware fuera de alcance.**
>
> **Rediseño:** validaciones **centralizadas** en `internal/validation`; **checksum = SHA-256(bytes
> del PDF)**; **dedup contra Persistence** antes de extraer; `page_count` desde `pdfcpu` (Opción 1,
> sin cambiar el contrato de Persistence).
>
> **Orden:** `{37, 38}` (paralelas) → `39` → **Checkpoint A** → `40` → `41` → `42` →
> **Checkpoint B** → `43`. Las Tasks 37, 41 y 42 tocan `pdf_service.go`, así que no se paralelizan
> entre sí.
>
> **Decisión (humano, 09-10-2026): se elimina el stress Go hecho a mano.** Los stress tests Go
> (`internal/server/stress_test.go`, `stress_limits_test.go`) y el generador
> `internal/testpdf/` se borraron: el stress ya lo cubren `scripts/k6/spike.js` y
> `scripts/vegeta/attack.sh` contra el stack real, y los tests unitarios/de integración usan los
> PDFs reales de `tests/stress/pdfs/` (Scrum Guide 16 págs como fixture principal).
>
> **DRY de fixtures:** el cifrado vive en `internal/pdfencrypt` (lo usan tests y el CLI
> `scripts/encryptpdf`) y el acceso al corpus en `internal/testsupport`, en vez de duplicar
> helpers por paquete. El caso cifrado → `422` se cubre E2E
> (`TestIntegrationEncryptedPDFReturns422AndWritesNoAudit`) y en `make smoke`; la validación
> solo clasifica (encrypted ⇒ error), nunca descifra.

### Task 37: Eliminar la conversión a markdown (Extract ya devuelve texto formateado) — #35 / ORC-10.1

**Descripción:** el MS Extract ahora devuelve el texto ya formateado en `content`. El
orquestador deja de convertir: `text = document.Content` y
`checksum = SHA-256(document.Content)`. Se borra el paquete `internal/markdown` completo
(código, tests y `testdata/` con los goldens).

**Criterios de aceptación:**
- [ ] `pdf_service.go` ya no importa `internal/markdown`; `Text` y `Checksum` salen de
      `document.Content` crudo
- [ ] El invariante `checksum == SHA-256(response.Text)` sigue asertado por test
      **temporalmente**: la Task 42 lo cambia a `SHA-256(bytes del PDF)`
- [ ] `internal/markdown/` eliminado por completo (verificar con
      `grep -rn 'markdown' internal/ --include='*.go'` sin resultados fuera de docs)
- [ ] Si el Extract además renombró campos de la respuesta, el contract test de
      `clients/extract` falla y se alinea el DTO (suposición §9.4.6)
- [ ] `page_count`, la auditoría y el `422` por texto vacío **no cambian**

**Verificación:**
- [ ] `go build ./... && go vet ./... && go test -race -count=1 ./...` en verde
- [ ] Tests de service/integración/stress actualizados: las aserciones sobre markdown
      esperan ahora el `content` crudo del mock
- [ ] Nota en `tasks/plan.md` §9 (ya escrita) deja §7/§11 como histórico

**Dependencias:** ninguna (independiente; puede correr en paralelo con la 38)

**Archivos:** `internal/services/pdf_service.go`, `internal/services/pdf_service_test.go`,
`internal/server/integration_test.go` (+ tests stress que aserten markdown), `README.md`,
borrado de `internal/markdown/` (18 archivos)

**Tamaño:** M (mayoría borrados)

**Riesgo a comunicar al humano antes de desplegar:** los checksums de documentos ya
persistidos cambian de valor (plan §9.5).

### Task 38: `internal/validation` — núcleo (errores, extensión, firma, capa de texto) — #36 / ORC-10.2

**Descripción:** paquete puro nuevo `internal/validation` que centraliza **todas** las
validaciones (decisión §9.4.1, se descarta `internal/pdfvalidate`). Esta task arma el
esqueleto y los módulos que no dependen de pdfcpu:
- `errors.go`: sentinelas `ErrUnsupportedExtension`, `ErrInvalidSignature`,
  `ErrMalformedPDF`, `ErrEncryptedPDF`, `ErrTooManyPages`, `ErrNoExtractableText` +
  `StatusOf(err) (status int, title string, ok bool)` / `TitleOf(err) string`.
- `validation.go`: `Input{PDF []byte; Filename string; MaxPages int}`,
  `Result{PageCount int}`, `PreExtract(Input) (Result, error)` (cadena fail-fast:
  extensión → firma; estructura/páginas las agregan 39/40) y `ValidateExtracted(content string) error`.
- `extension.go`: si `Filename` no está vacío, normalizar `/` y `\` y exigir que el último
  segmento termine en `.pdf` (case-insensitive).
- `signature.go`: los bytes deben empezar con `%PDF-`.
- `textlayer.go`: contenido sin texto extraíble ⇒ `ErrNoExtractableText`.

Esta task **no** cablea nada al service/handler (eso es la Task 41) ni agrega pdfcpu
(Task 39).

**Criterios de aceptación:**
- [ ] Sentinela → status según §9.2: extensión `415`, firma `400`, malformed `400`,
      encrypted `422`, too-many-pages `422`, no-text `422`; `StatusOf`/`TitleOf` lo exponen
- [ ] `PreExtract` fail-fast (extensión → firma); por ahora devuelve `Result{PageCount: 0}`
- [ ] Extensión: `informe.pdf`, `informe.PDF`, `docs/informe.pdf`, `C:\fakepath\a.pdf` ⇒
      pasan; `informe.exe`, `informe`, `a.pdf.exe`, `a.pdf.txt` ⇒ `ErrUnsupportedExtension`;
      header ausente/vacío ⇒ validación omitida (retrocompatible con `curl -T`)
- [ ] Firma: bytes con `%PDF-` inicial ⇒ pasa; cualquier otra cosa (incluso `""`) ⇒
      `ErrInvalidSignature`
- [ ] `ValidateExtracted`: vacío o sólo whitespace ⇒ `ErrNoExtractableText`; con texto ⇒ `nil`
- [ ] Paquete **puro**: sin importar pdfcpu todavía ni capas superiores (handler/service)
- [ ] Tests table-driven de los tres módulos

**Verificación:** `go test ./internal/validation/...`; `go vet ./...`.

**Dependencias:** ninguna (paralela con la 37)

**Archivos:** `internal/validation/errors.go`, `validation.go`, `extension.go`,
`signature.go`, `textlayer.go` (+ `*_test.go`)

**Tamaño:** M

### Task 39: `internal/validation` — pdfcpu: estructura + encriptado + `PageCount` — #37 / ORC-10.3

**Descripción:** agregar `github.com/pdfcpu/pdfcpu` v0.16 (pure Go, §9.4.1) al paquete
`internal/validation` de la Task 38:
- `structure.go`: valida la estructura del PDF (xref/trailer/objetos) en modo **relaxed**
  (§9.4.2) y clasifica el error de pdfcpu en `ErrMalformedPDF` vs `ErrEncryptedPDF`
  (§9.4.4).
- `pages.go`: expone la cantidad de páginas; `PreExtract` completa la cadena
  (extensión → firma → estructura) y devuelve `Result{PageCount: N}`.

Obligatorio **stateless** (`api.DisableConfigDir()`, §9.4.3): `compose.yaml` corre
`read_only: true`. Esta task **no** integra el paquete al service/handler (Task 41) ni
activa el gate de `MAX_PDF_PAGES` (Task 40; `Input.MaxPages` ya existe desde la 38).

**Criterios de aceptación:**
- [x] `github.com/pdfcpu/pdfcpu` v0.16 en `go.mod`; sin cgo (`CGO_ENABLED=0` compila)
      → v0.16.1 (última estable)
- [x] **Stateless / sin escritura en disco**: config `model.NewStatelessConfiguration()`
      (equivale a `api.DisableConfigDir()` sin mutar estado global)
- [x] Validación **relaxed** (no strict): sin falsos positivos sobre el corpus
- [x] PDF válido ⇒ `nil`; PDF truncado/corrupto ⇒ `ErrMalformedPDF`
- [x] PDF con contraseña ⇒ `ErrEncryptedPDF` — **jamás** clasificado como malformed ni
      enviado a Extract (test explícito de distinción)
- [x] `Result.PageCount` queda poblado con la cantidad real de páginas
- [x] Fixture encriptado **generado en el test** con `api.Encrypt` (no commitear binarios)
- [x] `go mod tidy` limpio

**Verificación:**
- [x] `go test ./internal/validation/...` (100% cobertura; incluye corpus real)
- [x] `go build ./... && go vet ./...`

**Desvíos vs. plan (revisar):**
- No se creó `pages.go`: un único parseo de pdfcpu (`api.PageCount` = Read+Validate)
  devuelve veredicto **y** cantidad de páginas, así que `structure.go` concentra la
  integración con pdfcpu. Evita parsear el PDF dos veces y hace imposible una
  discrepancia entre "válido" y "N páginas". El gate `MAX_PDF_PAGES` (Task 40) irá a
  `pages.go`.
- Stateless vía `model.NewStatelessConfiguration()` en vez de `api.DisableConfigDir()`:
  mismo efecto (no toca el FS) sin mutar una variable global de pdfcpu.


**Dependencias:** Task 38 (extiende el paquete)

**Archivos:** `internal/validation/structure.go`, `pages.go` (+ `*_test.go`), `go.mod`,
`go.sum`

**Tamaño:** M

**Nota de diseño:** ver §9.4.1-5. Si pdfcpu expone una API tipada para detectar encriptado,
preferirla sobre el match de mensajes de error; usar un match amplio como respaldo
(verificar con `source-driven-development`).

### Checkpoint A: Tras Tasks 37-39
- [ ] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [ ] `internal/validation` cubre extensión, firma, estructura, encriptado, `PageCount` y
      capa de texto, todo con tests unitarios
- [ ] PDF corrupto ⇒ `ErrMalformedPDF`; encriptado ⇒ `ErrEncryptedPDF` (fixture `api.Encrypt`
      generado en test)
- [ ] `internal/markdown` eliminado; `checksum = SHA-256(content)` **temporal**
- [ ] El corpus real de `tests/stress/pdfs/` (4 PDFs) no produce falsos positivos en relaxed
- [ ] Nada cableado aún al service/handler (la Task 41 lo integra)
- [x] Revisión con el humano antes de seguir (Decisiones §9 confirmadas en vivo)

### Task 40: Límite de páginas `MAX_PDF_PAGES` (default 1000) — #38 / ORC-10.4

**Descripción:** agregar la configuración y activar el gate de páginas en `internal/validation`
(§9.4.9) usando el `PageCount` que pdfcpu ya devuelve del parseo de la Task 39.

**Criterios de aceptación:**
- [x] `Config.MaxPDFPages int` leída de `MAX_PDF_PAGES`; default **1000**; `0` = sin límite
- [x] `PreExtract` rechaza `PageCount > Input.MaxPages` con `ErrTooManyPages` cuando
      `MaxPages > 0`; `0` ⇒ sin gate
- [x] Borde exacto: `== límite` pasa; `límite+1` ⇒ `ErrTooManyPages`
- [x] Número no numérico o negativo ⇒ `Load()` falla nombrando `MAX_PDF_PAGES` (patrón de `config.go`)
- [~] `config.MaxPDFPages` se cablea al service en el composition root **en la Task 41**
      (donde el service realmente lo consume). No se agrega todavía un parámetro a
      `NewPDFService` para no dejar un campo muerto (YAGNI) ni tocar 15 call sites de tests sin
      comportamiento que lo respalde.

**Verificación:** `go test ./internal/config/... ./internal/validation/...` (verde;
`validation` 100%, `config` 95.5%); test con PDF de N páginas generado (borde exacto incluido).

**Desvíos vs. plan (revisar):**
- El wiring `main.go` se difiere a la Task 41 (ver criterio `[~]`). `config.MaxPDFPages` ya se
  lee y se testea; el service lo consumirá al integrarse `PreExtract` en la Task 41.
- No se tocó `cmd/orchestrator/main.go` en esta task.


**Dependencias:** Task 39

**Archivos:** `internal/config/config.go` (+ `config_test.go`), `internal/validation/pages.go`
(+ test), `cmd/orchestrator/main.go`

**Tamaño:** S

### Task 41: Integrar `internal/validation` en service y handler — #39 / ORC-10.5

**Descripción:** cablear el paquete `internal/validation` en el flujo real (§9.3 pasos 3-4 y 8):
- El handler lee el header opcional `X-Filename` y lo pasa al service; la firma del service
  pasa a `IngestAndExtract(ctx context.Context, pdf []byte, filename string)` (§9.4.7).
- El service llama `validation.PreExtract` **antes** de Extract y
  `validation.ValidateExtracted` **después**.
- El handler reemplaza su `switch` de errores por `validation.StatusOf`/`TitleOf`.
- CORS suma `X-Filename` a los allowed headers.

`Content-Type` y el límite de 15 MB siguen en la capa HTTP (§9.4.6); el CRUD de `/texts` queda
fuera de alcance.

**Criterios de aceptación:**
- [x] `PDFService.IngestAndExtract(ctx, pdf, filename)` (interfaz + impl + tests actualizados)
- [x] Handler lee `X-Filename` (opcional) y lo pasa; `Content-Type`/tamaño intactos en HTTP
- [x] `PreExtract` antes de Extract ⇒ `415/400/422` sin llamar al Extract; `ValidateExtracted`
      después ⇒ `422`
- [x] El handler usa `validation.StatusOf`; sin `switch` de errores ad-hoc
- [x] CORS allowed headers incluye `X-Filename`
- [x] Los tests previos que asumían que `%PDF-` malformado llegaba al Extract
      se actualizan al nuevo `400` (los `stress_limits_test.go` que cubrían ese borde se
      eliminaron junto con el stress Go; ver la decisión de la Fase 8)
- [x] Un test con mock verifica que, ante error de validación, el Extract recibe **0** llamadas

**Verificación:** `go test ./internal/services/... ./internal/handlers/... ./internal/server/...`;
`go build ./... && go vet ./...`.

**Notas / desvíos:**
- Los tests usan PDFs **reales** del corpus (`tests/stress/pdfs/`); se descartó el generador
  `internal/testpdf` que se había agregado en una primera pasada y luego se eliminó por la
  decisión de la Fase 8. Los cuerpos `%PDF-1.7` sintéticos no parsean con pdfcpu, por eso los
  tests de estructura/páginas leen el Scrum Guide real.
- Se eliminaron de `services` los sentinelas `ErrInvalidPDF`/`ErrNoExtractableText` y el chequeo de
  firma ad-hoc; ahora todo el mapeo de errores vive en `validation`.
- **No** se implementó `TitleOf` como criterio (igual que Task 38): `StatusOf` devuelve `(status,
  title, ok)`, que cubre el handler.
- Se eliminaron los stress tests Go (`internal/server/stress_test.go`,
  `internal/server/stress_limits_test.go`), que dependían de `testpdf` y del invariante viejo; el
  stress queda cubierto por k6/vegeta (decisión de la Fase 8).

**Dependencias:** Tasks 37-40

**Archivos:** `internal/services/pdf_service.go` (+ test), `internal/services/services.go`,
`internal/handlers/pdf_handler.go` (+ test), `internal/server/middleware.go`,
`cmd/orchestrator/main.go`, `internal/server/integration_test.go`, `internal/server/routes_test.go`

**Tamaño:** M

### Task 42: Dedup contra Persistence + checksum sobre los bytes del PDF — #40 / ORC-10.6

**Descripción:** cambiar el checksum de salida a `SHA-256(bytes del PDF)` y usarlo para el
dedup (§9.3 pasos 5-6, §9.4.5): el service calcula `pdfSum`, busca en Persistence con
`FindByChecksum`; en **HIT** responde `200 {checksum: pdfSum, page_count: PageCount, text: registro.text}`
sin llamar al Extract ni auditar; en **miss** sigue el flujo normal; si el lookup **falla**,
fail-open (log warning y se extrae igual). Agregar `checksum.OfBytes` y
`httpclient.IsNotFound`.

**Criterios de aceptación:**
- [x] `checksum.OfBytes([]byte) string` (SHA-256 de los bytes) y `checksum` de la respuesta =
      `pdfSum`
- [x] `httpclient.IsNotFound(err) bool` distingue el `404` del lookup (de otros errores)
- [x] **HIT**: `200` con `page_count` de pdfcpu y `text` del registro; **sin** Extract ni auditoría
- [x] **miss**: flujo normal (Extract + auditoría)
- [x] **fail-open**: Persistence caído/`5xx`/timeout ⇒ log warning y se extrae igual ⇒ `200`
- [x] Invariante `checksum == SHA-256(bytes del PDF)` asertado; tests de hit/miss/fail-open con mock
- [x] Comentario de `checksum` en `internal/dto/pdf.go` y `internal/models/checksum.go` actualizado

**Verificación:** `go test ./internal/checksum/... ./internal/httpclient/... ./internal/services/... ./internal/server/...`.

**Notas / desvíos:**
- `NewPDFService` ahora recibe `(extract, persistence, audit, logger, maxPages)`; `main.go` construye
  un único `persistenceClient` y lo comparte con `NewTextService`.
- La firma del service/caller no cambió. El `logger` se agrega para el warning de fail-open (mismo
  patrón que `NewAuditService`).
- Se actualizaron los tests de `internal/server` que asertaban el invariante viejo
  `checksum == SHA-256(text)` a `SHA-256(pdf bytes)` (`integration_test.go`).
- `scripts/k6/spike.js` y `scripts/vegeta/attack.sh` (re-hash de bytes) quedan para la Task 43,
  que es donde el plan los lista.

**Dependencias:** Task 41

**Archivos:** `internal/checksum/checksum.go` (+ test), `internal/httpclient/problem.go` (+ test),
`internal/services/pdf_service.go`, `internal/services/services.go` (+ test), `internal/dto/pdf.go`,
`internal/models/checksum.go`, `internal/server/integration_test.go`

**Tamaño:** M

**Riesgo a comunicar antes de desplegar:** rompe el invariante histórico
`checksum == SHA-256(text)`; los registros viejos nunca matchean el lookup (plan §9.5).

### Checkpoint B: Tras Task 42
- [x] `PreExtract` + dedup integrados end-to-end en el flujo del service
- [x] HIT no llama al Extract ni audita (mock lo verifica); fail-open verificado
- [x] `checksum = SHA-256(bytes del PDF)` y `httpclient.IsNotFound` en uso
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] Revisión con el humano antes de la regresión final

### Task 43: Regresión integral — matrices de tests, stress, k6 y docs — #41 / ORC-10.7

**Descripción:** llevar toda la superficie de prueba y documentación al contrato nuevo de §9.2,
y verificar que las validaciones no rechazan PDFs reales.

**Criterios de aceptación:**
- [x] Matriz de validación (en `internal/validation` e integration test) cubierta: sin firma →
      `400`, corrupto → `400`, encriptado → `422`, páginas > límite → `422`, `X-Filename`
      inválido → `415`; los ≥400 siguen siendo `application/problem+json` RFC 9457
- [x] Integration test: flujo completo con las validaciones nuevas activas (miss → Extract →
      create; segundo request idéntico → HIT)
- [x] **Corpus real:** los tests unitarios/de integración usan los 4 PDFs de
      `tests/stress/pdfs/` y `internal/validation` no los rechaza en relaxed
      (falsos positivos ⇒ la Fase 8 NO se da por buena)
- [x] `scripts/k6/spike.js` y `scripts/vegeta/attack.sh` recalculan el checksum sobre los bytes
      del PDF (cambio de §9.5)
- [x] `README.md`: variable `MAX_PDF_PAGES`, header `X-Filename`, tabla de errores actualizada
- [x] `tasks/plan.md` §1.1 refleja el contrato final de errores (§9.2); `tests/stress/README.md`
      actualizado (sin la capa de stress Go)

**Verificación:**
- [x] `go build ./... && go vet ./... && go test -race -count=1 ./...` en verde
- [x] Smoke manual: `curl -T corrupto.pdf` → `400`; encriptado → `422`; `.exe` con
      `X-Filename` → `415`; PDF real → `200` con `checksum == SHA-256(bytes)`
      (automatizado en `make smoke`)

**Dependencias:** Tasks 37-42

**Archivos:** `internal/validation/*_test.go`, `internal/server/integration_test.go`,
`tests/stress/README.md`, `README.md`, `tasks/plan.md` (§1.1), `scripts/k6/spike.js`,
`scripts/vegeta/attack.sh`

**Tamaño:** M

### Checkpoint Final (Fin de Fase 8)
- [x] Todas las validaciones de §9.1 (salvo malware, fuera de alcance) implementadas y con tests
- [x] Paquete `internal/markdown` eliminado; ningún código llama `markdown.Convert`
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] Corpus real pasa sin falsos positivos
- [x] Checklist de verificación de la skill cumplido (criterios, verificación, dependencias, tareas en `tasks/todo.md`)
- [x] Revisión con el humano — incluida la decisión sobre el cambio de checksums históricos

## Fase 9: Apagado seguro (graceful shutdown) del orquestador — PLANIFICADA (09-10-2026, issues #42-#46)

> **Agregada al plan existente sin tocar las tareas sin cerrar de las Fases 4-8.** Diseño completo
> en `tasks/plan.md` §10. El gap: `auditService.Close(ctx)` existe pero no se llama en `run()`, y
> el timeout del shutdown está hardcodeado en 10s.
>
> **Orden:** `{44, 45}` (paralelas) → `46` → **Checkpoint C** → `47`.
>
> **Trade-off (decisión explícita):** el mismo deadline `SHUTDOWN_TIMEOUT` rige
> `srv.Shutdown` y `auditService.Close`. Si se agota, el proceso registra WARN
> y retorna un error (salida 1), sin colgarse más allá del deadline.

### Task 44: Drenar la auditoría asíncrona en el apagado — #43 / ORC-11.1

**Descripción:** llamar `auditService.Close(ctx)` en `run()`/`runWithSignals()` después de
`srv.Shutdown(ctx)` para que las emisiones async de `LogAsync` (que corren en goroutines con
`sync.WaitGroup`) terminen antes de que el proceso salga. El mismo `SHUTDOWN_TIMEOUT`
rige ambos pasos. (1) `srv.Shutdown` deja de aceptar conexiones y drena requests en vuelo;
(2) `auditService.Close` espera la `WaitGroup`; (3) si el deadline se agota, ambos pasos
pueden registrar WARN y el proceso retorna error (salida 1) — nunca se cuelga más allá
del deadline (ver §10).

**Criterios de aceptación:**
- [x] `auditService.Close(ctx)` se invoca en todo shutdown limpio (SIGINT/SIGTERM)
- [x] Un test demuestra que una emisión async iniciada antes de la señal termina antes de que
      `runWithSignals()` retorne
- [x] Los logs indican inicio y fin del drenado (`INFO "draining audit emissions"`,
      `INFO "audit emissions drained"`)

**Verificación:** `go build ./... && go vet ./... && go test -race -count=1 ./...`.

**Dependencias:** ninguna (el `Close` ya existe en `internal/services/audit_service.go`).

**Archivos:** `cmd/orchestrator/main.go`, `internal/server/integration_test.go` (o helper de
shutdown), `internal/services/audit_service_test.go`

**Tamaño:** S

### Task 45: Timeout de apagado configurable `SHUTDOWN_TIMEOUT` — #44 / ORC-11.2

**Descripción:** reemplazar el `10*time.Second` hardcodeado de `main.go` por `cfg.ShutdownTimeout`,
leído de `SHUTDOWN_TIMEOUT` (default `35s`, §10.3).

**Criterios de aceptación:**
- [x] `Config.ShutdownTimeout time.Duration` leída de `SHUTDOWN_TIMEOUT`; default `35s`
- [x] `main.go` usa `cfg.ShutdownTimeout` en el contexto del shutdown
- [x] Value inválido ⇒ `Load()` falla nombrando `SHUTDOWN_TIMEOUT` (patrón de `HTTP_TIMEOUT`)
- [x] Test de config (default + parse + inválido)

**Verificación:** `go test ./internal/config/... ./internal/server/...`.

**Dependencias:** Task 44 (mismo bloque de shutdown).

**Archivos:** `internal/config/config.go`, `internal/config/config_test.go`,
`cmd/orchestrator/main.go`, `README.md`

**Tamaño:** S

### Task 46: Tests de integración del apagado seguro — #45 / ORC-11.3

**Descripción:** cubrir el path de shutdown sin depender de señales del SO: refactorizar `run()`
para inyectar el canal de señales (ej. `runWithSignals(ctx, logger, cfg, stopCh)`) y verificar el
orden y el deadline.

**Criterios de aceptación:**
- [x] Test: auditoría async en vuelo al momento del shutdown se drena antes de retornar
- [x] Test: request en curso termina antes de que el server cierre (`srv.Shutdown` con handler lento)
- [x] Test: timeout agotado ⇒ `runWithSignals()` retorna error y **no cuelga** (deadline corto, ej. 50ms)
- [x] Correr con `-race`

**Verificación:** `go test -race ./cmd/orchestrator/... ./internal/server/...`.

**Dependencias:** Tasks 44 y 45.

**Archivos:** `cmd/orchestrator/main.go` (refactor `runWithSignals`), `cmd/orchestrator/main_test.go`
(nuevo) o `internal/server/integration_test.go`

**Tamaño:** M

### Checkpoint C: Tras Task 46
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] SIGINT/SIGTERM ⇒ proceso deja de aceptar conexiones, drena requests y auditoría, sale con 0
- [x] `SHUTDOWN_TIMEOUT` con valor chico ⇒ el proceso no se cuelga y loguea el error de timeout
- [x] Smoke manual: `kill -TERM <pid>` con un request lento en curso

### Task 47: Documentación del apagado seguro — #46 / ORC-11.4

**Descripción:** documentar `SHUTDOWN_TIMEOUT` y el comportamiento ante SIGTERM en `README.md`, y
reflejar la Fase 9 en `tasks/plan.md` §10 y en esta sección de `tasks/todo.md`.

**Criterios de aceptación:**
- [x] `README.md`: variable `SHUTDOWN_TIMEOUT` y párrafo de apagado seguro (drena requests y auditoría)
- [x] `tasks/plan.md` §10 documenta la secuencia y el trade-off del deadline
- [x] `tasks/todo.md` Fase 9 con tareas, criterios y checkpoints

**Verificación:** revisión manual de consistencia con el código.

**Dependencias:** Tasks 44-46.

**Archivos:** `README.md`, `tasks/plan.md` (§10), `tasks/todo.md` (esta sección)

**Tamaño:** S

### Checkpoint Final (Fin de Fase 9)
- [ ] `run()` drena `auditService` en todo shutdown limpio (Task 44)
- [ ] `SHUTDOWN_TIMEOUT` configurable con default 35s y tests de config (Task 45)
- [ ] Tests de integración del shutdown (drenado, request en vuelo, timeout) en verde con `-race` (Task 46)
- [ ] Docs actualizados: `README.md`, `tasks/plan.md` §10, `tasks/todo.md` (Task 47)
- [ ] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [ ] Checklist de verificación de la skill cumplido
- [ ] Revisión con el humano

## Fase 10: Observabilidad — calidad y cobertura de logs — COMPLETADA (2026-10-10, issues #47-#52)

> **Agregada al plan existente sin tocar las fases previas.** Diseño completo en
> `tasks/plan.md` §11. Diagnóstico: 6 gaps (logger hardcodeado, access log sin status, falta de
> `request_id` en logs de negocio, capa de red muda, handlers mudos, sin log de arranque).
>
> **Orden:** `48` → `49` → `50` → **Checkpoint D** → `51` → `52` → **Checkpoint Final**.
>
> **Regla dura:** nunca loguear tokens ni bodies — se verifica con tests.

### Task 48: Paquete `internal/logging` + wiring + log de arranque — #49 / ORC-12.1

**Descripción:** crear `internal/logging` (separado de `internal/config`), con `New()` que lee
`LOG_LEVEL` (`debug|info|warn|error`, default `info`) y `LOG_FORMAT` (`json|text`, default `json`),
`NewWithOptions(Options)` para tests/custom, `Discard()` para tests y `ParseLevel`. Reemplazar el
logger hardcodeado de `main.go` y agregar log de arranque con la config efectiva **sin secretos**.
Migrar los `discardLogger` duplicados de los tests a `logging.Discard()`.

**Criterios de aceptación:**
- [x] `internal/logging` exporta `New`, `NewWithOptions`, `Discard`, `ParseLevel` y `Options`
- [x] `LOG_LEVEL` inválido ⇒ `New` falla con error explícito; ausente ⇒ `info`
- [x] `LOG_FORMAT=text` produce salida Text; default JSON
- [x] `main.go` usa `logging.New()`; ningún `slog.New*Handler` hardcodeado en producción
- [x] Log de arranque con addr y config efectiva, sin tokens
- [x] Tests migrados a `logging.Discard()` (sin helpers duplicados)

**Verificación:** `go build ./... && go vet ./... && go test -race -count=1 ./...`.

**Dependencias:** ninguna.

**Archivos:** `internal/logging/logging.go` (+ `logging_test.go`), `cmd/orchestrator/main.go`,
tests con `discardLogger` (`internal/server/routes_test.go`, `internal/server/integration_test.go`,
`internal/services/audit_service_test.go`, etc.)

**Tamaño:** S

### Task 49: Access log con status, bytes y remote_addr — #48 / ORC-12.2

**Descripción:** envolver el `http.ResponseWriter` en `withRequestLog` con un `statusRecorder` que
captura `status` y `bytes`; loguear además `remote_addr`. Nivel según status: `5xx⇒Error`,
`4xx⇒Warn`, resto `Info`. Excluir `/healthz` y `/readyz`. El `statusRecorder` debe poder
implementar `http.Flusher` si el writer subyacente lo hace.

**Criterios de aceptación:**
- [x] Access log incluye `status`, `bytes`, `remote_addr` junto a method/path/duration/request_id
- [x] `5xx⇒Error`, `4xx⇒Warn`, resto `Info`
- [x] `/healthz` y `/readyz` no generan access log
- [x] Tests: 200⇒Info con status; 500⇒Error; `bytes` coincide; healthz no se loguea

**Verificación:** `go test -race ./internal/server/...`.

**Dependencias:** Task 48.

**Archivos:** `internal/server/routes.go` (+ `routes_test.go`), `internal/server/middleware.go` (si aplica)

**Tamaño:** S

### Task 50: `request_id` en logs de negocio — #50 / ORC-12.3

**Descripción:** crear `internal/reqctx` con la clave compartida de `request_id` y
`reqctx.Logger(ctx, logger)`. Migrar `middleware.go` a esa clave y usar `reqctx.Logger` en
`pdf_service`, `audit_service` y en el log de panic (para que el `request_id` llegue a las líneas
de negocio).

**Criterios de aceptación:**
- [x] Clave de `request_id` en un solo lugar, compartida por middleware y servicios
- [x] `reqctx.Logger(ctx, logger)` adjunta `request_id` cuando existe; no-op si no
- [x] Logs de `pdf_service` (dedup) y `audit_service` (emisión) incluyen `request_id`
- [x] El log de panic recovery incluye `request_id`
- [x] Tests: con y sin `request_id` en el ctx

**Verificación:** `go test -race ./internal/reqctx/... ./internal/services/... ./internal/server/...`.

**Dependencias:** Task 48.

**Archivos:** `internal/reqctx/reqctx.go` (+ `reqctx_test.go`), `internal/server/middleware.go`,
`internal/services/pdf_service.go`, `internal/services/audit_service.go` (+ tests)

**Tamaño:** S

### Checkpoint D: Tras Task 50
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] `LOG_LEVEL=debug` enciende el debug; `LOG_LEVEL=error` lo apaga
- [x] Access log con `status`; healthz/readyz silenciosos
- [x] Línea de dedup y de emisión de auditoría con `request_id`

### Task 51: Logs en la capa de red — #51 / ORC-12.4

**Descripción:** agregar logging opcional a `internal/httpclient.Client` (`NewWithLogger`/`WithLogger`,
la firma actual se mantiene). `Do` loguea: llamada saliente a `Debug` (`method`, `path`, `upstream`,
`duration_ms`) y fallos (timeout/red) o status no-2xx a `Warn`/`Error` con `status`. El `request_id`
del ctx llega vía `reqctx.Logger`. Los tres clientes (`extract`, `auditlog`, `persistence`) propagan
el logger desde el composition root.

**Criterios de aceptación:**
- [x] `httpclient.Client` acepta logger opcional sin romper la firma actual
- [x] Éxito ⇒ log `Debug` con method/path/upstream/duration
- [x] Fallo de transporte (timeout/conn rechazada) ⇒ `Warn`/`Error` con el error
- [x] Status no-2xx ⇒ log con `status`
- [x] `request_id` presente cuando el ctx lo trae
- [x] El token no aparece en ningún log (test)
- [x] Los 3 clientes reciben el logger en `main.go`

**Verificación:** `go test -race ./internal/httpclient/... ./internal/clients/...`.

**Dependencias:** Tasks 48 y 50.

**Archivos:** `internal/httpclient/client.go` (+ test), `internal/clients/{extract,auditlog,persistence}/client.go`,
`cmd/orchestrator/main.go`

**Tamaño:** M

### Task 52: Logs en los handlers — #52 / ORC-12.5

**Descripción:** los handlers loguean rechazos de validación (`415/400/422/413`) a `Info`/`Warn`
según status y fallos upstream (`502`; `401` de Persistence con el error real) a `Error`. Los
handlers reciben el logger por constructor o lo leen del ctx; elegir la opción con menos ruido en
los tests. `X-Filename` si viene, `X-Checksum` no.

**Criterios de aceptación:**
- [x] `pdf_handler` loguea 413/400/415/422/502 con status y `request_id`
- [x] `text_handler` loguea el 401 de Persistence con el error real y los 4xx/5xx
- [x] `audit_handler` loguea 400 y 502
- [x] Ningún log incluye body ni tokens
- [x] Tests por caso (fallo sí loguea; éxito no loguea ruido)

**Verificación:** `go test -race ./internal/handlers/...`.

**Dependencias:** Tasks 48 y 50.

**Archivos:** `internal/handlers/{pdf_handler,text_handler,audit_handler}.go` (+ tests),
`cmd/orchestrator/main.go`

**Tamaño:** M

### Checkpoint Final (Fin de Fase 10)
- [x] Access log con `status`/`bytes`/`remote_addr`; healthz/readyz excluidos (Task 49)
- [x] Líneas de negocio y de panic con `request_id` (Task 50)
- [x] Capa de red loguea fallos y salidas; los 3 clientes con logger (Task 51)
- [x] Handlers loguean rechazos y fallos upstream, sin bodies ni tokens (Task 52)
- [x] `LOG_LEVEL`/`LOG_FORMAT` funcionales; log de arranque sin secretos (Task 48)
- [x] Timeout de un upstream localizable en logs por `request_id`
- [x] `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` limpios
- [x] Checklist de verificación de la skill cumplido
- [x] Revisión con el humano

---

## Fase 11: Gateway Traefik (N-S + E-W) en `deploy/` — PLANIFICADA (10-10-2026, issues #53-#59)

> **Agregada al plan existente sin tocar las fases previas.** Diseño completo en `tasks/plan.md`
> §12. Traefik propio `v3.7.14` en una carpeta nueva `deploy/` (root del sistema de
> microservicios, hermana de `validator/`, `Conversor/`, `persistence/`, `audit_log/`): única puerta
> pública con TLS (mkcert), dos redes (`edge`/`backend internal`), un solo router público → el
> orquestador, y E-W interno (todo MS ↔ MS pasa por Traefik). Resiliencia (rate limit, CB, retry)
> movida al borde; los MS no publican puertos y Mongo queda orquestado. Certificaciones y
> decisiones cerradas: ver §12.1-§12.2 de `tasks/plan.md`.
>
> **Orden:** `53 → 54` → **Checkpoint E** → `55 → 56` → **Checkpoint F** → `57 → 58` →
> **Checkpoint G** → `59` → **Checkpoint Final**. Tareas secuenciales (todas tocan los mismos
> archivos de `deploy/`).
>
> **Desvíos vs. la primera propuesta (decisión D11.3):** **no** se cambian los defaults de base
> URLs en `internal/config/config.go` (siguen apuntando a `localhost` para dev); el compose setea
> `EXTRACT_BASE_URL`/`PERSISTENCE_BASE_URL`/`AUDIT_LOG_BASE_URL` y `SSL_CERT_FILE` por env.
>
> **Recordatorio de contexto (verificado contra el código):** Persistence (`:8000`, uvicorn),
> Audit (`:8000`, uvicorn) y Extract (`:8080`) **ya tienen Dockerfile**. Audit lo agregó el equipo
> de AUDA (escucha en `:8000`, no `:8083`) y quedó integrado al stack base. Extract con
> `deploy.replicas: 5`.
>
> **Estado final (2026-10-11):** Fase 11 implementada y verificada. README detallado en
> `deploy/README.md`. Desvíos/hallazgos documentados en `deploy/README.md` §10:
> - El criterio “`docker compose stop <ms>` ⇒ `503`” **no se cumple así**: con el provider Docker un
>   contenedor **removido** da `404` siempre (limitación del provider). El `503` confiable sale del
>   **circuit breaker** con el servidor presente pero fallando. Criterio corregido.
> - `allowEmptyServices: true` (documentado para dar `503` en vez de `404` con contenedores
>   unhealthy) devuelve **`500`** en v3.7.14 por el bug abierto de Traefik
>   [#13261](https://github.com/traefik/traefik/issues/13261). Se deja activo (no empeora lo visible).
> - Retries ejercitados de forma determinista con `deploy/scripts/verify-retries.sh` (6/6 ok).

### Task 53: Esqueleto `deploy/` — compose unificado + config estática de Traefik — #53 / ORC-13.1

**Descripción:** crear la estructura de `deploy/` en
`/home/francocp12/Desktop/3ro/Desarrollo de Software/Proyect/pdf-extractext_microservices/deploy`
(aún no existe): `compose.yaml` con redes `edge` + `backend` (`internal: true`) y servicios
`traefik` (v3.7.14, publica `80:80`/`443:443`, monta `docker.sock:ro`), `orchestrator`
(build `../validator/ValidationMicroservices-pdf-extractext`, `:8080`, en `edge`+`backend`, sin
`ports`, env `PORT`, tokens y `EXTRACT_BASE_URL`/`PERSISTENCE_BASE_URL`/`AUDIT_LOG_BASE_URL`),
`mongo` (`mongo:7`, sin publicar, volumen `mongo-data`), y los servicios-esqueleto `extract`,
`persistence` y `auditlog` por `build:` (comentados/declarados sin Dockerfile: Persistence y Audit
aún no lo tienen). `traefik/traefik.yml` (entrypoints `web`/`websecure`/`web-internal`, providers
docker `exposedByDefault: false` + file, `accessLog {}`), `traefik/dynamic.yml` mínimo y
`.env.example`. No cablear routers todavía (Tasks 55-56).

**Criterios de aceptación:**
- [x] `deploy/compose.yaml` valida con `docker compose config` (sintaxis, redes, build contexts existentes)
- [x] `traefik` publica `80` y `443`; **ningún** otro servicio tiene `ports`
- [x] Red `backend` con `internal: true`; `orchestrator` en `edge` y `backend`
- [x] `.env.example` documenta todos los envs de Traefik/orquestador/MS (tokens sin valores reales)
- [x] `traefik.yml` declara los 3 entrypoints (con redirect `web`→`websecure` 301) y ambos providers

**Verificación:**
- [x] `cd deploy && docker compose config` sale limpio
- [x] `docker compose up -d traefik mongo` → contenedores `healthy`
- [x] `curl -sS http://localhost/` redirige con 301 a `https://...`

**Dependencias:** ninguna (base de la fase).

**Archivos:** `deploy/compose.yaml`, `deploy/traefik/traefik.yml`, `deploy/traefik/dynamic.yml`,
`deploy/.env.example`

**Tamaño:** M

### Task 54: Certificados de dev (mkcert) + confianza del orquestador — #54 / ORC-13.2

**Descripción:** `deploy/certs/generate-certs.sh` usando **mkcert** (la máquina ya lo usa para
`*.universidad.localhost`): `mkcert -install`; generar un certificado para Traefik con SANs
`pdf-extractext.localhost`, `*.pdf-extractext.localhost`, `traefik`, `localhost`, `127.0.0.1`,
`::1` → `certs/traefik.pem` + `certs/traefik-key.pem`; copiar `$(mkcert -CAROOT)/rootCA.pem` →
`certs/rootCA.pem`; `certs/.gitignore` con `*.pem`/`*.key`. Montar `certs/` en Traefik (`:ro`)
y declarar el certificado en `dynamic.yml` (`tls.certificates`). El orquestador recibe
`SSL_CERT_FILE=/certs/rootCA.pem` (Go lo honra en Linux) para validar a Traefik en el tramo H2b.

**Criterios de aceptación:**
- [x] `./certs/generate-certs.sh` genera `traefik.pem`/`traefik-key.pem`/`rootCA.pem` sin errores
- [x] `traefik.pem` incluye los SANs de la lista (verificar con `openssl x509 -noout -text`)
- [x] `dynamic.yml` tiene `tls.certificates` con esos archivos (montados `:ro`)
- [x] `orchestrator` monta `certs:/certs:ro` y setea `SSL_CERT_FILE=/certs/rootCA.pem`
- [x] `.gitignore` (en `deploy/`) ignora `*.pem` y `*.key`

**Verificación:**
- [x] `curl --cacert certs/rootCA.pem -I https://pdf-extractext.localhost` valida la CA (sin error de
      certificado)
- [x] `openssl s_client -connect localhost:443 -servername pdf-extractext.localhost ... verify`
      → `Verify return code: 0 (ok)` con la CA de dev
- [x] `docker compose config` valida con los certs montados

**Dependencias:** Task 53.

**Archivos:** `deploy/certs/generate-certs.sh`, `deploy/certs/.gitignore`, `deploy/compose.yaml`,
`deploy/traefik/dynamic.yml`

**Tamaño:** S

### Checkpoint E: Tras Tasks 53-54
- [x] `docker compose config` válido; Traefik y Mongo `up`
- [x] TLS H1 con CA de dev: handshake verifica `0 (ok)` contra `pdf-extractext.localhost`
- [x] Redirect `http://` → `https://` funciona (301)
- [x] `rootCA.pem` montado y `SSL_CERT_FILE` seteado en el orquestador

### Task 55: Router público + borde N-S — #55 / ORC-13.3

**Descripción:** el único router que expone a la web: `orchestrator-public` en `websecure` con
`Host('pdf-extractext.localhost') && PathPrefix('/api')` → service `orchestrator@docker` (labels
`traefik.enable=true`, `traefik.docker.network=edge`, `server.port=8080`). Middlewares:
`security-headers` (SAMEORIGIN, nosniff, XSS filter, referrer/permissions policy) y `api-ratelimit`
(`rateLimit{average: 50, burst: 100}`) y `orchestrator-cb` (breaker default). `/healthz`/`/readyz`
**no** se rutean por web (sólo healthcheck local).

**Criterios de aceptación:**
- [x] `GET https://pdf-extractext.localhost/api/...` llega al orquestador (`200` en un endpoint real)
- [x] URL fuera de `/api` en el host → `404` de Traefik (sin pasar al orquestador)
- [x] Exceder `api-ratelimit` ⇒ `429 Too Many Requests` (prueba con burst)
- [x] Headers de seguridad presentes (`X-Frame-Options: SAMEORIGIN`, `nosniff`, ...)
- [x] `/healthz` y `/readyz` del orquestador **no** accesibles por `pdf-extractext.localhost`
- [x] Sin `Content-Type: application/pdf` en `/api/v1/pdfs/extract` ⇒ `415` (el router no lo cambia)

**Verificación:**
- [x] `curl --cacert certs/rootCA.pem -T .../2020-Scrum-...pdf --header 'Content-Type:
      application/pdf' https://pdf-extractext.localhost/api/v1/pdfs/extract` ⇒ `200`
- [x] Corrida corta de `scripts/vegeta/attack.sh` (o `curl` en loop) que supera el burst ⇒ `429`
- [x] `curl --cacert certs/rootCA.pem https://pdf-extractext.localhost/api/v1/texts` ⇒ rutea al
      orquestador (devuelve `405`/`400` según método: prueba el ruteo sin depender de upstream).
      *Nota:* con `GET` el orquestador responde `405 Method Not Allowed` (no `400`).

**Dependencias:** Tasks 53, 54.

**Archivos:** `deploy/traefik/dynamic.yml`, `deploy/compose.yaml`

**Tamaño:** M

### Task 56: E-W — todo el tráfico entre MS por Traefik — #56 / ORC-13.4

**Descripción:** los MS dejan de hablarse directo: el orquestador sale hacia
`https://traefik:8443` (entrypoint `web-internal`, TLS). Routers internos: `extract-ew`
(`PathPrefix('/extract')` → `extract@docker`), `texts-ew` (`PathPrefix('/texts')` →
`persistence@docker`), `audit-ew` (`PathPrefix('/audit')` → `auditlog@docker`). Labels de los MS
(`enable`, `network=backend`, `server.port` 8080/8000/8000). Completar los servicios `extract`
(replicas 5), `persistence`, `auditlog` en el compose con sus env (Mongo URI por servicio, tokens,
comandos uvicorn) y `mongo` con las dos bases. `.env.example` con los 3 base URLs =
`https://traefik:8443`. **Desvío D11.3:** los defaults de Go NO cambian; todo se setea por env.

**Criterios de aceptación:**
- [x] El flujo completo pasa por Traefik (verificable en el access log de Traefik: los paths
      `/extract`, `/texts`, `/audit` aparecen con `traefik` como destino)
- [x] `POST /api/v1/pdfs/extract` con PDF real ⇒ `200`, `checksum == SHA-256(bytes del PDF)`
- [x] `POST /api/v1/texts` ⇒ `201`; `GET /api/v1/audit/logs` ⇒ `200` con `{"logs":[...]}`
      (auditlog ya integrado, `:8000`)
- [ ] Sin token (o token distinto) en Persistence/Audit ⇒ el orquestador lo diagnostica (`502`,
      no `401`) y el E-W confirma que el `401` viene de los MS *(no ejecutado explícitamente)*
- [x] Mongo con las bases `pdf_extractext` y `pdf_extractext_audit` creadas por los MS

**Verificación:**
- [x] E2E completo (orquestador real + Traefik real): extract → create → find → audit
- [x] `docker compose ps` → sólo Traefik publica puertos en el host
- [x] Access log de Traefik muestra `host=pdf-extractext.localhost` (N-S) y los paths E-W

**Dependencias:** Tasks 53-55.

**Archivos:** `deploy/compose.yaml`, `deploy/traefik/dynamic.yml`, `deploy/.env.example`

**Tamaño:** M

### Checkpoint F: Tras Tasks 55-56
- [x] Flujo completo N-S + E-W vía Traefik (extract persistido, auditoría, CRUD)
- [x] `checksum == SHA-256(bytes)` en el E2E
- [x] MS no publican puertos; `remote_addr` del orquestador = IP de Traefik (documentado)
- [x] Sin error de certificado en el tramo orq→Traefik (H2b)

### Task 57: Resiliencia E-W — CBs + retries + el orquestador propaga el `503` — #57 / ORC-13.5

**Descripción:** agregar los circuit breakers por servicio (`orchestrator-cb`, `extract-cb`,
`persistence-cb`, `audit-cb`, expresión `NetworkErrorRatio() > 0.30 || ResponseCodeRatio(500, 600,
0, 600) > 0.30`), `extract-retry` (`attempts: 3`, `status: ["502-504"]`,
`retryNonIdempotentMethod: true`, `maxRequestBodyBytes: 16777216`) y `conservative-retry` para
`texts-ew` (`attempts: 2`, `status: ["502-504"]`, sin no-idempotentes). **Audit sin retry de
Traefik** (POST no-idempotente; ya hay retry in-app). `serversTransport default` con
`http2.enabled: false` (h2c rompería contra Go/uvicorn) y `responseHeaderTimeout: 35s`. **Cambio
Go (TDD RED→GREEN):** con un breaker abierto Traefik responde `503` con cuerpo no-RFC-9457; hoy el
orquestador lo traga como `502` (`internal/httpclient/client.go:68-71` pierde el status). Agregar
`StatusError{Code int, Path string}` + `StatusCode(err) (int, bool)` en `internal/httpclient/problem.go`,
devolverlo en `client.go` cuando `ParseProblem` falla, y que los handlers mapeen `503→503` (resto→`502`).

**Criterios de aceptación:**
- [x] Breaker de un MS (servidor presente fallando) → `503 Service Unavailable` al superar
      el ratio; se auto-recupera al volver el MS
- [x] El orquestador **propaga el `503`** del breaker (no lo convierte en `502`)
- [x] `extract-retry` reintenta `502-504` del Extract y no reenvía el body > 16 MB
- [x] `texts-ew` reintenta GET/PUT/DELETE pero **no** POST; audit nunca se reintenta desde Traefik
- [x] `serversTransport default` con HTTP/1.1 forzado (`disableHTTP2: true`; sin `505`/preface h2c)
- [x] Tests Go: `StatusCode` devuelve el status de un `StatusError` y de un `Problem`; handler
      convierte `503` en `503`; `ParsedProblem` sigue funcionando (RED antes del fix)

**Verificación:**
- [x] `go build ./... && go vet ./... && go test -race -count=1 ./...` en verde (cambio Go)
- [x] Un MS **presente pero fallando** (breaker abierto) → `POST /api/v1/pdfs/extract` ⇒ `503`
      (propagado por el orquestador); al recuperarse ⇒ `200`. *Criterio corregido:* con el provider
      Docker, `docker compose stop extract` (contenedor removido) da `404`, no `503` (ver
      `deploy/README.md` §10).
- [x] `get /api/v1/audit/logs` ⇒ `200` con auditlog integrado (Dockerfile de AUDA, `:8000`)
- [x] Retries verificados con `deploy/scripts/verify-retries.sh` (extract 3 intentos; GET
      reintentado; POST no) — 6/6 ok
- [x] Access log de Traefik muestra `circuitBreaker`/retry en los paths E-W

**Dependencias:** Tasks 53-56.

**Archivos:** `deploy/traefik/dynamic.yml`, `internal/httpclient/problem.go` (+ test),
`internal/httpclient/client.go`, `internal/handlers/{pdf_handler,text_handler,audit_handler}.go` (+ tests)

**Tamaño:** M

### Task 58: Hardening del despliegue — #58 / ORC-13.6

**Descripción:** endurecer el runtime: red `backend` `internal: true` (verificado, no sólo
declarado), **sólo** Traefik publica puertos, mounts de config/certs/`docker.sock` en `:ro`,
`security_opt: [no-new-privileges:true]`, `cap_drop: [ALL]` (con las capacidades mínimas que
requiera cada imagen), `read_only: true` donde la imagen lo permite (el orquestador y Traefik),
healthchecks de Docker para cada servicio (`/healthz`, `/readyz`, `traefik ping`, `mongodb
--ping`, y el equivalente de cada MS — documentar que Persistence/Audit exponen `/health`), y
`restart: unless-stopped`. Verificar que un contenedor comprometido en `backend` no alcanza la red
del host.

**Criterios de aceptación:**
- [x] Red `backend` con `internal: true` y **ningún** servicio con `ports` salvo Traefik
- [x] `docker.sock`, configs y certs montados `:ro` en Traefik
- [x] `no-new-privileges`, `cap_drop: ALL`, `read_only` donde aplique, healthchecks por servicio
- [x] `docker network inspect` de `backend` confirma `"internal": true`
- [x] Smoke: matar un MS no deja sockets al host; Traefik sigue sirviendo el resto

**Verificación:**
- [x] `docker compose config` valida con las opciones
- [x] `docker compose ps` → un solo puerto publicado al host (`80`/`443`)
- [x] `curl https://pdf-extractext.localhost/api/v1/pdfs/extract` sigue `200` tras el hardening

**Dependencias:** Tasks 53-57.

**Archivos:** `deploy/compose.yaml`, `deploy/traefik/traefik.yml`

**Tamaño:** S

### Checkpoint G: Tras Tasks 57-58
- [x] CBs por servicio → `503` (con servidor presente fallando) y auto-recuperación; el orquestador propaga el `503`
- [x] `disableHTTP2: true` activo (`serversTransport.default`); sin `505`/preface h2c en los MS
- [x] Sólo Traefik publica puertos; `backend` `internal: true` efectivo
- [x] `read_only`/`cap_drop`/healthchecks aplicados; smoke E2E sigue verde

### Task 59: Docs + validación final (429/503 con k6/vegeta) — #59 / ORC-13.7

**Descripción:** documentar `deploy/` (README del root del sistema o `deploy/README.md`),
actualizar `tasks/plan.md` §12 y esta sección de `tasks/todo.md`, y validar el comportamiento del
borde con carga: `api-ratelimit` ⇒ `429` y breaker ⇒ `503`, usando `scripts/k6/spike.js` y
`scripts/vegeta/attack.sh` **ajustados** para apuntar a `https://pdf-extractext.localhost` con el
CA de dev y el token si lo requiere el stack real.

**Criterios de aceptación:**
- [x] `deploy/README.md`: cómo generar certs, levantar el stack, variables, decisiones y
      `docker compose` (arquitectura, TLS por tramo, resiliencia, hardening, limitaciones)
- [x] `scripts/vegeta/attack.sh` ajustado (HEALTH_PATH + `-k`) para apuntar a `pdf-extractext.localhost`;
      `scripts/k6/spike.js` se deja para los compose locales (decisión del humano)
- [x] Con burst alto ⇒ `429` (api-ratelimit) medido (`2000` req → `599×200`, `1401×429`);
      breaker ⇒ `503` medido (`502:1  503:35`)
- [x] `tasks/plan.md` §12 y `tasks/todo.md` Fase 11 reflejan el estado final (checkboxes)
- [ ] Revisión con el humano

**Verificación:**
- [x] `go build ./... && go vet ./... && go test -race -count=1 ./...` verdes (incluye cambio de Task 57)
- [x] Corridas vegeta (`429`) y breaker (`503`) documentadas en `deploy/README.md` §8

**Dependencias:** tasks 53-58.

**Archivos:** `deploy/README.md`, `scripts/k6/spike.js`, `scripts/vegeta/attack.sh`,
`tasks/plan.md` (§12), `tasks/todo.md` (esta sección)

**Tamaño:** S

### Checkpoint Final (Fin de Fase 11)
- [x] `docker compose config` válido; `curl --cacert` con PDF real ⇒ `200` y `checksum == SHA-256(bytes)`
- [x] `429` al exceder el rate limit; `503` del breaker (servidor presente fallando) y auto-recuperación
- [x] Sólo Traefik publica puertos; `backend` `internal: true`; mounts `:ro` y `cap_drop`
- [x] Compliance Go del cambio Task 57 (RED→GREEN) con los comandos del repo en verde
- [x] Checklist de verificación de la skill cumplido
- [ ] Revisión con el humano