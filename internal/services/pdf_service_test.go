package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/httpclient"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/reqctx"
	"validationmicroservices-pdf-extractext/internal/testsupport"
	"validationmicroservices-pdf-extractext/internal/validation"
)

type stubExtractor struct {
	extracted    dto.ExtractedDocument
	err          error
	pdfReceived  []byte
	extractCalls int
}

func (s *stubExtractor) Extract(_ context.Context, pdfData []byte) (dto.ExtractedDocument, error) {
	s.extractCalls++
	s.pdfReceived = pdfData
	return s.extracted, s.err
}

type stubAudit struct {
	events []models.AuditEvent
}

func (s *stubAudit) LogAsync(_ context.Context, event models.AuditEvent) {
	s.events = append(s.events, event)
}

func (s *stubAudit) FetchLogs(_ context.Context, _ dto.AuditQueryParams) (dto.AuditLogsResponse, error) {
	return dto.AuditLogsResponse{}, nil
}

func (s *stubAudit) Close(_ context.Context) error { return nil }

func checksumOfBytes(data []byte) models.Checksum {
	sum := sha256.Sum256(data)
	return models.Checksum(hex.EncodeToString(sum[:]))
}

// missPersistence modela el caso habitual: el checksum todavía no está
// almacenado, así que la consulta responde 404 y el servicio debe continuar
// hacia el Extract.
func missPersistence() *stubPersistence {
	return &stubPersistence{findError: httpclient.Problem{
		Title:  "Not Found",
		Status: http.StatusNotFound,
		Detail: "checksum not found",
	}}
}

func newTestPDFService(extractor ExtractClient, persistence PersistenceClient, audit AuditService, maxPages int) PDFService {
	return NewPDFService(extractor, persistence, audit, discardLogger(), maxPages)
}

// TestPDFServiceUsesPDFCPUPageCountNotExtractPageCount fija la decisión de la
// Opción 1: en un hit de dedup no hay Extract, así que el número de páginas debe
// venir de pdfcpu. El mock informa un número de páginas deliberadamente erróneo
// para demostrar que se ignora.
func TestPDFServiceUsesPDFCPUPageCountNotExtractPageCount(t *testing.T) {
	t.Parallel()

	service := newTestPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 99, Content: "hola mundo"}},
		missPersistence(),
		&stubAudit{},
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if response.PageCount != testsupport.ScrumGuidePages {
		t.Errorf("PageCount = %d, want %d (pdfcpu's count, not the Extract's 99)", response.PageCount, testsupport.ScrumGuidePages)
	}
}

// TestPDFServiceReturnsChecksumOfThePDFBytes fija el nuevo invariante de la
// Fase 8: el checksum de la respuesta es SHA-256 sobre los bytes del PDF subido,
// no sobre el texto extraído. El texto aquí es deliberadamente distinto, para
// que una regresión al checksum del texto se detecte.
func TestPDFServiceReturnsChecksumOfThePDFBytes(t *testing.T) {
	t.Parallel()

	pdf := testsupport.ScrumGuidePDF(t)
	const content = "contenido extraido que no se hashea"
	service := newTestPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{Content: content}},
		missPersistence(),
		&stubAudit{},
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), pdf, "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if want := checksumOfBytes(pdf); response.Checksum != want {
		t.Errorf("Checksum = %q, want SHA-256 of the PDF bytes %q", response.Checksum, want)
	}
	if response.Checksum == checksumOfBytes([]byte(content)) {
		t.Error("Checksum must not be SHA-256 of the text")
	}
}

// TestPDFServiceReturnsExtractContentVerbatim blinda la decisión de diseño: el
// Extract ya devuelve el texto formateado, así que el orquestador no lo vuelve a
// convertir.
func TestPDFServiceReturnsExtractContentVerbatim(t *testing.T) {
	t.Parallel()

	const content = "PRINCIPIOS\n\nEl scrum es un marco iterativo e incremental\npara\ndesarrollar productos."
	service := newTestPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: content}},
		missPersistence(),
		&stubAudit{},
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if response.Text != content {
		t.Errorf("Text = %q, want the Extract content verbatim %q", response.Text, content)
	}
}

// TestPDFServiceDedupHitReturnsStoredTextWithoutExtracting es el núcleo del
// dedup: cuando los bytes del PDF ya están almacenados, la respuesta sale del
// registro y no se toca ni el Extract ni la auditoría. Un hit que siguiera
// llamando al Extract anularía el objetivo; un hit que auditara registraría una
// extracción que nunca ocurrió.
func TestPDFServiceDedupHitReturnsStoredTextWithoutExtracting(t *testing.T) {
	t.Parallel()

	pdf := testsupport.ScrumGuidePDF(t)
	const storedText = "texto ya almacenado en persistence"
	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "no deberia usarse"}}
	audit := &stubAudit{}
	persistence := &stubPersistence{
		foundText: models.Text{Checksum: checksumOfBytes(pdf), Text: storedText},
	}
	service := newTestPDFService(extractor, persistence, audit, 1000)

	response, err := service.IngestAndExtract(context.Background(), pdf, "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if response.Text != storedText {
		t.Errorf("Text = %q, want the stored record %q", response.Text, storedText)
	}
	if want := checksumOfBytes(pdf); response.Checksum != want {
		t.Errorf("Checksum = %q, want %q", response.Checksum, want)
	}
	if response.PageCount != testsupport.ScrumGuidePages {
		t.Errorf("PageCount = %d, want %d (pdfcpu's count on a hit)", response.PageCount, testsupport.ScrumGuidePages)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0 (a hit must not call Extract)", extractor.extractCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (a hit must not audit)", len(audit.events))
	}
	if !persistence.findCalled {
		t.Error("persistence lookup was not performed")
	}
	if persistence.findChecksum != checksumOfBytes(pdf) {
		t.Errorf("looked up %q, want %q", persistence.findChecksum, checksumOfBytes(pdf))
	}
}

// TestPDFServiceDedupMissExtractsAndAudits: un 404 de Persistence es un miss,
// no un fallo. El flujo normal de Extract + auditoría debe ejecutarse.
func TestPDFServiceDedupMissExtractsAndAudits(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "extraido del pdf"}}
	audit := &stubAudit{}
	persistence := missPersistence()
	service := newTestPDFService(extractor, persistence, audit, 1000)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if extractor.extractCalls != 1 {
		t.Errorf("extract calls = %d, want 1", extractor.extractCalls)
	}
	if response.Text != "extraido del pdf" {
		t.Errorf("Text = %q, want the Extract content", response.Text)
	}
	if len(audit.events) != 1 {
		t.Errorf("audit events = %d, want 1", len(audit.events))
	}
}

// TestPDFServiceDedupFailOpenWhenPersistenceFails: que Persistence esté caído no
// debe llevarse consigo la extracción. El servicio registra un warning y extrae
// igual; la respuesta es un 200 normal.
func TestPDFServiceDedupFailOpenWhenPersistenceFails(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "extraido pese al fallo"}}
	audit := &stubAudit{}
	persistence := &stubPersistence{findError: httpclient.Problem{
		Title:  "Service Unavailable",
		Status: http.StatusServiceUnavailable,
		Detail: "persistence down",
	}}
	service := NewPDFService(extractor, persistence, audit, logger, 1000)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if extractor.extractCalls != 1 {
		t.Errorf("extract calls = %d, want 1 (fail-open extracts anyway)", extractor.extractCalls)
	}
	if response.Text != "extraido pese al fallo" {
		t.Errorf("Text = %q, want the Extract content", response.Text)
	}
	if len(audit.events) != 1 {
		t.Errorf("audit events = %d, want 1", len(audit.events))
	}
	if !strings.Contains(logs.String(), "dedup lookup failed") {
		t.Errorf("expected a dedup-failure warning, got logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Errorf("expected the fail-open log at WARN level, got: %s", logs.String())
	}
}

// TestPDFServiceDedupLogIncludesRequestID: el warning de fail-open debe poder
// correlacionarse con su request, así que arrastra el request_id del contexto.
func TestPDFServiceDedupLogIncludesRequestID(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "extraido pese al fallo"}}
	persistence := &stubPersistence{findError: httpclient.Problem{
		Title:  "Service Unavailable",
		Status: http.StatusServiceUnavailable,
		Detail: "persistence down",
	}}
	service := NewPDFService(extractor, persistence, &stubAudit{}, logger, 1000)

	ctx := reqctx.WithRequestID(context.Background(), "req-dedup")
	if _, err := service.IngestAndExtract(ctx, testsupport.ScrumGuidePDF(t), "informe.pdf"); err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}

	if !strings.Contains(logs.String(), `"request_id":"req-dedup"`) {
		t.Errorf("dedup log does not carry request_id, got: %s", logs.String())
	}
}

// TestPDFServiceDoesNotLookUpPersistenceWhenValidationFails: PreExtract se
// ejecuta antes del dedup, así que un PDF rechazado nunca llega a Persistence.
func TestPDFServiceDoesNotLookUpPersistenceWhenValidationFails(t *testing.T) {
	t.Parallel()

	persistence := &stubPersistence{foundText: models.Text{Text: "no deberia consultarse"}}
	service := newTestPDFService(&stubExtractor{}, persistence, &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), []byte("no soy un pdf"), "informe.pdf")

	if !errors.Is(err, validation.ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
	if persistence.findCalled {
		t.Error("persistence must not be consulted when PreExtract rejects the PDF")
	}
}

// TestPDFServiceRejectsPDFWithoutExtractableText cubre el PDF escaneado: el
// Extract responde 200 con page_count > 0 y content vacío porque el PDF tiene
// páginas pero ninguna capa de texto.
func TestPDFServiceRejectsPDFWithoutExtractableText(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{PageCount: 3}}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if !errors.Is(err, validation.ErrNoExtractableText) {
		t.Fatalf("error = %v, want %v", err, validation.ErrNoExtractableText)
	}
}

// TestPDFServiceRejectsPDFWithOnlyWhitespaceText: mismo caso, pero con el
// contenido en blanco. Es distinto del vacío y hay que cubrirlo aparte.
func TestPDFServiceRejectsPDFWithOnlyWhitespaceText(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "   ", "\n\n", "\r\n \t \n"} {
		service := newTestPDFService(
			&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: content}},
			missPersistence(),
			&stubAudit{},
			1000,
		)

		_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

		if !errors.Is(err, validation.ErrNoExtractableText) {
			t.Errorf("content %q: error = %v, want %v", content, err, validation.ErrNoExtractableText)
		}
	}
}

// TestPDFServiceDoesNotAuditRejectedPDF: un PDF sin texto no llegó a procesarse,
// así que no debe generar auditoría. Si se auditara, el log afirmaría una
// extracción que nunca ocurrió.
func TestPDFServiceDoesNotAuditRejectedPDF(t *testing.T) {
	t.Parallel()

	audit := &stubAudit{}
	service := newTestPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 2}},
		missPersistence(),
		audit,
		1000,
	)

	_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if !errors.Is(err, validation.ErrNoExtractableText) {
		t.Fatalf("error = %v, want %v", err, validation.ErrNoExtractableText)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0", len(audit.events))
	}
}

// TestPDFServiceAuditsTheResponseChecksum: la auditoría tiene que llevar el mismo
// checksum que la respuesta, o el registro y la respuesta describirían
// documentos distintos.
func TestPDFServiceAuditsTheResponseChecksum(t *testing.T) {
	t.Parallel()

	audit := &stubAudit{}
	service := newTestPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: "SCRUM\n\ncuerpo"}},
		missPersistence(),
		audit,
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), 1)
	}
	if audit.events[0].Checksum != response.Checksum {
		t.Errorf("audit checksum = %q, want %q", audit.events[0].Checksum, response.Checksum)
	}
}

func TestPDFServiceDelegatesRawPDFBytesToExtractClient(t *testing.T) {
	t.Parallel()

	pdfBytes := testsupport.ScrumGuidePDF(t)
	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "texto"}}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), pdfBytes, "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if extractor.extractCalls != 1 {
		t.Errorf("extract calls = %d, want %d", extractor.extractCalls, 1)
	}
	if string(extractor.pdfReceived) != string(pdfBytes) {
		t.Errorf("client received %q, want %q", extractor.pdfReceived, pdfBytes)
	}
}

func TestPDFServiceRejectsBodyWithoutPDFSignatureBeforeExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	audit := &stubAudit{}
	service := newTestPDFService(extractor, missPersistence(), audit, 1000)

	_, err := service.IngestAndExtract(context.Background(), []byte("no soy un pdf"), "informe.pdf")

	if !errors.Is(err, validation.ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0 (no debe delegar)", extractor.extractCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (PDF inválido no debe auditarse)", len(audit.events))
	}
}

func TestPDFServiceRejectsEmptyBodyBeforeExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), nil, "informe.pdf")

	if !errors.Is(err, validation.ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0", extractor.extractCalls)
	}
}

func TestPDFServiceRejectsUnsupportedExtensionBeforeExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.exe")

	if !errors.Is(err, validation.ErrUnsupportedExtension) {
		t.Fatalf("error = %v, want ErrUnsupportedExtension", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0", extractor.extractCalls)
	}
}

func TestPDFServiceRejectsMalformedPDFBeforeExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7\ngarbage"), "informe.pdf")

	if !errors.Is(err, validation.ErrMalformedPDF) {
		t.Fatalf("error = %v, want ErrMalformedPDF", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0", extractor.extractCalls)
	}
}

func TestPDFServiceRejectsTooManyPagesBeforeExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	service := newTestPDFService(extractor, missPersistence(), &stubAudit{}, testsupport.ScrumGuidePages-1)

	_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if !errors.Is(err, validation.ErrTooManyPages) {
		t.Fatalf("error = %v, want ErrTooManyPages", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0", extractor.extractCalls)
	}
}

func TestPDFServicePropagatesExtractClientError(t *testing.T) {
	t.Parallel()

	extractErr := errors.New("extract unreachable")
	extractor := &stubExtractor{err: extractErr}
	audit := &stubAudit{}
	service := newTestPDFService(extractor, missPersistence(), audit, 1000)

	_, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if !errors.Is(err, extractErr) {
		t.Fatalf("error = %v, want %v", err, extractErr)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (fallo de extract no debe auditarse)", len(audit.events))
	}
}

func TestPDFServiceEmitsAuditEventAfterSuccessfulExtract(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{
		extracted: dto.ExtractedDocument{PageCount: 1, Content: "hola mundo"},
	}
	audit := &stubAudit{}
	service := newTestPDFService(extractor, missPersistence(), audit, 1000)

	response, err := service.IngestAndExtract(context.Background(), testsupport.ScrumGuidePDF(t), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want %d", len(audit.events), 1)
	}
	event := audit.events[0]
	if event.Action != models.OpPDFExtract {
		t.Errorf("Action = %q, want %q", event.Action, models.OpPDFExtract)
	}
	if event.EntityType != "document" {
		t.Errorf("EntityType = %q, want %q", event.EntityType, "document")
	}
	if event.Checksum != response.Checksum {
		t.Errorf("Checksum = %q, want %q", event.Checksum, response.Checksum)
	}
	if event.PerformedAt.IsZero() {
		t.Error("PerformedAt is zero")
	}
	details, ok := event.Details.(map[string]any)
	if !ok {
		t.Fatalf("Details = %T, want map[string]any", event.Details)
	}
	if details["page_count"] != testsupport.ScrumGuidePages {
		t.Errorf("Details[page_count] = %v, want %d (pdfcpu's count)", details["page_count"], testsupport.ScrumGuidePages)
	}
}
