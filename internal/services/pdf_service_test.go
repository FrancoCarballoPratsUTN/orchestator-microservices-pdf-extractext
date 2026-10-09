package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
	"validationmicroservices-pdf-extractext/internal/testpdf"
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

func checksumOf(text string) models.Checksum {
	sum := sha256.Sum256([]byte(text))
	return models.Checksum(hex.EncodeToString(sum[:]))
}

// TestPDFServiceUsesPDFCPUPageCountNotExtractPageCount pins the Opción-1
// decision: on a dedup hit there is no Extract, so the page count must come from
// pdfcpu. The mock reports a deliberately wrong page count to prove it is
// ignored.
func TestPDFServiceUsesPDFCPUPageCountNotExtractPageCount(t *testing.T) {
	t.Parallel()

	service := NewPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 99, Content: "hola mundo"}},
		&stubAudit{},
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testpdf.Build(2), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if response.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2 (pdfcpu's count, not the Extract's 99)", response.PageCount)
	}
}

// TestPDFServiceReturnsExtractContentVerbatim blinda la decisión de diseño: el
// Extract ya devuelve el texto formateado, así que el orquestador no lo vuelve a
// convertir. La respuesta `text` es el `content` del Extract tal cual, y el
// checksum es su SHA-256 (invariante checksum == SHA-256(text)).
func TestPDFServiceReturnsExtractContentVerbatim(t *testing.T) {
	t.Parallel()

	const content = "PRINCIPIOS\n\nEl scrum es un marco iterativo e incremental\npara\ndesarrollar productos."
	service := NewPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: content}},
		&stubAudit{},
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	if response.Text != content {
		t.Errorf("Text = %q, want the Extract content verbatim %q", response.Text, content)
	}
	if wantChecksum := checksumOf(content); response.Checksum != wantChecksum {
		t.Errorf("Checksum = %q, want SHA-256 of the content %q", response.Checksum, wantChecksum)
	}
}

// TestPDFServiceRejectsPDFWithoutExtractableText cubre el PDF escaneado: el
// Extract responde 200 con page_count > 0 y content vacío porque el PDF tiene
// páginas pero ninguna capa de texto.
func TestPDFServiceRejectsPDFWithoutExtractableText(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{PageCount: 3}}
	service := NewPDFService(extractor, &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

	if !errors.Is(err, validation.ErrNoExtractableText) {
		t.Fatalf("error = %v, want %v", err, validation.ErrNoExtractableText)
	}
}

// TestPDFServiceRejectsPDFWithOnlyWhitespaceText: mismo caso, pero con el
// contenido en blanco. Es distinto del vacío y hay que cubrirlo aparte.
func TestPDFServiceRejectsPDFWithOnlyWhitespaceText(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "   ", "\n\n", "\r\n \t \n"} {
		service := NewPDFService(
			&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: content}},
			&stubAudit{},
			1000,
		)

		_, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

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
	service := NewPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 2}},
		audit,
		1000,
	)

	_, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

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
	service := NewPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: "SCRUM\n\ncuerpo"}},
		audit,
		1000,
	)

	response, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

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

	pdfBytes := testpdf.Build(1)
	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "texto"}}
	service := NewPDFService(extractor, &stubAudit{}, 1000)

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
	service := NewPDFService(extractor, audit, 1000)

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
	service := NewPDFService(extractor, &stubAudit{}, 1000)

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
	service := NewPDFService(extractor, &stubAudit{}, 1000)

	_, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.exe")

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
	service := NewPDFService(extractor, &stubAudit{}, 1000)

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
	service := NewPDFService(extractor, &stubAudit{}, 2)

	_, err := service.IngestAndExtract(context.Background(), testpdf.Build(3), "informe.pdf")

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
	service := NewPDFService(extractor, audit, 1000)

	_, err := service.IngestAndExtract(context.Background(), testpdf.Build(1), "informe.pdf")

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
	service := NewPDFService(extractor, audit, 1000)

	response, err := service.IngestAndExtract(context.Background(), testpdf.Build(2), "informe.pdf")

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
	if details["page_count"] != 2 {
		t.Errorf("Details[page_count] = %v, want 2 (pdfcpu's count)", details["page_count"])
	}
}
