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

func TestPDFServiceComputesChecksumOverExtractedText(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{
		extracted: dto.ExtractedDocument{
			PageCount: 2,
			Pages:     []dto.ExtractedPage{{PageNumber: 1, Text: "hola"}, {PageNumber: 2, Text: "mundo"}},
			Text:      "hola mundo",
		},
	}
	service := NewPDFService(extractor)

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

func TestPDFServiceDelegatesRawPDFBytesToExtractClient(t *testing.T) {
	t.Parallel()

	pdfBytes := []byte("%PDF-1.7\nbinary payload")
	extractor := &stubExtractor{extracted: dto.ExtractedDocument{Text: "texto"}}
	service := NewPDFService(extractor)

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
	service := NewPDFService(extractor)

	_, err := service.IngestAndExtract(context.Background(), []byte("no soy un pdf"))

	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("error = %v, want ErrInvalidPDF", err)
	}
	if extractor.extractCalls != 0 {
		t.Errorf("extract calls = %d, want 0 (no debe delegar)", extractor.extractCalls)
	}
}

func TestPDFServiceRejectsEmptyBody(t *testing.T) {
	t.Parallel()

	extractor := &stubExtractor{}
	service := NewPDFService(extractor)

	_, err := service.IngestAndExtract(context.Background(), nil)

	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("error = %v, want ErrInvalidPDF", err)
	}
}

func TestPDFServicePropagatesExtractClientError(t *testing.T) {
	t.Parallel()

	extractErr := errors.New("extract unreachable")
	extractor := &stubExtractor{err: extractErr}
	service := NewPDFService(extractor)

	_, err := service.IngestAndExtract(context.Background(), []byte("%PDF-"))

	if !errors.Is(err, extractErr) {
		t.Fatalf("error = %v, want %v", err, extractErr)
	}
}