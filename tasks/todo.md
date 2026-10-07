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
- [ ] Revisión con el humano (queda pendiente: **no** la puedo marcar yo) (decisión sobre el límite de body tomada en 24-sep-2026)

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
- [ ] Revisión con el humano

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
- [x] Normaliza `\r\n` → `\n` antes de partir (PDFium usa `\r\n` *dentro* de una página)
- [x] Parte por `\n\n` (el Extract une páginas con `\n\n` ⇒ delimitador **no ambiguo**) y descarta segmentos vacíos
- [x] Des-hifeniza **sólo** si la línea termina en `-` y la siguiente empieza en minúscula
- [x] Unwrap: une las líneas de un párrafo con un espacio simple; un salto en blanco separa párrafos
- [x] `Convert("")`, `Convert("   \n\n  ")` y `Convert` de un solo segmento devuelven `""` (nunca panic)
- [x] `Convert` es **determinista**: dos llamadas con la misma entrada dan el mismo string

**Verificación:**
- [x] `go test ./internal/markdown/...` en verde, con tests table-driven y **goldens** (string entrada → string esperado exacto)
- [x] Golden real: un fragmento con guiones de corte de línea de PDFium, esperado written a mano
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
`Conversor/testdata/` (Scrum Guide 16 págs, Essential Kanban 90 págs y 8.5MB, Filosofía Lean
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
- [ ] Revisión con el humano (queda pendiente: **no** la puedo marcar yo)

---

## Fase 6: Stress y pruebas de carga — IMPLEMENTADA

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
- [ ] Los mocks de Persistence/Audit aceptan `Authorization`
- [ ] Documentado cómo correr el stress con tokens

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
- [ ] Revisión con el humano

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