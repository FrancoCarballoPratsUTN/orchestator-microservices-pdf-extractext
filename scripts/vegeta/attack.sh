#!/usr/bin/env bash
# Reproduce el perfil de referencia de Vegeta contra el orquestador: 50 req/s
# durante 30 s rotando los cuatro PDF del corpus, con un timeout de 30 s. Misma
# forma que el scripts/vegeta/attack.sh del propio microservicio Extract, apuntado
# a la ruta de extract del orquestador.
#
#   ./scripts/vegeta/attack.sh
#
# Todo es sobreescribible:
#
#   TARGET=http://127.0.0.1:8099 RATE=100 DURATION=60s ./scripts/vegeta/attack.sh
#
# Vegeta no puede variar el cuerpo por request, así que el archivo de targets
# recibe una línea por PDF y Vegeta los rota en round-robin, que es la rotación
# que pide el perfil.
#
# Una nota sobre qué mide esto: el orquestador es un proxy de I/O delante de
# Extract, así que un perfil de latencia aquí principalmente vuelve a medir
# Extract. Por eso el script también reporta el desglose por estado, porque "¿falló
# alguna request?" es la señal que pertenece a este servicio. La corrección del
# checksum bajo carga está cubierta por el script de k6 (puede re-hashear); este
# script la re-verifica con una request simple por PDF al final.
set -euo pipefail

cd "$(dirname "$0")/../.."

# El orquestador y Extract usan ambos 8080 por defecto, así que el default aquí
# es 8099: un generador de carga apuntado al proceso equivocado mediría Extract
# por segunda vez y reportaría una ejecución en verde que no probó nada sobre este
# servicio.
#
# Para ejercitar el edge (Fase 11) en lugar de un orquestador local, apúntalo a
# Traefik y omite la sonda /healthz (el router público solo expone /api):
#
#   TARGET=https://pdf-extractext.localhost HEALTH_PATH= \
#     RATE=200 DURATION=10s ./scripts/vegeta/attack.sh
TARGET="${TARGET:-http://127.0.0.1:8099}"
# Ruta de la sonda de liveness. Vacío omite la sonda (usado al apuntar al
# edge, donde solo se publica /api).
HEALTH_PATH="${HEALTH_PATH-/healthz}"
# El corpus viene con este repositorio en tests/stress/pdfs (~13MB de cuatro PDF),
# resuelto relativo a la raíz del repo desde donde se ejecuta este script.
CORPUS="${CORPUS:-tests/stress/pdfs}"
RATE="${RATE:-50}"
DURATION="${DURATION:-30s}"
TIMEOUT="${TIMEOUT:-40s}"
WORKERS="${WORKERS:-50}"

if ! command -v vegeta >/dev/null 2>&1; then
  echo "vegeta no está instalado: https://github.com/tsenart/vegeta#installation" >&2
  exit 1
fi

DOCS=(
  "2020-Scrum-Guide-Spanish-Latin-South-American.pdf"
  "Filosofia Lean.pdf"
  "scrum_manager_historias_usuario.pdf"
  "Essential-Kanban-Condensed-Spanish.pdf"
)

for doc in "${DOCS[@]}"; do
  [[ -f "$CORPUS/$doc" ]] || { echo "falta el corpus: $CORPUS/$doc" >&2; exit 1; }
done

# Fallar ruidosamente en lugar de medir un orquestador que no está. Sin esto,
# connection refused aparece como una pared de códigos no-2xx y se confunde con
# un servicio bajo estrés. -k tolera el certificado de desarrollo (mkcert) al
# apuntar al edge por HTTPS.
if [[ -n "$HEALTH_PATH" ]] && ! curl -fsS -k -m 5 "$TARGET$HEALTH_PATH" >/dev/null 2>&1; then
  echo "el orquestador no responde en $TARGET$HEALTH_PATH" >&2
  echo "arrancalo con: PORT=8099 EXTRACT_BASE_URL=http://127.0.0.1:8080 \\" >&2
  echo "  PERSISTENCE_API_TOKEN=persistence-secret AUDIT_LOG_API_TOKEN=audit-secret go run ./cmd/orchestrator" >&2
  exit 1
fi

TARGETS=$(mktemp)
RESULTS=$(mktemp)
trap 'rm -f "$TARGETS" "$RESULTS"' EXIT

for doc in "${DOCS[@]}"; do
  printf '{"method":"POST","url":"%s/api/v1/pdfs/extract","header":{"Content-Type":["application/pdf"]},"body":"%s"}\n' \
    "$TARGET" "$(base64 -w0 "$CORPUS/$doc")" >>"$TARGETS"
done

INSECURE="${INSECURE:-true}"
TLS_FLAG=()
[[ "$INSECURE" == "true" ]] && TLS_FLAG=(-insecure)

echo "POST $TARGET/api/v1/pdfs/extract | $RATE req/s durante $DURATION | timeout $TIMEOUT | ${#DOCS[@]} PDFs rotando"
echo "insecure=$INSECURE  workers=$WORKERS"
echo

vegeta attack \
  -format=json \
  -rate="$RATE" -duration="$DURATION" -timeout="$TIMEOUT" \
  -max-workers="$WORKERS" "${TLS_FLAG[@]}" \
  -targets="$TARGETS" >"$RESULTS"

echo
vegeta report -type=text "$RESULTS"

# 200 es una extracción exitosa. Cualquier otra cosa es un fallo que vale la pena
# leer: 422 significa que el PDF perdió su capa de texto, 413 que se superó el
# límite de tamaño, 502 que Extract era inalcanzable, 504 que se disparó el
# timeout propio del orquestador.
echo
echo "desglose por código (200 = extracción correcta):"
vegeta report -type=json "$RESULTS" | tr ',' '\n' |
  grep -E '"(requests|rate|success|latencies)"|"[0-9]{3}":'

# Vegeta no puede re-hashear los cuerpos, así que la corrección del checksum se
# verifica aparte: un request por PDF contra el SHA-256 local de los bytes del
# archivo. El invariante del sistema es checksum == SHA-256(bytes del PDF).
echo
echo "verificando checksum == SHA-256(bytes del PDF):"
checksum_fail=0
for doc in "${DOCS[@]}"; do
  want=$(sha256sum "$CORPUS/$doc" | cut -d' ' -f1)
  got=$(curl -fsS -k -X POST -H 'Content-Type: application/pdf' --data-binary "@$CORPUS/$doc" "$TARGET/api/v1/pdfs/extract" |
    grep -o '"checksum":"[0-9a-f]*"' | head -1 | cut -d'"' -f4)
  if [[ "$got" == "$want" ]]; then
    echo "  ok   $doc  $got"
  else
    echo "  FAIL $doc  got=$got want=$want" >&2
    checksum_fail=1
  fi
done
[[ "$checksum_fail" -eq 0 ]] || exit 1
