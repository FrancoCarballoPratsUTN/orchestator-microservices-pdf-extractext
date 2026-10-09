package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/checksum"
	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/pdfencrypt"
	"validationmicroservices-pdf-extractext/internal/services"
	"validationmicroservices-pdf-extractext/internal/testsupport"
)

const integrationTimeout = 3 * time.Second

// integrationMaxPages replica el default de MAX_PDF_PAGES en el stack de tests.
// Los PDFs del corpus tienen menos páginas, así que no dispara.
const integrationMaxPages = 1000

// integrationExtractContent simula lo que devuelve el Extract para un PDF real:
// texto plano con cortes de línea de pdf_oxide, un título en mayúsculas y tres
// páginas unidas con "\n\n", con el encabezado repetido en cada una (ver el
// extractor del servicio Extract).
//
// El orquestador devuelve este contenido tal cual: el Extract ya lo entrega
// formateado y no hay conversión a markdown. Por eso el encabezado repetido se
// conserva en la respuesta.
const integrationExtractContent = "INFORME DE PRUEBA\n" +
	"El orquestador devuelve el texto del Extract tal cual,\nsin conversión intermedia.\n\n" +
	"INFORME DE PRUEBA\nLa segunda pagina repite el encabezado.\n\n" +
	"INFORME DE PRUEBA\nCierre del informe."

const integrationPersistenceToken = "persistence-secret"

type textStore struct {
	mu    sync.Mutex
	texts map[models.Checksum]models.Text
}

func newTextStore() *textStore {
	return &textStore{texts: make(map[models.Checksum]models.Text)}
}

func (s *textStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+integrationPersistenceToken {
		writeProblem(w, http.StatusUnauthorized, "missing or invalid persistence token")
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/texts":
		s.create(w, r)
	case r.Method == http.MethodGet:
		s.get(w, r)
	case r.Method == http.MethodPut:
		s.update(w, r)
	case r.Method == http.MethodDelete:
		s.delete(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *textStore) create(w http.ResponseWriter, r *http.Request) {
	var payload dto.CreateTextPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid create payload")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.texts[payload.Checksum]; exists {
		writeProblem(w, http.StatusConflict, "checksum already exists")
		return
	}
	now := time.Now().UTC()
	s.texts[payload.Checksum] = models.Text{
		Checksum:  payload.Checksum,
		Text:      payload.Text,
		Name:      payload.Name,
		Metadata:  payload.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *textStore) get(w http.ResponseWriter, r *http.Request) {
	checksum := checksumFromQuery(r)
	if checksum == "" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text, exists := s.texts[checksum]
	if !exists {
		writeProblem(w, http.StatusNotFound, "checksum not found")
		return
	}
	_ = json.NewEncoder(w).Encode(text)
}

func (s *textStore) update(w http.ResponseWriter, r *http.Request) {
	checksum := checksumFromQuery(r)
	var payload dto.UpdateTextPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid update payload")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text, exists := s.texts[checksum]
	if !exists {
		writeProblem(w, http.StatusNotFound, "checksum not found")
		return
	}
	if payload.Name != nil {
		text.Name = *payload.Name
	}
	if payload.Metadata != nil {
		text.Metadata = payload.Metadata
	}
	text.UpdatedAt = time.Now().UTC()
	s.texts[checksum] = text
	_ = json.NewEncoder(w).Encode(text)
}

func (s *textStore) delete(w http.ResponseWriter, r *http.Request) {
	checksum := checksumFromQuery(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.texts[checksum]; !exists {
		writeProblem(w, http.StatusNotFound, "checksum not found")
		return
	}
	delete(s.texts, checksum)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "OK"})
}

func checksumFromQuery(r *http.Request) models.Checksum {
	return models.Checksum(r.URL.Query().Get("checksum"))
}

func ptr(s string) *string {
	return &s
}

type auditStore struct {
	mu     sync.Mutex
	events []models.AuditLog
}

func (s *auditStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/audit/logs":
		s.emit(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/audit/logs":
		s.listAll(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/audit/logs/checksum/"):
		s.listByChecksum(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *auditStore) emit(w http.ResponseWriter, r *http.Request) {
	var event models.AuditEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid audit event")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log := models.AuditLog{
		ID:          fmt.Sprintf("log-%d", len(s.events)+1),
		Action:      string(event.Action),
		EntityType:  event.EntityType,
		Checksum:    event.Checksum,
		Details:     event.Details,
		PerformedAt: event.PerformedAt,
	}
	s.events = append(s.events, log)
	w.WriteHeader(http.StatusCreated)
}

func (s *auditStore) listAll(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(s.events)
}

func (s *auditStore) listByChecksum(w http.ResponseWriter, r *http.Request) {
	checksum := models.Checksum(strings.TrimPrefix(r.URL.Path, "/audit/logs/checksum/"))
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]models.AuditLog, 0, len(s.events))
	for _, log := range s.events {
		if log.Checksum == checksum {
			filtered = append(filtered, log)
		}
	}
	_ = json.NewEncoder(w).Encode(filtered)
}

func (s *auditStore) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	actions := make([]string, 0, len(s.events))
	for _, log := range s.events {
		actions = append(actions, log.Action)
	}
	return actions
}

type extractMock struct {
	// content permite que cada test controle qué devuelve el Extract. El default
	// simula un PDF con capa de texto.
	content string
	// calls cuenta las invocaciones para poder afirmar que la validación cortó el
	// request antes de llegar al Extract.
	calls int32
}

// newExtractMock devuelve un mock de Extract que responde la forma real del
// servicio ({content, page_count}).
func newExtractMock(content string) *extractMock {
	return &extractMock{content: content}
}

func (m *extractMock) callCount() int {
	return int(atomic.LoadInt32(&m.calls))
}

func (m *extractMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/extract" {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	atomic.AddInt32(&m.calls, 1)
	body := make([]byte, 0, 1<<20)
	buf := make([]byte, 64)
	for {
		n, err := r.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	if !strings.HasPrefix(string(body), "%PDF-") {
		writeProblem(w, http.StatusBadRequest, "invalid pdf payload")
		return
	}
	_ = json.NewEncoder(w).Encode(dto.ExtractedDocument{
		PageCount: 3,
		Content:   m.content,
	})
}

type mockServers struct {
	extract     *httptest.Server
	extractMock *extractMock
	persist     *httptest.Server
	audit       *httptest.Server
	auditStore  *auditStore
	textStore   *textStore
	router      http.Handler
}

// newIntegrationStack arma el stack completo con el contenido de Extract por
// defecto.
func newIntegrationStack(t *testing.T) *mockServers {
	t.Helper()

	return newIntegrationStackWith(t, integrationExtractContent, integrationMaxPages)
}

// newIntegrationStackWithExtractContent arma el stack completo con un contenido de
// Extract explícito, para poder simular un PDF sin capa de texto.
//
// El contenido viaja como argumento y no como variable global: los tests corren en
// paralelo y una variable compartida haría que uno pise el mock de otro.
func newIntegrationStackWithExtractContent(t *testing.T, extractContent string) *mockServers {
	t.Helper()

	return newIntegrationStackWith(t, extractContent, integrationMaxPages)
}

// newIntegrationStackWith es el armado real: los wrappers de arriba fijan el
// contenido y el límite de páginas para que cada test se lea sin ruido.
func newIntegrationStackWith(t *testing.T, extractContent string, maxPages int) *mockServers {
	t.Helper()

	textStore := newTextStore()
	auditStore := &auditStore{}
	extractMock := newExtractMock(extractContent)
	extractServer := httptest.NewServer(extractMock)
	persistServer := httptest.NewServer(textStore)
	auditServer := httptest.NewServer(auditStore)
	t.Cleanup(func() {
		extractServer.Close()
		persistServer.Close()
		auditServer.Close()
	})

	logger := discardLogger()
	httpTimeout := 5 * time.Second
	auditClient := auditlog.NewClient(auditServer.URL, httpTimeout, "")
	auditService := services.NewAuditService(auditClient, logger, httpTimeout)
	persistenceClient := persistence.NewClient(persistServer.URL, httpTimeout, integrationPersistenceToken)
	pdfService := services.NewPDFService(extract.NewClient(extractServer.URL, httpTimeout), persistenceClient, auditService, logger, maxPages)
	textService := services.NewTextService(persistenceClient, auditService)

	router := Routes(
		config.Config{Port: "8080"},
		logger,
		handlers.NewPDFHandler(pdfService, testMaxPDFSize),
		handlers.NewAuditHandler(auditService),
		handlers.NewTextHandler(textService, 4*1024*1024),
	)

	return &mockServers{
		extract:     extractServer,
		extractMock: extractMock,
		persist:     persistServer,
		audit:       auditServer,
		auditStore:  auditStore,
		textStore:   textStore,
		router:      router,
	}
}

func serve(router http.Handler, method, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(httpclient.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}

func waitForAuditEvents(t *testing.T, store *auditStore, events []string) {
	t.Helper()
	deadline := time.Now().Add(integrationTimeout)
	for time.Now().Before(deadline) {
		actions := store.actions()
		if len(actions) >= len(events) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("audit events not captured within %v: got %v, want %v", integrationTimeout, store.actions(), events)
}

// TestIntegrationScannedPDFReturns422AndWritesNoAudit comprueba el flujo completo
// del PDF escaneado: 422, y ningún evento de auditoría. Si se escribiera, el log
// afirmaría una extracción que no ocurrió.
func TestIntegrationScannedPDFReturns422AndWritesNoAudit(t *testing.T) {
	t.Parallel()

	stack := newIntegrationStackWithExtractContent(t, "")

	response := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", string(testsupport.ScrumGuidePDF(t)), map[string]string{"Content-Type": "application/pdf"})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("extract status = %d, want %d (body: %s)", response.Code, http.StatusUnprocessableEntity, response.Body)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if actions := stack.auditStore.actions(); len(actions) > 0 {
			t.Fatalf("audit actions = %v, want none for a rejected PDF", actions)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestIntegrationEncryptedPDFReturns422AndWritesNoAudit comprueba el flujo
// completo de un PDF con contraseña: la validación lo clasifica antes de llamar
// al Extract, responde 422 y no audita una extracción que nunca ocurrió.
func TestIntegrationEncryptedPDFReturns422AndWritesNoAudit(t *testing.T) {
	t.Parallel()

	stack := newIntegrationStack(t)

	encrypted, err := pdfencrypt.Encrypt(testsupport.ScrumGuidePDF(t), pdfencrypt.DefaultPassword)
	if err != nil {
		t.Fatalf("pdfencrypt.Encrypt: %v", err)
	}

	response := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", string(encrypted), map[string]string{"Content-Type": "application/pdf"})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("extract status = %d, want %d (body: %s)", response.Code, http.StatusUnprocessableEntity, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	if calls := stack.extractMock.callCount(); calls != 0 {
		t.Errorf("extract calls = %d, want 0 (an encrypted PDF must be rejected before Extract)", calls)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if actions := stack.auditStore.actions(); len(actions) > 0 {
			t.Fatalf("audit actions = %v, want none for a rejected PDF", actions)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestIntegrationPDFAbovePageLimitReturns422 comprueba que MAX_PDF_PAGES corta el
// request antes de llamar al Extract y que el handler traduce el rechazo a 422 con
// Problem Details RFC 9457.
func TestIntegrationPDFAbovePageLimitReturns422(t *testing.T) {
	t.Parallel()

	// El Scrum Guide tiene 16 páginas; con el límite en 1 el gate se dispara.
	stack := newIntegrationStackWith(t, integrationExtractContent, 1)

	response := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", string(testsupport.ScrumGuidePDF(t)), map[string]string{"Content-Type": "application/pdf"})

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("extract status = %d, want %d (body: %s)", response.Code, http.StatusUnprocessableEntity, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	if calls := stack.extractMock.callCount(); calls != 0 {
		t.Errorf("extract calls = %d, want 0 (the page limit must cut before Extract)", calls)
	}
}

// TestIntegrationDedupHitSkipsExtractAndAudit comprueba el ciclo de dedup de punta
// a punta: el primer request hace miss y llama al Extract; una vez almacenado el
// texto bajo el checksum del PDF, un segundo request idéntico es un HIT y devuelve
// la misma respuesta sin volver a llamar al Extract ni auditar de nuevo.
func TestIntegrationDedupHitSkipsExtractAndAudit(t *testing.T) {
	t.Parallel()

	stack := newIntegrationStack(t)
	pdf := testsupport.ScrumGuidePDF(t)

	miss := extractPDF(t, stack, pdf)
	if calls := stack.extractMock.callCount(); calls != 1 {
		t.Fatalf("extract calls after miss = %d, want 1", calls)
	}

	storeExtractedText(t, stack, miss)

	// La auditoría del miss es asíncrona: hay que esperarla antes de tomar la línea
	// base, o el HIT mediría un evento que todavía viaja.
	waitForAuditAction(t, stack.auditStore, string(models.OpPDFExtract))
	baseline := countAuditAction(stack.auditStore, string(models.OpPDFExtract))

	hit := extractPDF(t, stack, pdf)
	if hit != miss {
		t.Errorf("hit response = %+v, want identical to miss %+v", hit, miss)
	}
	if calls := stack.extractMock.callCount(); calls != 1 {
		t.Errorf("extract calls after hit = %d, want 1 (a hit must not call Extract)", calls)
	}
	if got := countAuditAction(stack.auditStore, string(models.OpPDFExtract)); got != baseline {
		t.Errorf("pdf.extract audit events after hit = %d, want %d (a hit must not audit)", got, baseline)
	}
}

// extractPDF hace el POST de extracción y devuelve la respuesta tipada. Falla el
// test si el servicio no responde 200.
func extractPDF(t *testing.T, stack *mockServers, pdf []byte) dto.ExtractPDFResponse {
	t.Helper()

	response := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", string(pdf), map[string]string{"Content-Type": "application/pdf"})
	if response.Code != http.StatusOK {
		t.Fatalf("extract status = %d, want %d (body: %s)", response.Code, http.StatusOK, response.Body)
	}
	var extracted dto.ExtractPDFResponse
	if err := json.Unmarshal(response.Body.Bytes(), &extracted); err != nil {
		t.Fatalf("extract response is not valid JSON: %v", err)
	}
	return extracted
}

// storeExtractedText persiste el texto extraído bajo su checksum para que la
// siguiente extracción pueda pegarle al cache.
func storeExtractedText(t *testing.T, stack *mockServers, extracted dto.ExtractPDFResponse) {
	t.Helper()

	body, err := json.Marshal(dto.CreateTextRequest{Text: extracted.Text, Checksum: extracted.Checksum, Name: "cache"})
	if err != nil {
		t.Fatalf("marshaling create body: %v", err)
	}
	response := serve(stack.router, http.MethodPost, "/api/v1/texts", string(body), map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (body: %s)", response.Code, http.StatusCreated, response.Body)
	}
}

func waitForAuditAction(t *testing.T, store *auditStore, action string) {
	t.Helper()

	deadline := time.Now().Add(integrationTimeout)
	for time.Now().Before(deadline) {
		if countAuditAction(store, action) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("audit action %q not captured within %v: got %v", action, integrationTimeout, store.actions())
}

func countAuditAction(store *auditStore, action string) int {
	count := 0
	for _, got := range store.actions() {
		if got == action {
			count++
		}
	}
	return count
}

func TestIntegrationExtractPersistAuditFlow(t *testing.T) {
	t.Parallel()

	stack := newIntegrationStack(t)

	pdfBytes := testsupport.ScrumGuidePDF(t)
	extractResponse := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", string(pdfBytes), map[string]string{"Content-Type": "application/pdf"})
	if extractResponse.Code != http.StatusOK {
		t.Fatalf("extract status = %d, want %d", extractResponse.Code, http.StatusOK)
	}
	var extracted dto.ExtractPDFResponse
	if err := json.Unmarshal(extractResponse.Body.Bytes(), &extracted); err != nil {
		t.Fatalf("extract response is not valid JSON: %v", err)
	}
	// El orquestador devuelve el contenido del Extract tal cual: sin conversión a
	// markdown, los saltos de línea y los encabezados repetidos se conservan.
	wantText := integrationExtractContent
	if extracted.Text != wantText {
		t.Fatalf("extract text = %q, want %q", extracted.Text, wantText)
	}

	// El checksum ahora identifica los bytes del PDF (clave de dedup), no el texto.
	wantChecksum := models.Checksum(checksum.OfBytes(pdfBytes))
	if extracted.Checksum != wantChecksum {
		t.Fatalf("extract checksum = %q, want SHA-256 of the PDF bytes %q", extracted.Checksum, wantChecksum)
	}

	// El texto va marshaleado, no interpolado con Sprintf: el contenido contiene
	// saltos de línea y comillas, y una interpolación rompería el JSON.
	createBody, err := json.Marshal(dto.CreateTextRequest{
		Text:     extracted.Text,
		Checksum: extracted.Checksum,
		Name:     "documento integracion",
	})
	if err != nil {
		t.Fatalf("marshaling create body: %v", err)
	}
	createResponse := serve(stack.router, http.MethodPost, "/api/v1/texts", string(createBody), map[string]string{"Content-Type": "application/json"})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (body: %s)", createResponse.Code, http.StatusCreated, createResponse.Body)
	}

	checksum := string(extracted.Checksum)

	findResponse := serve(stack.router, http.MethodGet, "/api/v1/texts/"+checksum, "", nil)
	if findResponse.Code != http.StatusOK {
		t.Fatalf("find status = %d, want %d", findResponse.Code, http.StatusOK)
	}
	var persisted models.Text
	if err := json.Unmarshal(findResponse.Body.Bytes(), &persisted); err != nil {
		t.Fatalf("find response is not valid JSON: %v", err)
	}
	if persisted.Name != "documento integracion" {
		t.Errorf("Text.Name = %q, want %q", persisted.Name, "documento integracion")
	}

	updateResponse := serve(stack.router, http.MethodPut, "/api/v1/texts/"+checksum, `{"name":"renombrado"}`, map[string]string{"Content-Type": "application/json"})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d (body: %s)", updateResponse.Code, http.StatusOK, updateResponse.Body)
	}
	var updated models.Text
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("update response is not valid JSON: %v", err)
	}
	if updated.Name != "renombrado" {
		t.Errorf("Text.Name = %q, want %q", updated.Name, "renombrado")
	}

	deleteResponse := serve(stack.router, http.MethodDelete, "/api/v1/texts/"+checksum, "", nil)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d (body: %s)", deleteResponse.Code, http.StatusOK, deleteResponse.Body)
	}

	afterDelete := serve(stack.router, http.MethodGet, "/api/v1/texts/"+checksum, "", nil)
	if afterDelete.Code != http.StatusNotFound {
		t.Fatalf("find after delete status = %d, want %d", afterDelete.Code, http.StatusNotFound)
	}

	waitForAuditEvents(t, stack.auditStore, []string{"pdf.extract", "text.create", "text.update", "text.delete"})
	actions := stack.auditStore.actions()
	for _, want := range []string{"pdf.extract", "text.create", "text.update", "text.delete"} {
		if !contains(actions, want) {
			t.Errorf("audit events %v do not contain %q", actions, want)
		}
	}

	auditResponse := serve(stack.router, http.MethodGet, "/api/v1/audit/logs", "", nil)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want %d", auditResponse.Code, http.StatusOK)
	}
	var logs dto.AuditLogsResponse
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &logs); err != nil {
		t.Fatalf("audit response is not valid JSON: %v", err)
	}
	if len(logs.Logs) != 4 {
		t.Errorf("audit logs count = %d, want %d", len(logs.Logs), 4)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
