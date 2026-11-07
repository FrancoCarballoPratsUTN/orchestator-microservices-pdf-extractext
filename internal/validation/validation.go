package validation

// Input es la materia prima de la cadena de validación previa a la extracción.
// MaxPages viaja con la entrada para poder agregar el límite de páginas a
// PreExtract sin cambiar su firma.
type Input struct {
	PDF      []byte
	Filename string
	MaxPages int
}

// Result informa lo que la cadena previa a la extracción aprendió del PDF.
type Result struct {
	PageCount int
}

// PreExtract ejecuta las validaciones fail-fast que deben rechazar un PDF antes
// de que llegue al servicio Extract, deteniéndose en el primer fallo: extensión
// del nombre de archivo, firma %PDF-, luego estructura/cifrado/número de páginas
// vía pdfcpu y, por último, el límite de páginas configurado.
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
	if err := checkPageLimit(pageCount, input.MaxPages); err != nil {
		return Result{}, err
	}
	return Result{PageCount: pageCount}, nil
}
