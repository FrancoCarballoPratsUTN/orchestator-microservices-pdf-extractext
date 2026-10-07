package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"validationmicroservices-pdf-extractext/internal/dto"
	"validationmicroservices-pdf-extractext/internal/models"
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

func TestPDFServiceComputesChecksumOverExtractedText(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{
		extracted: dto.ExtractedDocument{
			PageCount: 2,
			Content:   "hola mundo",
		},
	}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	response, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}
	sum := sha256.Sum256([]byte("hola mundo"))
	wantChecksum := models.Checksum(hex.EncodeToString(sum[:]))
	if response.Checksum != wantChecksum {
		t.Errorf("Checksum = %q, want %q", response.Checksum, wantChecksum)
	}
	if response.PageCount != 2 {
		t.Errorf("PageCount = %d, want %d", response.PageCount, 2)
	}
	if response.Text != "hola mundo" {
		t.Errorf("Text = %q, want %q", response.Text, "hola mundo")
	}
}

// TestPDFServiceReturnsMarkdownInTextAndHashesIt blinda la decisión de diseño:
// la respuesta se llama `text` pero lleva markdown, y el checksum es el SHA-256
// de ESE markdown, no del texto crudo del Extract. Si el checksum se calculara
// sobre el crudo, dos PDFs con el mismo contenido pero distinto formato de salto
// de línea darían checksums distintos, y con él no se podría detectar contenido
// duplicado.
func TestPDFServiceReturnsMarkdownInTextAndHashesIt(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{
		extracted: dto.ExtractedDocument{
			PageCount: 1,
			Content:   "PRINCIPIOS\n\nEl scrum es un marco iterativo e incremental\npara\ndesarrollar productos.",
		},
	}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	response, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

	if err != nil {
		t.Fatalf("IngestAndExtract() unexpected error: %v", err)
	}

	want := "## PRINCIPIOS\n\nEl scrum es un marco iterativo e incremental para desarrollar productos."
	if response.Text != want {
		t.Errorf("Text = %q, want %q", response.Text, want)
	}

	sum := sha256.Sum256([]byte(want))
	if wantChecksum := models.Checksum(hex.EncodeToString(sum[:])); response.Checksum != wantChecksum {
		t.Errorf("Checksum = %q, want SHA-256 of the markdown %q", response.Checksum, wantChecksum)
	}
}

// TestPDFServiceGivesSameChecksumForSameContentWithDifferentLineBreaks documenta
// la consecuencia útil de hashear el markdown: el formato de salto de línea del
// PDF deja de cambiar la identidad del documento.
func TestPDFServiceGivesSameChecksumForSameContentWithDifferentLineBreaks(t *testing.T) {
	t.Parallel()

	contents := []string{
		"El scrum es un marco iterativo e incremental para\ndesarrollar productos.",
		"El scrum es un marco iterativo e incremental para\r\ndesarrollar productos.",
	}

	checksums := make([]models.Checksum, 0, len(contents))
	for _, content := range contents {
		service := NewPDFService(
			&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: content}},
			&stubAudit{},
		)

		response, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))
		if err != nil {
			t.Fatalf("IngestAndExtract() unexpected error: %v", err)
		}
		checksums = append(checksums, response.Checksum)
	}

	if checksums[0] != checksums[1] {
		t.Errorf("checksums differ for the same content: %q vs %q", checksums[0], checksums[1])
	}
}

// TestPDFServiceRejectsPDFWithoutExtractableText cubre el PDF escaneado: el
// Extract responde 200 con page_count > 0 y content vacío porque el PDF tiene
// páginas pero ninguna capa de texto.
func TestPDFServiceRejectsPDFWithoutExtractableText(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{extracted: dto.ExtractedDocument{PageCount: 3}}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

	if !errors.Is(err, ErrNoExtractableText) {
		t.Fatalf("error = %v, want %v", err, ErrNoExtractableText)
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
		)

		_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

		if !errors.Is(err, ErrNoExtractableText) {
			t.Errorf("content %q: error = %v, want %v", content, err, ErrNoExtractableText)
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
	)

	_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

	if !errors.Is(err, ErrNoExtractableText) {
		t.Fatalf("error = %v, want %v", err, ErrNoExtractableText)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0", len(audit.events))
	}
}

// TestPDFServiceAuditsTheMarkdownChecksum: la auditoría tiene que llevar el mismo
// checksum que la respuesta, o el registro y la respuesta describirían
// documentos distintos.
func TestPDFServiceAuditsTheMarkdownChecksum(t *testing.T) {
	t.Parallel()

	audit := &stubAudit{}
	service := NewPDFService(
		&stubExtractor{extracted: dto.ExtractedDocument{PageCount: 1, Content: "SCRUM\n\ncuerpo"}},
		audit,
	)

	response, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

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

	pdfBytes := []byte("%PDF-1.7\nbinary payload")
	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Content: "texto"}}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	_, err := service.IngestAndExtract(context.Background(), pdfBytes)

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

func TestPDFServiceRejectsBodyWithoutPDFSignature(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	_, err := service.IngestAndExtract(context.Background(), []byte("no soy un pdf"))

	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("error = %v, want ErrInvalidPDF", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0 (no debe delegar)", extractor.extractCalls)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (PDF inválido no debe auditarse)", len(audit.events))
	}
}

func TestPDFServiceRejectsEmptyBody(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	_, err := service.IngestAndExtract(context.Background(), nil)

	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("error = %v, want ErrInvalidPDF", err)
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %d, want 0 (body vacío no debe auditarse)", len(audit.events))
	}
}

func TestPDFServicePropagatesExtractClientError(t *testing.T) {
	t.Parallel()

	extractErr := errors.New("extract unreachable")
	extractor := &stubExtractor{err: extractErr}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-"))

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
		extracted: dto.ExtractedDocument{PageCount: 2, Content: "hola mundo"},
	}
	audit := &stubAudit{}
	service := NewPDFService(extractor, audit)

	response, err := service.IngestAndExtract(context.Background(), []byte("%PDF-1.7"))

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
		t.Errorf("Details[page_count] = %v, want %v", details["page_count"], 2)
	}
}
