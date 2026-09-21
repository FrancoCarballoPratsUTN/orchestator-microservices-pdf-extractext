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
| **Extract** (Rust/axum) | `extract/extract-text-microservice-pdf-extractext` | `POST /extract`, body = PDF crudo (binario, sin base64), límite 15MB. Respuesta: `{ "page_count", "pages":[{page_number,text}], "text", "duration_ms" }`. Errores RFC 9457. |
| **Audit Log** (Python/FastAPI) | `AuditLog/AuditLogMicroservice-pdf-extractext` | `POST /audit/logs` → 201 con `{action, entity_type, checksum, details, performed_at, _id}`. `GET /audit/logs?skip&limit`. `GET /audit/logs/checksum/{checksum}`. |
| **Persistence** | `persistence/PersistenceMicroservices-pdf-extractext` | **No implementado aún.** El contrato cliente→persistence definido en este documento es la propuesta de integración. |

---

## Architecture Decisions

1. **3 capas estrictas y unidireccionales.** `handlers` (transporte HTTP + validación de entrada) → `services` (reglas de negocio, orquestación, checksum, auditoría) → `clients` (REST a otros MS). Nunca se llaman capas horizontales; los handlers nunca hablan HTTP con otros MS.
2. **Checksum = SHA-256 (hex) sobre el texto extraído.** Es el ID único de todo el sistema. Se modela como tipo propio `models.Checksum` (no `string` a secas) para prevenir confusiones de tipos a nivel de compilación.
3. **Auditoría fire-and-forget.** El envío al MS Audit Log se ejecuta en una goroutine separada con `context.WithTimeout` derivado; nunca bloquea la respuesta del endpoint ni propaga errores al cliente (best-effort: reintento simple + log local ante fallo).
4. **DI por constructores.** `NewService(clients..., cfg)` / `NewHandler(services..., logger)`. Las interfaces se declaran en el lado del consumidor (máxima testabilidad con mocks).
5. **`context.Context` primero en todas las firmas** (handlers, services, clients). Timeouts por destinatario vía configuración.
6. **Clientes "tontos".** Los clientes REST solo serializan/deserializan y traducen transporte HTTP → errores tipados de dominio. Nunca toman decisiones de negocio.
7. **Errores RFC 9457 (`application/problem+json`).** El orquestador emite y consume Problem Details (consistente con el MS Extract).
8. **Inmutabilidad por contrato.** En `Update` el servicio valida y rechaza intentos de modificar `text`/`checksum` *antes* de delegar a Persistence (regla de negocio en la capa de servicio, no solo en el cliente).
9. **Convención de nombres Go:** cada contrato es la interfaz `Client` / `Service` dentro de su paquete (`extract.Client`, `persistence.Client`, `auditlog.Client`), consumida como `internal.Service`.
10. **módulo Go:** se propone `validationmicroservices-pdf-extractext` (los nombres de módulo no admiten mayúsculas; según el nombre de directorio actual).

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
  "checksum":    "d4a5...",          // SHA-256 hex del texto extraído (ID único del sistema)
  "page_count":  10,
  "text":        "texto extraído..."
}

400 Bad Request (Problem): PDF inválido / no es PDF / body vacío
413 Payload Too Large (Problem): supera el límite
502 Bad Gateway (Problem): MS Extract no disponible o devolvió error
```

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
GET /readyz   → 200 {"status":"ok"} (verifica conectividad a dependencias)
```

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
│   └── httpclient/                  # HTTP client compartido: contextos, timeouts,
│       └── problem.go               #   parsing de RFC 9457 (Problem Details)
├── go.mod
├── go.sum
└── .gitignore                       # Adaptado a Go (ver tarea 1)
```

Los directorios `handlers/`, `services/` y `clients/` son las 3 capas. `models/` y `dto/` contienen los contratos de datos; `httpclient/` y `checksum/` son utilidades transversales. No hay `pkg/` porque el código es 100 % privado del microservicio (`internal/`).

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

// ExtractedDocument: forma del MS Extract (contrato real, v2).
type ExtractedPage struct {
    PageNumber int    `json:"page_number"`
    Text       string `json:"text"`
}

type ExtractedDocument struct {
    PageCount  int             `json:"page_count"`
    Pages      []ExtractedPage `json:"pages"`
    Text       string          `json:"text"`
    DurationMs uint64          `json:"duration_ms"`
}

type ExtractPDFResponse struct {
    Checksum  models.Checksum `json:"checksum"`
    PageCount int             `json:"page_count"`
    Text      string          `json:"text"`
}
```

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
type UpdateTextRequest struct {
    Name     string         `json:"name"`
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
   │                       │───────────────────────────▶│  valida firma %PDF- y tamaño    │                             │
   │                       │                            │  Extract(ctx, pdf)              │                             │
   │                       │                            │────────────────────────────────▶│                             │
   │                       │                            │   POST /extract (binario crudo) │                             │
   │                       │                            │◀────────────────────────────────│  text, page_count           │
   │                       │                            │  checksum = SHA-256(text)       │                             │
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
2. **Service** (`pdf_service.go`): valida la firma mágica (`%PDF-`) → 400 si falla. Delega en `ExtractClient.Extract(ctx, pdf)`.
3. **Client Extract** (`clients/extract`): hace `POST /extract` con body binario; parsea la respuesta a `dto.ExtractedDocument`; traduce errores HTTP/Problem Details a errores tipados de dominio (→ 502).
4. **Service**: calcula `checksum = SHA-256(text)` (el ID único del sistema).
5. **Service — auditoría**: crea `models.AuditEvent{Action: OpPDFExtract, Checksum, Details: {page_count}}` y lo pasa a un `auditwriter` interno que lo envía en **goroutine con `context.WithTimeout`** (fire-and-forget). La llamada original **no espera ni propaga errores** de auditoría.
6. **Service**: retorna `dto.ExtractPDFResponse{Checksum, Text, PageCount}` al handler.
7. **Handler**: serializa y responde `200` con JSON.

El mismo patrón se replica en Create/Update/Delete: `text_handler` → `TextService` (regla de inmutabilidad en Update) → `PersistenceClient` → emisión asíncrona de auditoría. El find (`GET /texts/{checksum}`) **omite** el paso 5 por regla de negocio.

---

## Task List (Fase 3 — backlog)

La Fase 3 (implementación) está descompuesta y registrada en `tasks/todo.md` con criterios de aceptación, verificación, dependencias y checkpoints. **No se ejecutará hasta que el humano apruebe este plan.**

---

## Riesgos y Mitigaciones

| Riesgo | Impacto | Mitigación |
|--------|---------|------------|
| MS Persistence no existe aún | Alto | El contrato `persistence.Client` definido aquí es la propuesta de integración; el orquestador negocia ese contrato con el equipo de Persistence antes de implementar. |
| Fire-and-forget pierde eventos si Audit Log falla | Medio | `context.WithTimeout` acotado + 1 reintento + log local del fallo. Opción futura: cola interna. Nunca degrada la respuesta al cliente. |
| PDFs enormes / malformados | Alto | Límite de tamaño en handler (413) + validación de firma `%PDF-` en service (400). El MS Extract ya limita a 15MB. |
| Checksum no único / colisiones | Medio | SHA-256 hex (64 chars). Política de colisión: la regla del sistema la resuelve Persistence (409 en Create); el orquestador solo lo genera. |
| Contratos de MS hermanos cambian | Medio | Clientes aislados en `clients/*`: un cambio externo solo altera un paquete + sus contract tests. |

## Preguntas Abiertas

1. **¿Este repo es el destino de implementación?** Este plan asume `ValidationMicroservices-pdf-extractext` (directorio actual). Existe un repo hermano `orchestrator/orchestrator-microservice-pdf-extractext` (Go, chi) con scaffolding básico. ¿Se implementa aquí, allá, o se descarta ese scaffolding?
2. **Nombre del módulo Go:** se propone `validationmicroservices-pdf-extractext` (Go exige minúsculas). ¿Confirmar? ¿Usar una ruta tipo `github.com/<org>/...`? (No definida hasta ahora).
3. **Create: `name` y `metadata`, ¿obligatorios u opcionales?** Se asume `name` opcional y `metadata` opcional.
4. **Delete: ¿`200` + JSON (asumido) o `204`?**
5. **¿El MS Extract devuelve páginas y el texto completo (`text`); el checksum se calcula sobre `text`?** Se asume sí (contiene el texto concatenado). Confirmar para no romper el ID único si se recalcula.
6. **Payload de auditoría `details`:** se asume `{page_count}` para extract y `{name}` para CRUD. Definir el contenido exacto si el MS Audit Log lo exige.