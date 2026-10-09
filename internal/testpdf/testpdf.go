// Package testpdf builds minimal, structurally valid PDFs so tests do not have
// to commit binary fixtures. It is test support code: the only callers live in
// _test.go files.
package testpdf

import (
	"bytes"
	"fmt"
	"strings"
)

// Build returns a minimal, structurally valid PDF with pageCount empty pages.
func Build(pageCount int) []byte {
	return build(pageCount, "")
}

// BuildWithComment is Build plus a PDF comment line carrying comment, so two
// otherwise identical PDFs can be made byte-distinct and therefore
// checksum-distinct in tests.
func BuildWithComment(pageCount int, comment string) []byte {
	return build(pageCount, comment)
}

func build(pageCount int, comment string) []byte {
	var buf bytes.Buffer

	offsets := make([]int, 0, pageCount+2)
	writeObject := func(number int, body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", number, body)
	}

	buf.WriteString("%PDF-1.7\n")
	if comment != "" {
		fmt.Fprintf(&buf, "%% %s\n", comment)
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")

	kids := make([]string, pageCount)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	writeObject(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount))

	for i := 0; i < pageCount; i++ {
		writeObject(3+i, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>")
	}

	xrefOffset := buf.Len()
	objectCount := pageCount + 3 // free object 0 plus the two roots plus the pages
	fmt.Fprintf(&buf, "xref\n0 %d\n", objectCount)
	buf.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", objectCount, xrefOffset)

	return buf.Bytes()
}
