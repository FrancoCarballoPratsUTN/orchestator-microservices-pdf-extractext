// Perfil de pico (spike) para el orquestador, espejo de scripts/vegeta/attack.sh
// y del perfil de referencia del microservicio Extract: rampa de 10s a 100 VUs,
// meseta de 20s y bajada de 10s.
//
//   k6 run scripts/k6/spike.js
//
// TARGET, CORPUS y VUS son sobreescribibles, por ejemplo:
//
//   TARGET=http://127.0.0.1:8099 CORPUS=/abs/path/pdfs k6 run scripts/k6/spike.js
//
// Qué lo diferencia de las pruebas de carga de Extract: k6 recalcula el
// SHA-256 de los bytes del PDF que envió y lo compara con el checksum que
// devolvió el servicio. Un generador de carga que solo contara códigos de estado
// calificaría a este servicio como 100% sano aunque devolviera documentos
// indexados con un checksum erróneo, que es el único fallo que realmente
// importa aquí.
//
// k6 resuelve open() respecto al directorio que contiene este script, es decir,
// scripts/k6, no respecto al directorio desde el que se ejecutó el comando. El
// corpus viene con este repositorio en tests/stress/pdfs, dos niveles arriba de
// scripts/k6. Determinado ejecutando k6, no supuesto. Pasa CORPUS=/ruta/absoluta
// para sobreescribirlo.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Rate } from 'k6/metrics';
import { sha256 } from 'k6/crypto';

const TARGET = __ENV.TARGET || 'http://127.0.0.1:8099';
const CORPUS = __ENV.CORPUS || '../../tests/stress/pdfs';
const VUS = parseInt(__ENV.VUS || '100', 10);

const EXTRACT_PATH = '/api/v1/pdfs/extract';

// Los mismos cuatro documentos que usan las pruebas de carga de Extract,
// ordenados de menor a mayor para que una rampa ejercite payloads pequeños antes
// que el de 8.9MB.
const DOCS = [
  '2020-Scrum-Guide-Spanish-Latin-South-American.pdf',
  'Filosofia Lean.pdf',
  'scrum_manager_historias_usuario.pdf',
  'Essential-Kanban-Condensed-Spanish.pdf',
];

// open() solo existe durante la etapa de init, así que los fixtures se leen aquí
// en el ámbito global y se conservan durante toda la ejecución. Llamar a open()
// dentro de la función default lanza "open is only available in the init stage".
//
// El segundo argumento 'b' devuelve un ArrayBuffer en lugar de un string: los
// PDF son binarios, y un cuerpo string se recodificaría en UTF-8, hasheando y
// enviando bytes que ya no coinciden con el archivo. El checksum es sobre los
// bytes exactos.
//
// Mantener el documento de 8.9MB en cada VU es la razón por la que el número de
// VUs por defecto es 100 y no algo mayor.
const pdfs = DOCS.map((doc) => open(`${CORPUS}/${doc}`, 'b'));

// Métricas explícitas en lugar de resultados de check() sin más.
//
// k6 valida los thresholds antes de que se ejecute ninguna iteración, y un
// threshold sobre una métrica derivada de check falla allí con 'no metric name
// found' porque esas métricas aún no existen. Declararlas de antemano es lo que
// hace que los thresholds sean aplicables.
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
    // El invariante del sistema, verificado por request.
    checksum_matches: ['rate==1.0'],
    extract_ok: ['rate==1.0'],
    text_artefact_free: ['rate==1.0'],
    // El orquestador es un proxy de I/O delante de Extract, así que la latencia
    // aquí está dominada por Extract. Estos límites son laxos a propósito:
    // detectan una regresión de un orden de magnitud (un bug de serialización,
    // un pool de conexiones agotado), no la varianza normal.
    'http_req_duration{expected_response:true}': ['p(95)<10000'],
    http_req_failed: ['rate<0.01'],
  },
  // Los cuerpos deben conservarse: el objetivo es re-hashear cada respuesta.
  discardResponseBodies: false,
};

export default function () {
  // Rotar el corpus de la misma forma que el script de vegeta.
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

  // Un CR filtrado o un espacio de no separación significan que artefactos de
  // pdf_oxide llegaron al texto que se le indica al cliente persistir. El NBSP se
  // escribe como escape porque un U+00A0 literal en el código es invisible y se
  // pierde trivialmente con un reformateo.
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
