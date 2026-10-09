// Spike profile for the orchestrator, mirroring scripts/vegeta/attack.sh and the
// reference profile of the Extract microservice: ramp 10s to 100 VUs, hold 20s,
// ramp down 10s.
//
//   k6 run scripts/k6/spike.js
//
// TARGET, CORPUS and VUS are overridable, e.g.
//
//   TARGET=http://127.0.0.1:8099 CORPUS=/abs/path/pdfs k6 run scripts/k6/spike.js
//
// What makes this different from the Extract load tests: k6 recomputes the
// SHA-256 of the PDF bytes it sent and compares it against the checksum the
// service returned. A load generator that only counted status codes would rate
// this service 100% healthy while it returned documents keyed by a wrong
// checksum, which is the one failure mode that actually matters here.
//
// k6 resolves open() against the directory holding this script, i.e. scripts/k6,
// not against the directory the command was run from. The corpus ships with this
// repo under tests/stress/pdfs, two levels up from scripts/k6. Determined by
// running k6, not assumed. Pass CORPUS=/absolute/path to override.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Rate } from 'k6/metrics';
import { sha256 } from 'k6/crypto';

const TARGET = __ENV.TARGET || 'http://127.0.0.1:8099';
const CORPUS = __ENV.CORPUS || '../../tests/stress/pdfs';
const VUS = parseInt(__ENV.VUS || '100', 10);

const EXTRACT_PATH = '/api/v1/pdfs/extract';

// The same four documents the Extract load tests use, ordered smallest to
// largest so a ramp exercises small payloads before the 8.9MB one.
const DOCS = [
  '2020-Scrum-Guide-Spanish-Latin-South-American.pdf',
  'Filosofia Lean.pdf',
  'scrum_manager_historias_usuario.pdf',
  'Essential-Kanban-Condensed-Spanish.pdf',
];

// open() only exists during the init stage, so the fixtures are read here at
// global scope and kept for the whole run. Calling open() inside the default
// function throws "open is only available in the init stage".
//
// The second argument 'b' returns an ArrayBuffer instead of a string: the PDFs
// are binary, and a string body would be UTF-8 re-encoded, hashing and sending
// bytes that no longer match the file. The checksum is over the exact bytes.
//
// Holding the 8.9MB document in every VU is why the default VU count is 100
// rather than something larger.
const pdfs = DOCS.map((doc) => open(`${CORPUS}/${doc}`, 'b'));

// Explicit metrics rather than bare check() results.
//
// k6 validates thresholds before any iteration runs, and a threshold over a
// check-derived metric fails there with 'no metric name found' because those
// metrics do not exist yet. Declaring them up front is what makes the
// thresholds enforceable at all.
const extractOK = new Rate('extract_ok');
const checksumMatches = new Rate('checksum_matches');
const artefactFree = new Rate('text_artefact_free');

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: VUS },
        { duration: '20s', target: VUS },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    // The invariant of the system, asserted per request.
    checksum_matches: ['rate==1.0'],
    extract_ok: ['rate==1.0'],
    text_artefact_free: ['rate==1.0'],
    // The orchestrator is an I/O proxy in front of Extract, so latency here is
    // dominated by Extract. These bounds are loose on purpose: they catch an
    // order-of-magnitude regression (a serialization bug, an exhausted connection
    // pool), not ordinary variance.
    'http_req_duration{expected_response:true}': ['p(95)<10000'],
    http_req_failed: ['rate<0.01'],
  },
  // The bodies have to be kept: the whole point is re-hashing each response.
  discardResponseBodies: false,
};

export default function () {
  // Rotate the corpus the same way the vegeta script does.
  const index = exec.scenario.iterationInTest % DOCS.length;
  const body = pdfs[index];

  const res = http.post(`${TARGET}${EXTRACT_PATH}`, body, {
    headers: { 'Content-Type': 'application/pdf' },
    tags: { name: EXTRACT_PATH, doc: DOCS[index] },
    timeout: '40s',
  });

  const ok = check(res, { 'extract responds 200': (r) => r.status === 200 });
  if (!ok) {
    extractOK.add(0);
    console.error(`extract failed: status=${res.status} body=${String(res.body).slice(0, 200)}`);
    return;
  }
  extractOK.add(1);

  const text = res.json('text');
  const declared = res.json('checksum');

  // El checksum identifica los bytes del PDF (clave de dedup), no el texto: se
  // re-hashea el mismo ArrayBuffer que se envió, byte por byte.
  const matches = sha256(body, 'hex') === declared;
  checksumMatches.add(matches ? 1 : 0);

  // A leaked CR or a non-breaking space means pdf_oxide artefacts reached the
  // text the client is told to persist. The NBSP is written as an escape
  // because a literal U+00A0 in the source is invisible and trivially lost to a
  // reformat.
  const clean = text.indexOf('\r') === -1 && text.indexOf('\u00a0') === -1;
  artefactFree.add(clean ? 1 : 0);

  check(
    { text, declared },
    {
      'text is not empty': () => text.length > 0,
      'checksum matches': () => matches,
      'no pdf_oxide artefacts': () => clean,
    },
  );
}