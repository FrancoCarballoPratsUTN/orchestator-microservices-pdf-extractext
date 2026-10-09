package testpdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func config() *model.Configuration {
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

func TestBuildProducesValidPDFs(t *testing.T) {
	t.Parallel()

	for _, pages := range []int{1, 2, 3, 5} {
		pdf := Build(pages)
		count, err := api.PageCount(context.Background(), bytes.NewReader(pdf), config())
		if err != nil {
			t.Fatalf("Build(%d): pdfcpu rejected it: %v", pages, err)
		}
		if count != pages {
			t.Errorf("Build(%d): PageCount = %d, want %d", pages, count, pages)
		}
	}
}

func TestBuildWithCommentProducesDistinctValidPDFs(t *testing.T) {
	t.Parallel()

	first, second := BuildWithComment(1, "doc-a"), BuildWithComment(1, "doc-b")
	if bytes.Equal(first, second) {
		t.Fatal("BuildWithComment produced identical bytes for different comments")
	}
	for _, pdf := range [][]byte{first, second} {
		if _, err := api.PageCount(context.Background(), bytes.NewReader(pdf), config()); err != nil {
			t.Fatalf("BuildWithComment: pdfcpu rejected it: %v", err)
		}
	}
}
