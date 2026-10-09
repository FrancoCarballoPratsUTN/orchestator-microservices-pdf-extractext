# Stress y pruebas de carga

Dos capas, cada una contestando una pregunta distinta.

| Capa | Herramienta | Pregunta que responde |
|-------|-------------|-----------------------|
| Spike | k6 | ¿El servicio aguanta 100 VUs sin romper el checksum? |
| Ataque | vegeta | ¿Cuál es el techo real de requests por segundo? |

No hay capa de stress en Go: los invariantes bajo concurrencia se cubren con k6
y vegeta, que corren contra el stack real en lugar de mocks. Los tests unitarios
e de integración (`go test ./...`) usan los PDFs reales del corpus.

Ambas capas necesitan el Extract real levantado. Arrancálo primero (el del repo
`Conversor`, en `:8080`) y después el orquestador en otro puerto, porque los dos
usan 8080 por defecto:

```bash
# 1. Extract real, en :8080
cd ../Conversor/extract-markdown-microservice-pdf-extractext && docker compose up -d

# 2. Orquestador, en :8099
cd ../validator/ValidationMicroservices-pdf-extractext
PORT=8099 EXTRACT_BASE_URL=http://127.0.0.1:8080 \
  AUDIT_LOG_BASE_URL=http://127.0.0.1:8091 \
  PERSISTENCE_BASE_URL=http://127.0.0.1:8092 \
  go run ./cmd/orchestrator
```

`PORT=8099` no es opcional: sin él el orquestador intenta el 8080 que ya
ocupó el Extract.

## 1. Spike de k6

```bash
k6 run scripts/k6/spike.js          # 10s rampa a 100 VUs, 20s sostén, 10s baja
VUS=200 k6 run scripts/k6/spike.js   # más VUs
```

La diferencia con los load tests del Extract: k6 **re-hashea cada respuesta** y
la compara con el `checksum` que devolvió el servicio. Un generador de carga que
sólo cuenta códigos de estado daría 100% healthy a un servicio que devuelve
documentos con checksum incorrecto, que es justamente el fallo que importa acá.

También verifica que no queden `\r` ni espacios duros U+00A0 en el markdown.

> Ojo con `open()`: sólo existe en el init stage, así que los PDFs se leen a
> nivel global del script. Y k6 resuelve la ruta contra el directorio **del
> script**, no contra el de invocación, aunque el error diga lo contrario.

## 2. Ataque de vegeta

```bash
./scripts/vegeta/attack.sh
RATE=100 DURATION=60s ./scripts/vegeta/attack.sh
```

Perfil de referencia del enunciado: 50 req/s durante 30 s, rotando los cuatro
PDFs, timeout 40 s. Falla ruidosamente si el orquestador no está en
`/healthz`, porque un `connection refused` medido como una muralla de no-2xx se
confunde con un servicio bajo estrés.

## Resultados medidos

Stack: Extract real (5 réplicas vía compose) + orquestador en `:8099`, sin mocks
de Audit Log ni Persistence. Corpus completo (0.3MB a 8.9MB).

### Spike de k6 (100 VUs, 40 s)

```
extract_ok.....................: 100.00% 1343 out of 1343
checksum_matches...............: 100.00% 1343 out of 1343
markdown_artefact_free.........: 100.00% 1343 out of 1343
http_req_failed................:   0.00%    0 out of 1343
http_req_duration..............: avg=2.33s  p(95)=4.84s  max=5.56s
```

El invariante del sistema aguantó 1343 requests con 100 VUs concurrentes sin una
sola desviación.

### vegeta (30 s)

```
Requests  [total, rate]  971, 32.29/s
Success   [ratio]        100.00%
Status Codes              200:971
Latencies                 p95=2.642s  max=3.377s
```

### Techo de capacidad

Pedidos 50 req/s, entregados **32.29/s**. Pedidos 70 req/s se entregaron
**34.54/s**. El techo está en **~33 req/s** para este corpus.

Lo relevante es **cómo** degrada: con 100% de éxito, cero errores y sin timeouts.
La latencia sube (p95 de 2.6 s) y el resto espera en cola. No hay colapso.

El cuello de botella es el egreso, no el orquestador: 3.3 GB en 30 s, unos
111 MB/s de entrada. Son PDFs de 3.3 MB de promediodevueltos como markdown, así
que cada request mueve ~3.6 MB. Con un corpus de documentos más chicos el techo
sería mucho mayor, y hace falta medirlo antes de afirmar un número de capacidad
en serio.

**Lo que este perfil NO mide:** el Extract es CPU-bound y ya tiene sus propias
pruebas de carga. Acá sólo se demuestra que el orquestador no agrega degradación
propia y no rompe el checksum.

## Corpus

Los PDFs viven en `tests/stress/pdfs/` (unos 13 MB): Scrum Guide de 16
páginas, Essential Kanban de 90, Filosofía Lean de 42 y scrum_manager de 62.
Están versionados en este repo; `CORPUS` los sobreescribe en los dos scripts.

El caso de PDF sin capa de texto (422) lo cubre el test de integración
`TestIntegrationScannedPDFReturns422AndWritesNoAudit`, que usa un PDF real del
corpus con una respuesta vacía del Extract.

El caso de PDF cifrado (422) lo cubre `TestIntegrationEncryptedPDFReturns422AndWritesNoAudit`
y `make smoke`. El PDF cifrado se genera al vuelo con `scripts/encryptpdf`, que
envuelve `internal/pdfencrypt` (una sola implementación para tests y CLI); no se
versiona ningún binario cifrado.

## Qué se encontró con esto

1. **Espacios duros internos en el markdown.** El spike de k6 marcó 75% de
   respuestas con artefactos, es decir exactamente 1 de cada 4 documentos:
   `Essential Kanban` traía seis U+00A0 dentro de las líneas, en cosas como
   `(ver Fig 14)`. `TrimSpace` limpiaba los bordes de cada renglón pero
   `normalizeSpaces` sólo comparaba con `' '` y `'\t'`. Ahora usa
   `unicode.IsSpace`. Lo encontró la métrica del spike, no un test unitario.
2. **404 y 405 no eran problem documents.** Todos los errores que produce la API
   son RFC 9457, pero chi devolvía texto plano o cuerpo vacío para ruta
   inexistente o método incorrecto, así que un cliente tenía que parsear dos
   formatos para el mismo tipo de error. Ahora `Routes` define `NotFound` y
   `MethodNotAllowed`.