#!/usr/bin/env bash
# Reproduce the reference Vegeta profile against the orchestrator: 50 req/s for
# 30 s rotating the four PDFs of the corpus, with a 30 s timeout. Same shape as
# the Extract microservice's own scripts/vegeta/attack.sh, pointed at the
# orchestrator's extract route instead.
#
#   ./scripts/vegeta/attack.sh
#
# Everything is overridable:
#
#   TARGET=http://127.0.0.1:8099 RATE=100 DURATION=60s ./scripts/vegeta/attack.sh
#
# Vegeta cannot vary the body per request, so the targets file gets one line per
# PDF and Vegeta round-robins them, which is the rotation the profile asks for.
#
# A note on what this measures: the orchestrator is an I/O proxy in front of
# Extract, so a latency profile here mostly re-measures Extract. The script
# therefore also reports the status breakdown, because "did any request fail" is
# the signal that belongs to this service. Checksum correctness under load is
# covered by the k6 script (it can re-hash); this script re-verifies it with a
# plain request per PDF at the end.
set -euo pipefail

cd "$(dirname "$0")/../.."

# The orchestrator and Extract both default to 8080, so the default here is 8099:
# a load generator pointed at the wrong process would otherwise measure Extract a
# second time and report a green run that proved nothing about this service.
TARGET="${TARGET:-http://127.0.0.1:8099}"
# The corpus ships with this repo under tests/stress/pdfs (~13MB of four PDFs),
# resolved relative to the repo root this script is run from.
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

# Fail loudly instead of measuring an orchestrator that is not there. Without
# this, connection refused shows up as a wall of non-2xx codes and gets mistaken
# for a service under stress.
if ! curl -fsS -m 5 "$TARGET/healthz" >/dev/null 2>&1; then
  echo "el orquestador no responde en $TARGET/healthz" >&2
  echo "arrancalo con: PORT=8099 EXTRACT_BASE_URL=http://127.0.0.1:8080 go run ./cmd/orchestrator" >&2
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

# 200 is a successful extraction. Anything else is a failure worth reading: 422
# means the PDF lost its text layer, 413 the size limit tripped, 502 the Extract
# was unreachable, 504 the orchestrator's own timeout fired.
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
  got=$(curl -fsS -X POST -H 'Content-Type: application/pdf' --data-binary "@$CORPUS/$doc" "$TARGET/api/v1/pdfs/extract" |
    grep -o '"checksum":"[0-9a-f]*"' | head -1 | cut -d'"' -f4)
  if [[ "$got" == "$want" ]]; then
    echo "  ok   $doc  $got"
  else
    echo "  FAIL $doc  got=$got want=$want" >&2
    checksum_fail=1
  fi
done
[[ "$checksum_fail" -eq 0 ]] || exit 1