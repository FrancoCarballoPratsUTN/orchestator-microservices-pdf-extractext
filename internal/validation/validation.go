package validation

// Input is the raw material of the pre-extraction validation chain. MaxPages
// travels with the input so the page-count gate can be appended to PreExtract
// without changing its signature.
type Input struct {
	PDF      []byte
	Filename string
	MaxPages int
}

// Result reports what the pre-extraction chain learned about the PDF.
type Result struct {
	PageCount int
}

// PreExtract runs the fail-fast validations that must reject a PDF before it
// reaches the Extract service, stopping at the first failure: file name
// extension, %PDF- signature, then structure/encryption/page count via pdfcpu.
// The page-count limit is appended to the same chain by a later task.
func PreExtract(input Input) (Result, error) {
	if err := checkExtension(input.Filename); err != nil {
		return Result{}, err
	}
	if err := checkSignature(input.PDF); err != nil {
		return Result{}, err
	}
	pageCount, err := analyzePDF(input.PDF)
	if err != nil {
		return Result{}, err
	}
	return Result{PageCount: pageCount}, nil
}
