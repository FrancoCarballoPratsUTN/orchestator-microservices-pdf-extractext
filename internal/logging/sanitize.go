package logging

import (
	"errors"
	"net/url"
	"unicode/utf8"
)

// MaxValueLength acota la longitud de un valor que viene de fuera (una
// cabecera, un nombre de fichero, el mensaje de un panic). Sin un tope, un
// cliente podría escribir megabytes en una línea de log.
const MaxValueLength = 256

// MaxStackLength acota la longitud de un stack de panic. El stack completo
// puede ocupar kilobytes: interesa el principio (dónde empezó), no la cola.
const MaxStackLength = 4096

// SafeURL devuelve una URL apta para loguear: conserva esquema, host y ruta,
// y descarta las credenciales embebidas, el query string y el fragmento.
//
// El caso que motiva la función es una URL de servicio mal configurada con
// usuario y contraseña (http://user:pass@host). Loguear la URL cruda
// publicaría el secreto en stdout; el log sólo necesita saber a qué upstream
// se llamó.
//
// El control de caracteres no es responsabilidad de esta función: los
// handlers de slog (JSON y texto) escapan el valor al escribirlo.
func SafeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		// Una URL que ni siquiera parsea no debe llegar al log tal cual.
		return Truncate(raw, MaxValueLength)
	}

	safe := url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
		Path:   parsed.Path,
	}
	if safe.Scheme == "" && safe.Host == "" {
		return Truncate(raw, MaxValueLength)
	}
	return Truncate(safe.String(), MaxValueLength)
}

// SafeError devuelve el texto de un error sin la URL que net/http envuelve
// alrededor.
//
// El error de transporte de net/http es un *url.Error que incluye la URL
// completa del request. Esa URL viene de la configuración y puede llevar un
// usuario o un token en el query string, y el error acaba en stdout y en la
// respuesta de error: la URL sólo se conserva como campo estructurado y
// saneado (SafeURL). Aquí queda el error de verdad, que es el que explica el
// fallo.
//
// Un error sin URL se devuelve tal cual.
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}

// Truncate acota un valor a max runes y marca el recorte con "…" para que un
// lector sepa que la línea está incompleta. Un max de 0 o menos significa
// "sin límite" y devuelve el valor intacto.
//
// El control de caracteres no es responsabilidad de esta función: los handlers
// de slog (JSON y texto) escapan el valor al escribirlo.
func Truncate(value string, max int) string {
	if max <= 0 {
		return value
	}
	// El conteo evita copiar la cadena entera en un []rune cuando el valor
	// cabe: los valores que vienen de fuera pueden ser grandes.
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	return string([]rune(value)[:max]) + "…"
}
