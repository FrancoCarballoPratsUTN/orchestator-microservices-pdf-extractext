package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/clients/auditlog"
	"validationmicroservices-pdf-extractext/internal/clients/extract"
	"validationmicroservices-pdf-extractext/internal/clients/persistence"
	"validationmicroservices-pdf-extractext/internal/config"
	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/handlers"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/services"
)

const integrationTimeout = 3 * time.Second

type textStore struct {
	mu    sync.Mutex
	texts map[models.Checksum]models.Text
}

func newTextStore() *textStore {
	return &textStore{texts: make(map[models.Checksum]models.Text)}
}

func (s *textStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	checksum := models.Checksum(strings.TrimPrefix(r.URL.Path, "/texts/"))
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
	checksum := models.Checksum(strings.TrimPrefix(r.URL.Path, "/texts/"))
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
	text.Name = payload.Name
	text.Metadata = payload.Metadata
	text.UpdatedAt = time.Now().UTC()
	s.texts[checksum] = text
	_ = json.NewEncoder(w).Encode(text)
}

func (s *textStore) delete(w http.ResponseWriter, r *http.Request) {
	checksum := models.Checksum(strings.TrimPrefix(r.URL.Path, "/texts/"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.texts[checksum]; !exists {
		writeProblem(w, http.StatusNotFound, "checksum not found")
		return
	}
	delete(s.texts, checksum)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "OK"})
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

type extractMock struct{}

func (extractMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/extract" {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
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
		PageCount:  3,
		Pages:      []dto.ExtractedPage{{PageNumber: 1, Text: "a"}, {PageNumber: 2, Text: "b"}, {PageNumber: 3, Text: "c"}},
		Text:       "texto integrado del pdf",
		DurationMs: 7,
	})
}

type mockServers struct {
	extract    *httptest.Server
	persist    *httptest.Server
	audit      *httptest.Server
	auditStore *auditStore
	textStore  *textStore
	router     http.Handler
}

func newIntegrationStack(t *testing.T) *mockServers {
	t.Helper()

	textStore := newTextStore()
	auditStore := &auditStore{}
	extractServer := httptest.NewServer(extractMock{})
	persistServer := httptest.NewServer(textStore)
	auditServer := httptest.NewServer(auditStore)
	t.Cleanup(func() {
		extractServer.Close()
		persistServer.Close()
		auditServer.Close()
	})

	logger := discardLogger()
	httpTimeout := 5 * time.Second
	auditClient := auditlog.NewClient(auditServer.URL, httpTimeout)
	auditService := services.NewAuditService(auditClient, logger, httpTimeout)
	pdfService := services.NewPDFService(extract.NewClient(extractServer.URL, httpTimeout), auditService)
	textService := services.NewTextService(persistence.NewClient(persistServer.URL, httpTimeout), auditService)

	router := Routes(
		config.Config{Port: "8080"},
		logger,
		handlers.NewPDFHandler(pdfService, testMaxPDFSize),
		handlers.NewAuditHandler(auditService),
		handlers.NewTextHandler(textService, 4*1024*1024),
	)

	return &mockServers{
		extract:    extractServer,
		persist:    persistServer,
		audit:      auditServer,
		auditStore: auditStore,
		textStore:  textStore,
		router:     router,
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
		Type:  "about:blank",
		Title: http.StatusText(status),
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

func TestIntegrationExtractPersistAuditFlow(t *testing.T) {
	t.Parallel()

	stack := newIntegrationStack(t)

	extractResponse := serve(stack.router, http.MethodPost, "/api/v1/pdfs/extract", "%PDF-1.7 contenido de prueba", map[string]string{"Content-Type": "application/pdf"})
	if extractResponse.Code != http.StatusOK {
		t.Fatalf("extract status = %d, want %d", extractResponse.Code, http.StatusOK)
	}
	var extracted dto.ExtractPDFResponse
	if err := json.Unmarshal(extractResponse.Body.Bytes(), &extracted); err != nil {
		t.Fatalf("extract response is not valid JSON: %v", err)
	}
	if extracted.Checksum == "" || extracted.Text != "texto integrado del pdf" {
		t.Fatalf("extract response = %+v, want checksum and full text", extracted)
	}

	checksum := string(extracted.Checksum)
	createBody := fmt.Sprintf(`{"text":"%s","checksum":"%s","name":"documento integracion"}`, extracted.Text, checksum)
	createResponse := serve(stack.router, http.MethodPost, "/api/v1/texts", createBody, map[string]string{"Content-Type": "application/json"})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (body: %s)", createResponse.Code, http.StatusCreated, createResponse.Body)
	}

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