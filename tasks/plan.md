# Implementation Plan: Orchestrator & Validator (SDD — Fase 2)

## Overview

Este documento es la **Fase 2 (Planning/Diseño y Estructura)** del microservicio **Orchestrator & Validator**, el único punto de entrada del sistema de extracción de PDFs. Orquesta secuencialmente vía REST a los otros 3 microservicios: **Extract** (extracción de texto), **Persistence** (CRUD de texto) y **Audit Log** (registro de auditoría).

**Stack:** Go, `github.com/go-chi/chi/v5`, arquitectura de 3 capas (Handlers → Services → Clients), `context.Context` en todas las firmas, inyección de dependencias por constructores, errores estandarizados bajo RFC 9457 (Problem Details).

**Alcance:** SOLO diseño. Se definen el contrato de API, la estructura de directorios, las entidades/DTOs, las interfaces y el flujo de datos. **No se incluye implementación de lógica de negocio** (Fase 3); esa queda descompuesta como backlog de tareas en `tasks/todo.md`.

**Estado de Fase 2: COMPLETADA** (revisada y aprobada por el humano antes de iniciar Fase 3).

---

## Contexto: Contractos reales de los MS hermanos

El diseño de la capa de *clients* se validó contra los repos existentes (no contra supuestos):

| MS | Repo | Contrato detectado |
|----|------|--------------------|
| **Extract** (Go, pdf_oxide) | `Conversor/extract-markdown-microservice-pdf-extractext` | `POST /extract`, body = PDF crudo (binario, sin base64) con `Content-Type: application/pdf` (también acepta `application/octet-stream` y `multipart/form-data` campo `file`). Límite 50MB. **Respuesta: `{ "content", "page_count" }`.** Errores RFC 9457 `application/problem+json`. Puerto 8080. Sin auth. **Sin `/health` ni `/readyz`.** Deadline interno 30s. Ver §1.6. |
| **Audit Log** (Python/FastAPI) | `AuditLog/AuditLogMicroservice-pdf-extractext` | Puerto **8083**. **Auth Bearer YA implementado** (§6): `SERVICE_API_TOKEN` sin default, middleware *outermost*, `401` con `WWW-Authenticate`, abiertas sólo `/health` y `/readyz`. Contrato previsto: `POST /audit/logs` → 201 con `{action, entity_type, checksum, details, performed_at, _id}`; `GET /audit/logs?skip&limit`; `GET /audit/logs/checksum/{checksum}`. **Los endpoints HTTP todavía NO están cableados** (`app/api/routers/audit_logs.py` no existe; `main.py` sólo monta `health`; T10/T13 sin cerrar), así que la auditoría E2E sigue bloqueada del lado de AUDA. |
| **Persistence** (Python/FastAPI) | `persistence/PersistenceMicroservices-pdf-extractext` | **YA implementado.** Ruta base **`/texts`** (sin `/api/v1`): `POST /texts` → `201 {message, checksum}` con `Location`; `GET|PUT|DELETE /texts?checksum=...` (**query param, no path**); `TextOut` = `{checksum, text, name, metadata, created_at, updated_at}` RFC 3339 (ms). Requiere `Authorization: Bearer <SERVICE_API_TOKEN>` en toda ruta salvo `/health` y `/readyz`. Puerto **8000**. Ver §8. |

> **Cambio de contrato del Extract (verificado contra el código, no contra docs).** El Extract
> fue **reescrito en Go** (antes Rust/axum) y su respuesta **cambió de forma incompatible**:
> `text` → `content`, y `pages[]` + `duration_ms` **se eliminaron**. Como `encoding/json` ignora
> campos desconocidos, un DTO con `json:"text"` seguiría compilando y devolvería `""` **sin
> error**: el checksum sería `SHA-256("")` para todos los PDFs. Es un fallo silencioso, y por eso
> la Task 18 (arreglo del DTO) va primera en la Fase 5.

---

## Architecture Decisions

1. **3 capas estrictas y unidireccionales.** `handlers` (transporte HTTP + validación de entrada) → `services` (reglas de negocio, orquestación, checksum, auditoría) → `clients` (REST a otros MS). Nunca se llaman capas horizontales; los handlers nunca hablan HTTP con otros MS.
2. **Checksum = SHA-256 (hex) sobre el markdown devuelto.** Es el ID único de todo el sistema. Se modela como tipo propio `models.Checksum` (no `string` a secas) para prevenir confusiones de tipos a nivel de compilación. Como `text` es exactamente el markdown que se persiste vía `POST /api/v1/texts`, el invariante `checksum == SHA-256(text persistido)` **se preserva** (ver §7.2).
3. **Auditoría fire-and-forget.** El envío al MS Audit Log se ejecuta en una goroutine separada con `context.WithTimeout` derivado; nunca bloquea la respuesta del endpoint ni propaga errores al cliente (best-effort: reintento simple + log local ante fallo).
4. **DI por constructores.** `NewService(clients..., cfg)` / `NewHandler(services..., logger)`. Las interfaces se declaran en el lado del consumidor (máxima testabilidad con mocks).
5. **`context.Context` primero en todas las firmas** (handlers, services, clients). Timeouts por destinatario vía configuración.
6. **Clientes "tontos".** Los clientes REST solo serializan/deserializan y traducen transporte HTTP → errores tipados de dominio. Nunca toman decisiones de negocio. En particular `clients/extract` es un **espejo fiel del contrato upstream**: devuelve `content` crudo, sin transformar.
7. **Errores RFC 9457 (`application/problem+json`).** El orquestador emite y consume Problem Details (consistente con el MS Extract).
8. **Inmutabilidad por contrato.** En `Update` el servicio valida y rechaza intentos de modificar `text`/`checksum` *antes* de delegar a Persistence (regla de negocio en la capa de servicio, no solo en el cliente).
9. **Convención de nombres Go:** cada contrato es la interfaz `Client` / `Service` dentro de su paquete (`extract.Client`, `persistence.Client`, `auditlog.Client`), consumida como `internal.Service`.
10. **módulo Go:** se propone `validationmicroservices-pdf-extractext` (los nombres de módulo no admiten mayúsculas; según el nombre de directorio actual).
11. **El markdown se arma en el orquestador, no en el Extract.** El Extract no cruza datos de layout por su contrato (sólo un `string` por documento), así que el orquestador no puede detectar títulos reales. La conversión es **cosmética y determinista** (ver §7); producir markdown estructural de verdad requiere que el Extract exponga los *text runs* con `Size`/`Weight`/`Box` de pdf_oxide, y eso es trabajo de su equipo, no de este repo.

---

## 1. Diseño de la API (API Contract)

Base path: `/api/v1`. Todas las respuestas de error son `application/problem+json` (RFC 9457).

### 1.1 Ingesta y Extracción de PDF

```
POST /api/v1/pdfs/extract
Content-Type: application/pdf
Body: binario crudo del PDF (máx. 15MB)

200 OK
{
  "checksum":    "d4a5...",          // SHA-256 hex del markdown devuelto (ID único del sistema)
  "page_count":  10,
  "text":        "# markdown del documento..."
}

400 Bad Request (Problem): PDF inválido / no es PDF / body vacío (falta la firma %PDF-)
413 Payload Too Large (Problem): supera el límite
422 Unprocessable Entity (Problem): el PDF es válido pero NO tiene texto extraíble
                                    (típicamente un PDF escaneado: sólo imágenes)
502 Bad Gateway (Problem): MS Extract no disponible o devolvió error
```

> El campo conserva el nombre `text` **a propósito**: `checksum == SHA-256(text)` es un
> invariante del sistema, y renombrarlo obligaría a revisar ese invariante en todos los
> clientes. Lo que contiene es markdown (ver §7).
>
> `422` se distingue de `400` a propósito: `400` significa "esto no es un PDF", `422` significa
> "esto es un PDF perfectamente válido cuyo contenido no se puede procesar". Un escaneado
> no es un request mal formado, así que `400` mentiría.

### 1.2 CRUD de Texto (delegado a Persistence)

| Operación | Método y ruta | Cuerpo (request) | Respuesta |
|-----------|---------------|------------------|-----------|
| **Create** | `POST /api/v1/texts` | `{ "text", "checksum", "name", "metadata" }` | `201` → `{ "message": "OK", "checksum" }` |
| **Update** | `PUT /api/v1/texts/{checksum}` | `{ "name", "metadata" }` (SÓLO estos campos) | `200` → registro completo modificado |
| **Delete** | `DELETE /api/v1/texts/{checksum}` | — | `200` → `{ "message": "OK", "checksum" }` |
| **Read** | `GET /api/v1/texts/{checksum}` | — | `200` → registro almacenado (Text) |

Regla de negocio: en **Update** únicamente se permiten `name` y `metadata`; `text` y `checksum` son inmutables (el servicio rechaza el request si intenta tocarlos).

Errores: `400` (payload inválido), `404` (checksum inexistente), `409` (checksum ya existe en Create), `502` (Persistence no disponible).

### 1.3 Auditoría (delegada a Audit Log)

> **AUTH IMPLEMENTADO; ENDPOINTS PENDIENTES.** AUDIT ya implementó el auth de la §6: *todas* sus
> llamadas exigen `Authorization: Bearer <SERVICE_API_TOKEN>` (salvo `/health` y `/readyz`) y la
> app no arranca sin la variable. **Falta cablear sus endpoints HTTP** `/audit/logs` (ver §6.0),
> así que el orquestador puede implementar su lado (Fase 4, issues #16-#23) pero la auditoría E2E
> no se puede verificar hasta que AUDA los exponga.

- **Escritura:** interna, asíncrona (fire-and-forget). Se emite para `PDF_EXTRACT`, `Create`, `Update`, `Delete`. **Excluida** del `GET /api/v1/texts/{checksum}` (find).
- **Lectura (proxy):**
```
GET /api/v1/audit/logs?checksum=<sha256-hex>&skip=0&limit=10
   · sin checksum → GET /audit/logs?skip&limit            (MS Audit Log)
   · con checksum → GET /audit/logs/checksum/{checksum}   (MS Audit Log)

200 OK
{ "logs": [ { "_id", "action", "entity_type", "checksum", "details", "performed_at" } ] }
```

### 1.4 Operacionales

```
GET /healthz  → 200 {"status":"ok"}
GET /readyz   → 200 {"status":"ok"}
```

> **Corrección:** ambas rutas comparten el mismo `statusEndpoint()` y **ninguna verifica
> conectividad a las dependencias**, pese a que la versión anterior de este documento lo
> afirmaba. El MS Extract además **no expone `/health`**, así que hoy no hay a qué preguntar.
> Dejarlo así es coherente: un readiness que consulta un Extract sin endpoint de salud sólo
> produciría falsos negativos. Si se quiere un readiness real, primero hay que agreedar con
> el equipo del Extract que agregue el endpoint (ver Pregunta Abierta 9).

### 1.5 Límites y timeouts frente al Extract

El Extract tiene un **deadline interno de 30s** (`WRITE_TIMEOUT`) que, al vencerse, devuelve
un `503 "context deadline exceeded"` bien tipado. Si nuestro `HTTP_TIMEOUT` fuera **menor**,
cortaría primero el cliente y devolveríamos un error opaco en vez del `503` real. Por eso
`HTTP_TIMEOUT` debe quedar por **encima** de 30s (Task 25).

| Límite | Extract | Orquestador |
|--------|---------|-------------|
| Tamaño máximo de PDF | 50MB | **15MB** (nuestro, más estricto: gana primero) |
| Deadline de la request | 30s | 35s (nuestro, más laxo a propósito) |
| Endpoint de salud | no existe | — |

---

## 2. Estructura de Directorios

```
ValidationMicroservices-pdf-extractext/
├── cmd/
│   └── orchestrator/
│       └── main.go                  # Bootstrap: config, DI (composición), servidor HTTP
├── internal/
│   ├── config/
│   │   └── config.go                # Carga de env (puertos, base URLs de MS, timeouts)
│   ├── handlers/                    # [CAPA 1] Transporte HTTP + validación de entrada
│   │   ├── pdf_handler.go           # POST /api/v1/pdfs/extract
│   │   ├── text_handler.go          # CRUD texto (create/update/delete/read)
│   │   ├── audit_handler.go         # GET /api/v1/audit/logs (proxy)
│   │   └── handlers.go              # structs con DI + helpers JSON/Problem Details
│   ├── services/                    # [CAPA 2] Lógica de negocio + orquestación
│   │   ├── pdf_service.go           # Ingesta: validar PDF → extraer → checksum → auditar
│   │   ├── text_service.go          # CRUD delegado a Persistence + regla de inmutabilidad
│   │   ├── audit_service.go         # Emisión async + lectura de logs (proxy)
│   │   └── services.go              # Interfaces de servicio (contratos de la capa)
│   ├── clients/                     # [CAPA 3] REST hacia otros MS
│   │   ├── extract/                 # → MS Extract (POST /extract, PDF binario)
│   │   │   └── client.go
│   │   ├── persistence/             # → MS Persistence (CRUD texto)
│   │   │   └── client.go
│   │   └── auditlog/                # → MS Audit Log (emit + fetch)
│   │       └── client.go
│   ├── models/                      # Entidades de dominio
│   │   ├── checksum.go              # type Checksum string
│   │   ├── text.go                  # Text
│   │   └── audit_log.go             # AuditEvent, AuditLog, OperationType
│   ├── dto/                         # Structs request/response entre capas y hacia el cliente
│   │   ├── pdf.go
│   │   ├── text.go
│   │   └── audit.go
│   ├── checksum/                    # Util: SHA-256 hex sobre []byte/string
│   │   └── checksum.go
│   ├── markdown/                    # Fase 5: texto plano del Extract → markdown (puro, sin deps)
│   │   ├── convert.go               #   pipeline: split páginas → headers/pies → títulos → unwrap → escape
│   │   └── escape.go                #   escapado mínimo de caracteres con sintaxis markdown
│   └── httpclient/                  # HTTP client compartido: contextos, timeouts,
│       └── problem.go               #   parsing de RFC 9457 (Problem Details)
├── go.mod
├── go.sum
├── Dockerfile                       # Multi-stage, non-root (Fase 4 de infraestructura)
├── compose.yaml                     # Servicio del orquestador en la red externa "mired"
└── .gitignore                       # Adaptado a Go (ver tarea 1)
```

Los directorios `handlers/`, `services/` y `clients/` son las 3 capas. `models/` y `dto/` contienen los contratos de datos; `httpclient/`, `checksum/` y `markdown/` son utilidades transversales. No hay `pkg/` porque el código es 100 % privado del microservicio (`internal/`).

---

## 3. Entidades y DTOs (Structs)

### 3.1 `internal/models/checksum.go`

```go
package models

// Checksum identifica de forma única el texto extraído en todo el sistema.
type Checksum string

func (c Checksum) String() string { return string(c) }
```

### 3.2 `internal/models/text.go`

```go
package models

import "time"

type Text struct {
    Checksum  Checksum
    Text      string
    Name      string
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### 3.3 `internal/models/audit_log.go`

```go
package models

import "time"

type OperationType string

const (
    OpPDFExtract OperationType = "pdf.extract"
    OpTextCreate OperationType = "text.create"
    OpTextUpdate OperationType = "text.update"
    OpTextDelete OperationType = "text.delete"
)

// AuditEvent es el payload que se envía al MS Audit Log (fire-and-forget).
type AuditEvent struct {
    Action      OperationType
    EntityType  string   // "document" | "text"
    Checksum    Checksum
    Details     any      // metadatos relevantes (ej: page_count)
    PerformedAt time.Time
}

// AuditLog refleja la forma que devuelve el MS Audit Log (FastAPI/Mongo).
type AuditLog struct {
    ID          string      `json:"_id"`
    Action      string      `json:"action"`
    EntityType  string      `json:"entity_type"`
    Checksum    Checksum    `json:"checksum"`
    Details     any         `json:"details"`
    PerformedAt time.Time   `json:"performed_at"`
}
```

### 3.4 `internal/dto/pdf.go`

```go
package dto

// ExtractedDocument: espejo FIEL del contrato del MS Extract (Fase 5).
// El Extract (Go/pdf_oxide) devuelve {content, page_count}. No devuelve páginas
// ni duración: NO inventar campos que upstream no manda.
type ExtractedDocument struct {
    PageCount int    `json:"page_count"`
    Content   string `json:"content"`
}

// ExtractPDFResponse: lo que devuelve el ORQUESTADOR a su cliente.
// `text` contiene markdown (ver §7), no texto plano.
type ExtractPDFResponse struct {
    Checksum  models.Checksum `json:"checksum"` // == SHA-256(Text)
    PageCount int             `json:"page_count"`
    Text      string          `json:"text"`
}
```

> Se conserva el nombre `Text` en ambos lados a propósito, aunque upstream diga `content`: los
> DTO de *entrada* desde un MS hermano deben calcar sus nombres de campo, y los DTO de *salida*
> hacia el cliente son nuestro contrato. El tag JSON es lo que conecta con el cable en cada
> caso.

### 3.5 `internal/dto/text.go`

```go
package dto

type CreateTextRequest struct {
    Text     string         `json:"text"`
    Checksum models.Checksum `json:"checksum"` // vital: ID único del sistema
    Name     string         `json:"name"`
    Metadata map[string]any `json:"metadata"`
}

type CreateTextResponse struct {
    Message  string         `json:"message"`
    Checksum models.Checksum `json:"checksum"`
}

// UpdateTextRequest: SOLO name/metadata (regla de negocio).
// Semántica de update parcial (Persistence trata `null` como campo ausente y
// `""`/`{}` como "limpiar"): los campos opcionales se modelan con punteros/mapas
// nil y sin `omitempty`, para no perder la diferencia entre "no enviar" y
// "enviar vacío". Decisión adoptada en la Fase 7 (Task 30, #29).
type UpdateTextRequest struct {
    Name     *string        `json:"name"`
    Metadata map[string]any `json:"metadata"`
}

type DeleteTextResponse struct {
    Message  string         `json:"message"`
    Checksum models.Checksum `json:"checksum"`
}

// CreateTextPayload / UpdateTextPayload: payloads hacia el MS Persistence.
type CreateTextPayload = CreateTextRequest
type UpdateTextPayload = UpdateTextRequest
```

### 3.6 `internal/dto/audit.go` + `httpclient/problem.go`

```go
package dto

type AuditQueryParams struct {
    Checksum models.Checksum
    Skip     int
    Limit    int
}

type AuditLogsResponse struct {
    Logs []models.AuditLog `json:"logs"`
}

// Problem: RFC 9457.
type Problem struct {
    Type   string `json:"type"`
    Title  string `json:"title"`
    Status int    `json:"status"`
    Detail string `json:"detail"`
}
```

---

## 4. Interfaces (Contratos internos)

Declaradas en el lado del consumidor. Implementaciones concretas en cada paquete (`Client`, `Service`).

### 4.1 Capa de Servicios (`internal/services/services.go`)

```go
package services

// PDFService orquesta la ingesta y extracción de PDFs.
type PDFService interface {
    // IngestAndExtract valida el PDF, lo envía a Extract, calcula el checksum
    // del texto resultante y registra el evento de auditoría.
    IngestAndExtract(ctx context.Context, pdf []byte) (dto.ExtractPDFResponse, error)
}

// TextService delega el CRUD de texto a Persistence y aplica reglas de negocio.
type TextService interface {
    Create(ctx context.Context, req dto.CreateTextRequest) (dto.CreateTextResponse, error)
    Update(ctx context.Context, checksum models.Checksum, req dto.UpdateTextRequest) (*models.Text, error)
    Delete(ctx context.Context, checksum models.Checksum) (dto.DeleteTextResponse, error)
    FindByChecksum(ctx context.Context, checksum models.Checksum) (*models.Text, error)
}

// AuditService registra y lee logs de auditoría.
type AuditService interface {
    // LogAsync emite un evento de forma asíncrona (fire-and-forget).
    LogAsync(ctx context.Context, event models.AuditEvent)
    // FetchLogs actúa como proxy hacia el MS Audit Log.
    FetchLogs(ctx context.Context, params dto.AuditQueryParams) (dto.AuditLogsResponse, error)
}
```

### 4.2 Capa de Clientes REST (contratos hacia los otros MS)

```go
// internal/clients/extract/client.go
package extract

type Client interface {
    // Extract envía el PDF binario y devuelve el documento extraído.
    Extract(ctx context.Context, pdf []byte) (dto.ExtractedDocument, error)
}

// internal/clients/persistence/client.go
package persistence

type Client interface {
    Create(ctx context.Context, payload dto.CreateTextPayload) error
    Update(ctx context.Context, checksum models.Checksum, payload dto.UpdateTextPayload) (models.Text, error)
    Delete(ctx context.Context, checksum models.Checksum) error
    FindByChecksum(ctx context.Context, checksum models.Checksum) (models.Text, error)
}

// internal/clients/auditlog/client.go
package auditlog

type Client interface {
    Emit(ctx context.Context, event models.AuditEvent) error          // POST /audit/logs
    ListAll(ctx context.Context, skip, limit int) ([]models.AuditLog, error)   // GET /audit/logs
    ListByChecksum(ctx context.Context, checksum models.Checksum) ([]models.AuditLog, error) // GET /audit/logs/checksum/{checksum}
}
```

> **Firmas a cambiar en la Fase 4** (Task 14, ver §6): `auditlog.NewClient` y
> `httpclient.New` pasan a llevar un `token string` que se inyecta como
> `Authorization: Bearer` en toda request. La **interfaz** `Client` no cambia: el token es
> infraestructura de transporte, no parte del contrato de negocio.

```go
// internal/httpclient/client.go — después de la Fase 4 (Task 12)
package httpclient

func New(baseURL string, timeout time.Duration, token string) *Client

func (c *Client) Do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error
```

> `Do` mantiene la regla de la línea `:39`: **todo** `status >= 300` se traduce a un error
> parseando el body como Problem Details. Con la Fase 4, un `401` además queda distinguible
> por el `logger` (Task 15).

> Nota de diseño: `text_service`/`pdf_service` dependen de estas interfaces, por lo que en los tests se inyectan mocks (`extractmock.Client`, `persistencemock.Client`, `auditlogmock.Client`). Los handlers dependen de las interfaces de servicio, así que también se testeán con mocks.

---

## 5. Flujo de Datos (Sequence) — Ingesta de PDF + Auditoría

La operación más compleja, `POST /api/v1/pdfs/extract`, atraviesa las 3 capas así:

```
Cliente                Handler (1)                PDFService (2)                     ExtractClient (3)            AuditLogClient (3)
   │  PDF binario          │                            │                                  │                             │
   │──────────────────────▶│   valida content-type,     │                                  │                             │
   │                       │   tamaño y lee body        │                                  │                             │
   │                       │   IngestAndExtract(ctx,pdf)│                                  │                             │
│                       │───────────────────────────▶│  valida firma %PDF-             │                             │
    │                       │                            │  Extract(ctx, pdf)              │                             │
    │                       │                            │────────────────────────────────▶│                             │
    │                       │                            │   POST /extract (binario crudo) │                             │
    │                       │                            │◀────────────────────────────────│  content, page_count         │
    │                       │                            │  ¿content vacío? → 422          │                             │
    │                       │                            │  markdown.Convert(content)      │                             │
    │                       │                            │  checksum = SHA-256(markdown)   │                             │
    │                       │                            │  evento de auditoría (async)    │                             │
   │                       │                            │───────────────────────────────────────────────▶│  POST /audit/logs
   │                       │                            │                                 (fire-and-forget,        │  (goroutine propia
   │                       │                            │                                  ctx derivado+timeout)   │   con timeout)
   │                       │                            │                                  retorna de inmediato      │
   │                       │                            │◀───────────────────────────────────────────────│  best-effort
   │                       │  200 {checksum,text,       │                                  │                             │
   │◀──────────────────────│       page_count}          │                                  │                             │
```

**Pasos (detallado):**
1. **Handler** (`pdf_handler.go`): valida `Content-Type: application/pdf`, aplica el límite de tamaño (15MB → 413), lee el cuerpo como `[]byte` y llama a `PDFService.IngestAndExtract(ctx, pdf)`.
2. **Service** (`pdf_service.go`): valida la firma mágica (`%PDF-`) → `ErrInvalidPDF` → 400 si falla. Delega en `ExtractClient.Extract(ctx, pdf)`.
3. **Client Extract** (`clients/extract`): hace `POST /extract` con body binario; parsea la respuesta a `dto.ExtractedDocument`; traduce errores HTTP/Problem Details a errores tipados de dominio (→ 502). **No transforma nada**: devuelve `content` crudo, tal cual lo manda upstream.
4. **Service — verificación de contenido**: si `content` está vacío o es sólo whitespace ⇒ el PDF no tiene capa de texto (típicamente un escaneado: sólo imágenes) ⇒ `ErrNoExtractableText` → **422**. Sin este paso, todos los escaneados compartirían el checksum `SHA-256("")` (ver §7.3).
5. **Service — markdown**: `markdown.Convert(content)` (§7). Función pura y determinista.
6. **Service**: calcula `checksum = SHA-256(markdown)`. Como `text` devuelto es ese mismo markdown, se preserva el invariante `checksum == SHA-256(text persistido)`.
7. **Service — auditoría**: crea `models.AuditEvent{Action: OpPDFExtract, Checksum, Details: {page_count}}` y lo pasa a un `auditwriter` interno que lo envía en **goroutine con `context.WithTimeout`** (fire-and-forget). La llamada original **no espera ni propaga errores** de auditoría.
8. **Service**: retorna `dto.ExtractPDFResponse{Checksum, Text, PageCount}` al handler.
9. **Handler**: serializa y responde `200` con JSON.

El mismo patrón se replica en Create/Update/Delete: `text_handler` → `TextService` (regla de inmutabilidad en Update) → `PersistenceClient` → emisión asíncrona de auditoría. El find (`GET /texts/{checksum}`) **omite** el paso 5 por regla de negocio.

---

## Task List (Fases 3-8 — backlog)

Todas las fases están descompuestas en `tasks/todo.md` con criterios de aceptación,
verificación, dependencias y checkpoints. Los issues del repo siguen esa misma
descomposición (`[ORC-XX]` épico + `[ORC-XX.Y]` subissues).

| Fase | Alcance | Estado |
|------|---------|--------|
| **3** | Implementación del orquestador (Tasks 1-10) | **COMPLETADA** |
| **4** | Handoff de auth con AUDA (Tasks 11-17, issues #16-#23) | **DESBLOQUEADA — lista para ejecutar** (AUDA ya implementó el auth, §6) |
| **5** | Contrato real del Extract + markdown + verificación de texto (Tasks 18-27) | **IMPLEMENTADA, EN REVISIÓN** |
| **6** | Stress y pruebas de carga | **IMPLEMENTADA** |
| **7** | Integración con el MS Persistence real (contrato + token) | **PENDIENTE** — issue #24 (`[ORC-09]`) |
| **8** | Validaciones centralizadas + dedup + fin del markdown (Tasks 37-43, §9) | **PLANIFICADA** — issue #34 (`[ORC-10]`) |

---

## 6. Handoff de autenticación con el MS Audit Log (Fase 4 — DESBLOQUEADA)

### 6.0 Estado real: AUDA YA implementó el auth (verificado 07-10-2026)

El bloqueo que difería esta fase ya no existe. Verificado contra el código del repo
`AuditLog/AuditLogMicroservice-pdf-extractext`:

- `app/config.py` declara `service_api_token: SecretStr` **sin default** y rechaza token vacío/blanco.
- `app/main.py` registra `BearerAuthMiddleware` como middleware *outermost*.
- `app/api/auth.py`: abiertas sólo `/health` y `/readyz`; el resto responde `401` con
  `WWW-Authenticate: Bearer` y un `detail` idéntico para los 4 modos de fallo.
- **Puerto real: 8083** (su `SPEC.md` lo fija ahí), no el `8081` que quedó en `config.go` (ver §8 / Task 25).

**Salvedad:** AUDA todavía **no cableó sus endpoints HTTP** (`POST/GET /audit/logs`): no existe
`app/api/routers/audit_logs.py`, `main.py` sólo monta `health`, y las tasks T10/T13 de AUDA siguen
sin cerrar. Por lo tanto:

- **Las Tasks 11-17 (issues #16-#23) SÍ se pueden ejecutar ya**: el token, el header y el
  diagnóstico de `401` no dependen de las rutas de AUDA.
- La **verificación E2E de auditoría** sigue bloqueada hasta que AUDA exponga las rutas. El código
  del orquestador queda listo mientras tanto.

**Decisión (humano, 07-10-2026): Fase 4 desbloqueada.** Se ejecuta el handoff tal como está diseñado.

### 6.1 El contrato (ya implementado en AUDA)

El MS **Audit Log (AUDA)** —`audit-log-microservice-pdf-extractext`, FastAPI— debe cambiar su
contrato para que **todas** sus rutas exijan `Authorization: Bearer <SERVICE_API_TOKEN>`, salvo
`/health` y `/readyz`.

- El middleware de autenticación es *outermost* en su `create_app`: se evalúa **antes** del
  routing, así que incluso una ruta inexistente devuelve `401` y no `404`.
- `SERVICE_API_TOKEN` **no tiene valor por defecto**: AUDA **no arranca** sin ella. Ausente,
  vacía o sólo con espacios ⇒ la app falla al cargar la configuración.
- Los 7 casos de fallo de auth devuelven `401` con `WWW-Authenticate: Bearer` y un `detail`
  **idéntico** (sin oráculo de cuál fue el error).
- `401` nunca se traduce a otro status: el cliente Go debe poder distinguirlo.

### 6.2 Variables y propagación

| Origen | Destino | Dónde |
|--------|---------|-------|
| `AUDIT_LOG_API_TOKEN` (orquestador) | `Config.AuditLogAPIToken` | `internal/config/config.go` |
| `Config.AuditLogAPIToken` | `auditlog.NewClient(..., token)` | `cmd/orchestrator/main.go:35` |
| `token` | `httpclient.New(baseURL, timeout, token)` | `internal/clients/auditlog/client.go:25` |
| `token` | header `Authorization: Bearer` | `internal/httpclient/client.go:24` (`Do`) |

El **mismo valor** debe estar en `AUDIT_LOG_API_TOKEN` (orquestador) y `SERVICE_API_TOKEN`
(AUDA). No hay emisión ni descubrimiento: es configuración explícita en ambos lados.

### 6.3 Reglas de diseño

1. **Sin default.** `AUDIT_LOG_API_TOKEN` ausente ⇒ `config.Load()` falla nombrando la
   variable. Un default vacío o hardcodeado convertiría un `401` en un fallo silencioso.
2. **El token es infraestructura.** Viaja config → composition root → cliente → header. Nunca
   se lee `os.Getenv` fuera de `config`; nunca se loguea.
3. **`401` no es transitorio.** El bucle `emit` (`internal/services/audit_service.go:28-39`)
   reintenta `maxEmitAttempts` veces y termina en `logger.Warn`. Ante un `401` debe cortar
   el reintento (1 intento) y reportar con `logger.Error`: es un error de **cableado**, y
   reintentar sólo retrasa el diagnóstico. Los `5xx`, timeouts y errores de red **siguen**
   reintentando con `Warn`.
4. **No cambia el contrato de negocio.** `LogAsync` sigue siendo fire-and-forget y no
   propaga el error al llamador (decisión de la Task 4). La Fase 4 sólo cambia el
   **diagnóstico**.
5. **Paginación de `ListByChecksum` es opcional.** AUDA no aplica `.limit()` a ese cursor
   (decisión **D12** de su `SPEC.md` §8.1): devuelve el array completo a propósito. Sólo
   agregar `skip`/`limit` si el volumen real por checksum lo justifica.

### 6.4 Orden de despliegue

1. Mergear la Fase 4 del orquestador (Tasks 11-16).
2. Configurar `AUDIT_LOG_API_TOKEN` en el orquestador **y** `SERVICE_API_TOKEN` en AUDA, con
   el mismo valor.
3. Recién entonces, el primer arranque de AUDA.

Si se arranca AUDA antes, AUDA no levanta. Si el orquestador no manda el header, los eventos
de auditoría se pierden en silencio (fire-and-forget). **Ninguno de los dos órdenes relativos
produce un fallo ruidoso** — por eso `401` debe verse como `Error`.

---

## 7. Markdown y verificación de contenido (Fase 5)

### 7.1 Qué se puede y qué no se puede hacer aquí

El Extract entrega `content` como **un único string plano**, sin tamaño de fuente, sin peso y sin
coordenadas (`extractor.go:90` sólo extrae texto plano). **Desde el orquestador es
imposible detectar títulos, listas ni tablas reales**: no hay datos de layout en el cable. Todo
lo que sigue son transformaciones **cosméticas** sobre texto, no reconstrucción de estructura.

Lo que sí es determinable sin datos de layout:

| Transformación | Por qué funciona |
|----------------|------------------|
| **Des-hifenización** | pdf_oxide corta palabras con guion al final de línea. Unir `"conoci-\ndo"` → `"conociendo"` es inequívoco. |
| **Unwrap de párrafos** | `pdf_oxide` corta línea cada ~80 columnas; el texto llega como una tira de líneas sueltas que en markdown se lee peor. |
| **Headers/pies repetidos** | La 1ª y última línea de cada página se repiten en todas las páginas de un libro. Se exige repetición en **todos** los segmentos, porque el `"\n\n"` no marca páginas de forma fiable (§7.4.1). |
| **Títulos por mayúsculas** | Las cabeceras de sección suelen ir en mayúsculas sostenidas. Es un *proxy*, no detección de layout. |
| **Separar título del cuerpo por ancho** | Un título no se envuelve a lo ancho de la página; un párrafo sí. Es lo único que distingue ambos cuando no hay renglones vacíos (§7.4.1). |

> Para markdown estructural de verdad (jerarquía real de títulos, listas anidadas, tablas), el
> Extract tendría que exponer los *text runs* con `Size`/`Weight`/`Box`. Su librería fijada
> (`pdf_oxide`) ya expone el texto estructurado (Markdown) en su binding de Go
> — una llamada por página. Eso es trabajo del **equipo de Conversor**, fuera de este repo.

### 7.2 El invariante del checksum

`checksum = SHA-256(markdown devuelto)`, y ese mismo markdown es lo que el cliente persiste vía
`POST /api/v1/texts {text, checksum}`. Por lo tanto:

```
checksum == SHA-256(text persistido)      ← sigue siendo cierto
```

Ésta es la razón de calcularlo sobre el markdown y no sobre el texto crudo: mantiene el
invariante central del sistema sin cambiar el nombre del campo ni tocar a los clientes.

**Consecuencia a aceptar:** el checksum ahora depende también del algoritmo de markdown y de
cómo pdf_oxide corta las líneas. Un upgrade de pdf_oxide puede cambiar el checksum de un PDF que antes
daba el mismo valor. Ese acoplamiento ya existía (el checksum dependía del texto crudo que
produce pdf_oxide); la conversión lo hace explícito.

### 7.3 Verificación: ¿el PDF tiene texto?

Un PDF escaneado es un PDF perfectamente válido: tiene páginas, pero su contenido son imágenes.
El Extract devuelve `content: ""` con `page_count > 0`. Sin verificación, el orquestador
respondería `200` con `text: ""` y un checksum = `SHA-256("")` **compartido por todos los
escaneados** — una colisión de ID garantizada.

Regla: si `strings.TrimSpace(content) == ""` ⇒ `ErrNoExtractableText` ⇒ **422**. Es una regla
de negocio y vive en la **capa de servicio**, no en el cliente ni en el handler.

#### 7.3.1 Qué se verifica realmente (y qué no)

La regla detecta **"el Extract no devolvió texto"**, no "el PDF es una imagen". No se miran las
imágenes: no hay nada en el contrato que las describa. Para el caso 100 % escaneado las dos
preguntas son equivalentes, pero conviene no confundirlas.

**Verificado contra el Extract real** (Task 27) con un PDF generado como imagen rasterizada, sin
`/Font` y sólo con `/Image` XObject:

```
POST /extract (Extract real)     -> 200 {"content":"","page_count":1}
POST /api/v1/pdfs/extract (orq.) -> 422 "pdf has no extractable text layer"
                                    y CERO intentos de auditoría (0 menciones en el log)
```

Es decir: el Extract **sí** devuelve `content: ""` con `page_count > 0` para un escaneado, que era
la suposición de esta sección y nunca se había comprobado. Y la ausencia de auditoría queda
confirmada contra el servicio real, no sólo contra el stub.

Para reproducirlo:

```python
from PIL import Image, ImageDraw
img = Image.new("RGB", (1240, 1754), "white")   # A4 a 150 dpi
ImageDraw.Draw(img).text((80, 80), "SCANNED DOCUMENT - NO TEXT LAYER", fill="black")
img.save("scanned.pdf", "PDF", resolution=150.0)
```

Lo que **queda fuera**, por diseño y no por olvido:

- **PDF parcialmente escaneado:** 89 páginas de imagen + 1 con capa de texto pasa el chequeo global
  y devuelve un markdown casi vacío. El dato existe (`page_count` vs. segmentos con texto); el
  umbral es decisión de producto (§8.9).
- **PDF con capa de texto basura:** un OCR pobre produce `content` no vacío y por lo tanto pasa. No
  se evalúa la calidad del texto extraído, sólo su ausencia.

> **Lo que NO se resuelve acá:** un PDF con 90 páginas escaneadas y 1 con capa de texto **pasa**
> el chequeo global. El dato para detectarlo existe (`page_count` vs. cantidad de segmentos con
> texto), pero elegir un umbral es una decisión de producto. Queda como pregunta abierta (§8.9).

### 7.4 El pipeline

`internal/markdown` es un paquete **puro**, sin dependencias ni I/O — igual que
`internal/checksum`. La firma es deliberadamente simple:

```go
package markdown

// Convert transforma el texto plano del MS Extract en markdown.
func Convert(content string) string
```

Orden de las etapas, que **no es libre** (ver los dos *guards*):

```
1. split por "\n\n"                   ← el Extract une páginas con \n\n, pero NO es delimitador inequívoco (ver abajo)
2. quitar headers/pies repetidos      ← necesita estructura de LÍNEA ⇒ antes del unwrap
3. agrupar renglones en párrafos       ← corta en los renglones vacíos
4. separar títulos pegados al cuerpo  ← por ancho de renglón ⇒ todavía sobre LÍNEAS
5. de-hifenizar dentro de cada párrafo ← necesita ver el "-"\n de la línea siguiente
6. decidir título (ALL-CAPS) y escapar ← ya sobre el párrafo aplanado
7. join de párrafos con "\n\n"
```

**No se normaliza `\r\n`.** Los renglones se recortan con `TrimSpace` al partir la página, y
`TrimSpace` ya descarta el `\r`. Una pasada extra sería código muerto.

### 7.4.1 `"\n\n"` NO es un delimitador de página inequívoco

El plan anterior afirmaba que sí lo era. **Los goldens de la Task 27 lo refutaron.**

`extractor.go:59` une páginas con `"\n\n"`, pero pdf_oxide también emite renglones vacíos *dentro* de
una página, y esos renglones vacíos se convierten en `"\n\n"` al normalizar. Con los 4 PDFs del
corpus:

| PDF | `page_count` | segmentos por `"\n\n"` | renglones vacíos internos |
|-----|--------------|------------------------|---------------------------|
| Scrum Guide (es) | 16 | 16 | 28 |
| Kanban Esencial | 90 | 90 | **0** |
| Filosofía Lean | 42 | 41 | 0 |
| Scrum Manager | 62 | 61 | 0 |

Sólo el Scrum Guide tiene una coincidencia exacta de páginas y segmentos. En los otros tres, un
`"\n\n"` es **casi siempre** un salto de párrafo interno, no un cambio de página.

Consecuencias asumidas:

- **La detección de headers/pies se abstiene.** Un renglón de borde sólo se considera repetido si
  aparece en el mismo borde de *todos* los segmentos elegibles. Ante la duda no se borra nada: perder
  una línea de contenido es peor que dejar un header pegado.
- **La separación título/cuerpo se recupera por ancho, no por página.** Como dentro de una página
  no hay señal de párrafo, un renglón corto se considera título sólo si el renglón *siguiente* ocupa
  ancho completo. Sin ese criterio, el Kanban (cero renglones vacíos en 90 páginas) saldría como
  ~89 párrafos gigantes.

> **Limitación que queda, y no se puede resolver acá:** dentro de una página, un párrafo que
> empieza con un renglón corto seguido de uno largo **se corta de más**. Se prefiere un corte
> cosmético equivocado a un título fundido con el cuerpo, pero es un error de formato, no de datos:
> ningún texto se pierde, y `TestConvertNeverLosesWordsFromRealPDFs` lo verifica.

Los dos *guards* que respetan el orden:

- **Headers/pies (etapa 2) y títulos (etapa 3) antes del unwrap (etapa 5).** Después del unwrap
  la estructura de línea ya no existe: no hay forma de saber dónde terminaba una página ni qué
  línea era un título.
- **De-hifenización (etapa 4) antes del unwrap (etapa 5).** El unwrap junta todas las líneas
  del párrafo; si corriera antes, el guion dejaría de estar pegado a un fin de línea y la
  señal se perdería.

**Determinismo (requisito duro).** El checksum es el ID del sistema, así que la conversión debe
ser una función pura de `content`: nada de `time`, `rand`, iteración de mapas que afecte el
orden, ni casing dependiente de locale. La estabilidad de la conversión se testea explícitamente.

---

## 8. Integración con el MS Persistence (Fase 7)

> **Issue #24 (`[ORC-09]`), subissues #25-#33.** Este apartado reemplaza la "propuesta de
> integración" de §1.2/§4.2, escrita cuando el repo de Persistence sólo tenía `README.md`.

### 8.1 Contrato real (verificado contra el código)

Persistence **no** usa `/api/v1` ni el checksum en el path; su ruta base es `/texts`:

| Operación | Método y ruta reales | Cuerpo | Respuesta |
|-----------|----------------------|--------|-----------|
| **Create** | `POST /texts` | `{text, checksum, name?, metadata?}` | `201 {message:"OK", checksum}` + header `Location` |
| **Read** | `GET /texts?checksum=<cs>` | — | `200 TextOut` |
| **Update** | `PUT /texts?checksum=<cs>` | `{name?, metadata?}` (al menos uno) | `200 TextOut` |
| **Delete** | `DELETE /texts?checksum=<cs>` | — | `200 {message:"OK", checksum}` |

`TextOut` = `{checksum, text, name, metadata, created_at, updated_at}` con fechas RFC 3339 (ms).
El checksum viaja como **query param**, escapado con `url.QueryEscape` (no `url.PathEscape`).
`PUT` exige al menos un cambio: `null` cuenta como campo **ausente** y `""`/`{}` como
**limpiar** el campo; un body sin cambios efectivos ⇒ `400 "no changes requested"`. Por eso el
orquestador modela `name` como `*string` y `metadata` como `map[string]any` **sin `omitempty`**
(nil ⇒ `null` ⇒ sin cambio; `""`/`{}` ⇒ limpiar). Ver Task 30 (#29).

### 8.2 Auth: dos secretos separados

AUDA y Persistence validan cada uno su propio `SERVICE_API_TOKEN`, así que el orquestador
necesita **dos variables independientes**, ambas sin default:

| Orquestador | Servicio | Header |
|-------------|----------|--------|
| `AUDIT_LOG_API_TOKEN` | AUDA (`:8083`) | `Authorization: Bearer <token>` |
| `PERSISTENCE_API_TOKEN` | Persistence (`:8000`) | `Authorization: Bearer <token>` |

`httpclient.New(baseURL, timeout, token)` (compartido, Fase 4 / #18) es la base: cada `client`
pasa el suyo.

### 8.3 De la propuesta vieja al contrato real

| Asumido antes | Real | Issue |
|---------------|------|-------|
| `PUT/DELETE/GET /api/v1/texts/{checksum}` | `/texts?checksum=...` | #28 |
| sin auth | `Authorization: Bearer <SERVICE_API_TOKEN>` | #25-#27 |
| Puerto `:8082` | `:8000` | #32 |
| `UpdateTextRequest` siempre manda `name`+`metadata` | `*string`/`map` nil sin `omitempty`: `null`=sin cambio, `""`/`{}`=limpiar | #29 |
| `401` de Persistence propagado tal cual | error de cableado, no reenviar `401` | #30 |

### 8.4 Puerto de AUDA

El default de `AUDIT_LOG_BASE_URL` era `:8081` (de un `EXPOSE` viejo); AUDA corre en **`:8083`**
según su `SPEC.md`. Se corrige en #32 junto con el `:8000` de Persistence.

---

## Riesgos y Mitigaciones

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| **Contrato real de Persistence difiere del asumido** | **Alto** | Persistence ya está implementado con rutas `?checksum=`, auth Bearer y puerto `:8000`. El client actual (`/texts/{checksum}`, sin token) fallaría con `404`/`401` en cuanto se apunte al servicio real. Mitigación: Fase 7 (issues #24-#33), contratos en §8. |
| Fire-and-forget pierde eventos si Audit Log falla | Medio | `context.WithTimeout` acotado + 1 reintento + log local del fallo. Opción futura: cola interna. Nunca degrada la respuesta al cliente. |
| PDFs enormes / malformados | Alto | Límite de tamaño en handler (413) + validación de firma `%PDF-` en service (400). Nuestro límite (15MB) es más estricto que el del Extract (50MB), así que el nuestro dispara primero. |
| **PDF escaneado (sólo imágenes) ⇒ `content` vacío** | **Alto** | Sin verificación, todos los escaneados devolverían el checksum `SHA-256("")`: colisión de ID garantizada. Mitigación: `ErrNoExtractableText` → **422** (§7.3, Task 23). Un escaneado parcial (90 de 90 páginas sin texto salvo 1) **queda fuera**: ver pregunta abierta 8. |
| **El checksum depende del algoritmo de markdown y del corte de línea de pdf_oxide** | Medio | Un upgrade de pdf_oxide puede cambiar el checksum de un PDF que antes daba el mismo valor. Aceptado explícitamente (§7.2). Mitigación operativa: si algún día hay que re-hashear, es una migración con `409` de por medio, no un hotfix. |
| El markdown es cosmético y puede leerse peor que el texto crudo en algunos PDFs | Medio | Transformaciones conservadoras (de-hifenizar, unwrap, quitar repetidos). Los títulos por mayúsculas sólo se aceptan con umbrales estrictos. **Resuelto y medido en Task 27**: goldens contra los 4 PDFs del corpus más `TestConvertNeverLosesWordsFromRealPDFs`, que verifica que no se pierde texto aunque el formato sea imperfecto. |
| Quitar headers/pies borra contenido legítimo | Medio | Criterio **fail-seguro**, no de umbral: un borde sólo se borra si aparece en el mismo borde de **todos** los segmentos elegibles, y sólo en las líneas de borde. Ante la ambigüedad del `"\n\n"` (§7.4.1) se prefiere dejar el header pegado antes que perder contenido. Mínimo 2 segmentos para considerar repetición (Task 21). |
| Contratos de MS hermanos cambian | **Alto** | Ya ocurrió con el Extract (`text`→`content`, `pages`/`duration_ms` eliminados) y lo aggravó que `encoding/json` ignorara el campo faltante en silencio. Mitigación: el DTO de entrada calca el contrato upstream y hay un **contract test con la forma real** (§ Task 24). |
| **AUDA exige `Authorization: Bearer` y no arranca sin `SERVICE_API_TOKEN`** | Medio | **Ya aplica**: AUDA implementó el auth y **no arranca sin token** (§6.0). El `LogAsync` fire-and-forget **oculta** el `401`, así que la auditoría se pierde en silencio. Mitigación: Fase 4 (issues #16-#23) antes del primer arranque de AUDA con el auth. Los **endpoints HTTP** de AUDA siguen pendientes: la verificación E2E espera a que los cablee. |
| **Base URLs por defecto a puertos inexistentes (`:8082`, `:8081`)** | Medio | Persistence escucha en `:8000` y AUDA en `:8083`; con los defaults viejos todas las llamadas fallan. Mitigación: #32 actualiza `config.go` y `compose.yaml`. |
| **Token filtrado en logs** | Alto | El token nunca se loguea ni se imprime en `fmt.Stringer`; viaja config → composition root → header. Cubierto por los tests previstos en Tasks 11-16. |

## Preguntas Abiertas

1. **¿Este repo es el destino de implementación?** Este plan asume `ValidationMicroservices-pdf-extractext` (directorio actual). Existe un repo hermano `orchestrator/orchestrator-microservice-pdf-extractext` (Go, chi) con scaffolding básico. ¿Se implementa aquí, allá, o se descarta ese scaffolding? *Resuelto: se implementa aquí.*
2. **Nombre del módulo Go:** se propone `validationmicroservices-pdf-extractext` (Go exige minúsculas). ¿Confirmar? ¿Usar una ruta tipo `github.com/<org>/...`? *Resuelto: se confirma el nombre sin dominio; el módulo no es importable externamente, lo cual es aceptable porque el código es 100 % `internal/`.*
3. **Create: `name` y `metadata`, ¿obligatorios u opcionales?** Se asume `name` opcional y `metadata` opcional.
4. **Delete: ¿`200` + JSON (asumido) o `204`?**
5. ~~**¿El MS Extract devuelve páginas y el texto completo (`text`); el checksum se calcula sobre `text`?**~~ **RESUELTO y la premisa era falsa.** El Extract devuelve `content` (un string) y `page_count`; **no** devuelve páginas. El checksum se calcula sobre el markdown derivado de `content` (§7.2).
6. **¿Dónde se genera y rota `AUDIT_LOG_API_TOKEN` / `PERSISTENCE_API_TOKEN` / `SERVICE_API_TOKEN`?** **Ya aplica**: AUDA y Persistence exigen cada uno su `SERVICE_API_TOKEN` sin default. La §6 asume configuración explícita con el mismo valor por par (orquestador↔servicio). **Falta decidir** el emisor (secret manager, `.env` compartido, variable del orquestador de infra) y la política de rotación: si se rota, hay que reiniciar el servicio y el orquestador a la vez o el `401` aparece en uno de los dos. Son **dos secretos independientes** (AUDA y Persistence), no uno compartido entre ambos.
7. **¿`401` debe ser distinguible por código en el `Problem`?** Hoy se detectaría por status (`401`) en el cliente Go. Si AUDA promete un `code` estable (p. ej. `UNAUTHORIZED`), conviene matchear ese `code` en vez del status numérico.
8. **Payload de auditoría `details`:** se asume `{page_count}` para extract y `{name}` para CRUD. Definir el contenido exacto si el MS Audit Log lo exige.
9. **¿Umbral para "PDF mayormente escaneado"?** El chequeo global de §7.3 sólo rechaza el 100 % sin texto. Un PDF con 1 página de texto entre 90 escaneadas pasa y devuelve un markdown casi vacío. El dato para detectarlo existe (`page_count` vs. segmentos con texto) pero el umbral es decisión de producto. **Sin resolver: la Fase 5 no lo implementa.**
10. **¿Le pedimos al equipo del Extract un `GET /healthz`?** Hoy no existe, y por eso `/readyz` no puede verificar la dependencia (§1.4). Sin endpoint de salud del Extract, un readiness real no es posible.
11. **¿Conflictos de puerto en desarrollo local?** El Extract tomó el `8080` y el orquestador tiene `defaultPort = "8080"`. En local hay que setear `PORT` explícitamente para el orquestador. ¿Se cambia el default a otro puerto, o se documenta el override?
12. **¿Títulos en mayúsculas: mantener las MAYÚSCULAS o pasarlas a Title Case?** Se asume **mantenerlas tal cual** y sólo prefijar `## `, porque title-caser español rompe acentos y nombres propios (`ÍA`, `Ñ`, `JR`). Revisar si el resultado visual incomoda.

---

## 9. Fase 8 — Validaciones centralizadas del PDF + dedup + baja del markdown (2026-10-08)

> **Decisiones del humano (08-10-2026).** La Fase 8 se agrega al plan existente **sin tocar** las
> tareas sin cerrar de las Fases 4-7. **Esta sección enmienda §1.1, §7 y §11** (no las reescribe).
> **Malware queda fuera de alcance** (decisión explícita).
>
> **Rediseño (misma fecha).** Tras una primera versión de esta sección, el humano pidió
> (a) **centralizar** todas las validaciones en un único paquete y (b) calcular el **checksum
> sobre los bytes del PDF** para hacer **dedup contra Persistence antes de extraer**. Para el
> `page_count` en cache hits se evaluaron tres opciones (§9.5) y se eligió la **Opción 1**
> (`pdfcpu` antes del dedup, sin persistir `page_count`), con lo cual **el contrato de
> Persistence no cambia**. Este rediseño **reemplaza** la versión previa de §9 (paquete
> `internal/pdfvalidate`, checksum sobre `content`, sin dedup).

### 9.1 Alcance (estado final)

| # | Validación | Implementación | Error → Status |
|---|-----------|----------------|----------------|
| 1 | Extensión del archivo | Header opcional `X-Filename`; si viene, debe terminar en `.pdf` (case-insensitive, normalizando `/` y `\`) | `ErrUnsupportedExtension` → `415` |
| 2 | Firma `%PDF-` | `internal/validation` (bytes) | `ErrInvalidSignature` → `400` |
| 3 | Estructura / PDF corrupto (xref, trailer, objetos) | `pdfcpu` **relaxed** | `ErrMalformedPDF` → `400` |
| 4 | Contraseña / encriptado | `pdfcpu`, clasificado como encriptado y **no** como corrupto | `ErrEncryptedPDF` → `422` |
| 5 | Cantidad de páginas | `MAX_PDF_PAGES` (default **1000**, `0` = sin límite), contada por `pdfcpu` | `ErrTooManyPages` → `422` |
| 6 | Texto extraíble (post-Extract) | `internal/validation.ValidateExtracted` | `ErrNoExtractableText` → `422` |
| 7 | ~~Malware~~ | — | fuera de alcance |
| 8 | Baja de la conversión a markdown | `text = content` del Extract (ya viene formateado) | — |

Estado final del checksum: **`checksum = SHA-256(bytes del PDF)`**. Esto **rompe** el invariante
histórico `checksum == SHA-256(text)` (que era `SHA-256(markdown)`); el motivo es el dedup (§9.3).

Las validaciones 1-5 son **previas** a delegar en Extract: si el PDF se rechaza localmente, el
Extract no gasta CPU.

### 9.2 Contrato de errores (enmienda a §1.1)

Todos los errores de validación viven en **`internal/validation`**, con sentinelas únicos y
`StatusOf(err) (status int, title string, ok bool)`; el handler deja de tener un `switch` de
errores. La extensión deja de validarse en el handler y pasa a este paquete (§9.4.7).

| Condición | Dónde | Error tipado | Status |
|-----------|-------|--------------|--------|
| `Content-Type` ≠ `application/pdf` | middleware (ya existe) | — | `415` (ya) |
| Body > 15 MB | handler (ya existe) | `errPayloadTooLarge` | `413` (ya) |
| `X-Filename` presente y no termina en `.pdf` | `internal/validation` | `ErrUnsupportedExtension` | **`415`** |
| Sin firma `%PDF-` | `internal/validation` | `ErrInvalidSignature` | `400` |
| Estructura inválida / PDF corrupto | `internal/validation` (pdfcpu) | `ErrMalformedPDF` | **`400`** |
| Encriptado / requiere contraseña | `internal/validation` (pdfcpu) | `ErrEncryptedPDF` | **`422`** |
| `page_count` > `MAX_PDF_PAGES` | `internal/validation` | `ErrTooManyPages` | **`422`** |
| Extract devolvió non-2xx o timeout | client (ya existe) | — | `502` (ya) |
| Sin texto extraíble (escaneado) | `internal/validation` | `ErrNoExtractableText` | `422` |
| Persistence caído en el lookup de dedup | service | — (fail-open: se extrae igual) | `200` |

Los `422` siguen la filosofía ya documentada en §1.1: `400` = "esto está mal formado",
`422` = "es un PDF real pero no se puede procesar en este sistema" (encriptado, demasiadas
páginas, sin capa de texto). El `415` de extensión es coherente con el `415` de Content-Type:
"este medio/archivo no lo soportamos".

### 9.3 Flujo (Opción 1)

```
POST /api/v1/pdfs/extract
 1. middleware  → Content-Type application/pdf             → 415   [queda en HTTP]
 2. handler     → MaxBytesReader 15 MB                      → 413   [queda en HTTP]
 3. validation  → X-Filename (.pdf) · firma %PDF-           → 415/400
 4. validation  → pdfcpu: estructura / encriptado / páginas → 400/422  (devuelve pageCount)
 5. service     → pdfSum = SHA-256(bytes del PDF)
 6. service     → Persistence FindByChecksum(pdfSum)
        · HIT   → 200 {checksum: pdfSum, page_count: pageCount, text: registro.text}
                   (NO Extract, NO auditoría)
        · miss  → continúa
        · error → fail-open: log warning y continúa
 7. service     → Extract MS                                → 502
 8. validation  → content sin texto extraíble               → 422
 9. service     → auditoría async + 200 {pdfSum, pageCount, text}
```

`pageCount` sale de **pdfcpu** en ambos caminos (fuente única; el `page_count` del Extract deja de
usarse para la respuesta). El invariante de dedup es: mismos bytes ⇒ mismo checksum ⇒ mismo
registro. **Fail-open** significa que, si Persistence no responde, se extrae igual (se pierde la
optimización, no la respuesta).

### 9.4 Decisiones de arquitectura (Fase 8)

1. **Paquete único `internal/validation`** (se descarta el nombre `internal/pdfvalidate`): acá
   viven **todas** las validaciones, para que no queden dispersas entre handler, service y un
   paquete de pdfcpu. API:
   - `errors.go`: sentinelas + `StatusOf(err)` / `TitleOf(err)`.
   - `validation.go`: `Input{PDF []byte; Filename string; MaxPages int}`, `Result{PageCount int}`,
     `PreExtract(Input) (Result, error)` (cadena fail-fast: extensión → firma → estructura →
     páginas) y `ValidateExtracted(content string) error`.
   - `extension.go`, `signature.go`, `structure.go` (pdfcpu), `pages.go`, `textlayer.go`.
   - Librería: **`github.com/pdfcpu/pdfcpu`** (pure Go, sin cgo; v0.16 requiere Go ≥ 1.26 y el
     módulo ya es `go 1.26.8`).
2. **Validación relaxed, no strict.** La validación estricta de pdfcpu rechaza PDFs reales
   válidos (ver sus issues: strings `/DA` de anotaciones, etc.). Relaxed detecta corrupción real
   sin falsos positivos sobre el corpus (`tests/stress/pdfs/`: 4 PDFs reales).
3. **Estado sin disco: `api.DisableConfigDir()` / config stateless.** `compose.yaml` corre el
   contenedor con `read_only: true`; pdfcpu con config por defecto intenta crear su directorio de
   configuración y fallaría. Esto es obligatorio, no opcional.
4. **Detección de encriptado ≠ corrupción.** `pdfcpu` falla en ambos casos; hay que clasificar el
   error (preferir sentinela tipada si existe; si no, match del mensaje) y devolver
   `ErrEncryptedPDF` distinto de `ErrMalformedPDF`. Criterio de aceptación duro: un PDF encriptado
   **nunca** puede caer en `400` ni llegar a Extract. El fixture encriptado se **genera en el
   test** con `api.Encrypt` (no se commitea un binario).
5. **Checksum sobre los bytes del PDF + dedup.** `pdfSum` se calcula una vez y sirve para (a)
   buscar en Persistence y (b) ser el `checksum` de la respuesta. En un **hit** se responde desde
   el registro almacenado (sin Extract ni auditoría); si Persistence **falla**, fail-open. `400`
   (corrupto) no es lo mismo que "no está en la BD" (`404`): el miss se detecta con
   `httpclient.IsNotFound(err)`.
6. **Content-Type y tamaño siguen en la capa HTTP** (necesitan header/stream antes de leer el
   body); **el CRUD de `/texts` queda fuera de alcance** (es otro endpoint).
7. **La extensión se valida en `internal/validation`**, no en el handler: por eso la firma del
   service pasa a `IngestAndExtract(ctx, pdf []byte, filename string)`. El header es **opcional y
   no autentica** (cualquiera lo forja); lo que protege son las validaciones de contenido.
8. **`page_count` NO se persiste**: se resuelve con pdfcpu (local y barato) tanto en hit como en
   miss. **El contrato de Persistence no cambia** (no se toca la SPEC del MS Persistence).
9. **`MAX_PDF_PAGES` default 1000, `0` = sin límite** (env + `config.go`, simétrico a
   `MAX_PDF_SIZE_BYTES`).

### 9.5 Opciones evaluadas para `page_count` en cache hits

| Opción | Contrato Persistence | Costo | Veredicto |
|--------|----------------------|-------|-----------|
| **1. pdfcpu antes del dedup** | no cambia | parseo local en cada request (barato vs. el Extract que se evita) | **elegida** |
| 2. `page_count` dentro de `metadata` | no cambia (free-form) | mutable/sin tipo; el cliente debe reenviarlo entre dos requests | descartada |
| 3. campo `page_count` de primer nivel | **cambia** (SPEC v4 + modelos + tests) | coordinación con el equipo de Persistence | descartada |

La Opción 2 se descartó porque `metadata` es mutable en `PUT /texts` (un update podría borrar el
`page_count`), no tiene tipo, y obliga a que el cliente reenvíe el valor entre el extract y el
create.

### 9.6 Riesgos (Fase 8)

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| pdfcpu rechaza PDFs reales válidos (falsos positivos) | Alto | relaxed + los 4 PDFs de `tests/stress/pdfs/` como criterio duro (Tasks 39 y 43). Si el corpus falla, degradar a detección de corrupción gruesa. |
| Detección de encriptado por mensaje de error de pdfcpu (string inestable entre versiones) | Medio | Preferir sentinela tipada; si no, match amplio + fixture encriptado generado en test (verificar con `source-driven-development`). |
| pdfcpu escribe en disco y el contenedor es `read-only` | Alto | `api.DisableConfigDir()`/stateless obligatorio (§9.4.3) + e2e en compose. |
| Latencia por parsear pdfcpu en **cada** request (incluidos hits) | Medio | Es local y barato frente al Extract que se evita; medir en Task 43. Si crece mucho, reconsiderar la Opción 2/3. |
| **Cambian los checksums**: antes `SHA-256(markdown)`, ahora `SHA-256(bytes)`. Los registros viejos (checksum sobre texto) **nunca** matchean el lookup nuevo | Alto | Aceptar y avisar antes de desplegar: no hay migración; un PDF ya persistido se re-extrae y, si se re-crea, da `409`. |
| Fail-open oculta que Persistence está caído | Bajo | Log warning en cada fallo de lookup; la extracción continúa igual. |
| `X-Filename` se forja trivialmente | Bajo | Conveniencia, no seguridad; protegen las validaciones de contenido (§9.4.7). |

### 9.7 Task List — Fase 8 (índice)

Tareas detalladas en `tasks/todo.md` (Tasks 37-43, issues #34-#41).

| Orden | Task | Issue | Alcance |
|-------|------|-------|---------|
| `{37, 38}` (paralelas) | 37: eliminar markdown · 38: `internal/validation` núcleo (errors/extension/signature/textlayer) | #35 / ORC-10.1 · #36 / ORC-10.2 | M |
| → 39 | pdfcpu: estructura + encriptado + `PageCount` en `internal/validation` | #37 / ORC-10.3 | M |
| **Checkpoint A** | tras Tasks 37-39 | | |
| → 40 | `MAX_PDF_PAGES` + gate de páginas | #38 / ORC-10.4 | S |
| → 41 | integrar `internal/validation` en service/handler (`X-Filename`, `StatusOf`, CORS, wiring) | #39 / ORC-10.5 | M |
| → 42 | dedup + checksum-over-PDF (`FindByChecksum`, hit/miss/fail-open, sin audit en hit) | #40 / ORC-10.6 | M |
| **Checkpoint B** | tras Task 42 | | |
| → 43 | regresión integral (integración, stress, k6, README) | #41 / ORC-10.7 | M |