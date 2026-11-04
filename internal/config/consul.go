package config

import (
	"fmt"
	"strings"
)

// ConsulSettings agrupa la configuración de Consul: bootstrap del KV y datos de
// registro del servicio. Se parsea una sola vez para que el composition root no
// lea variables de entorno dispersas.
type ConsulSettings struct {
	Enabled        bool
	HTTPAddr       string
	HTTPToken      string
	HTTPCAFile     string
	KVPrefix       string
	ServiceAddress string
	ServiceName    string
	ServiceID      string
	CheckURL       string
	Tags           []string
}

// LoadConsulSettings parsea CONSUL_* y exige las variables de bootstrap cuando
// CONSUL_ENABLED=true. Con Consul deshabilitado devuelve la configuración
// vacía sin fallar.
func LoadConsulSettings(getenv func(string) string) (ConsulSettings, error) {
	settings := ConsulSettings{
		Enabled:        strings.EqualFold(strings.TrimSpace(getenv("CONSUL_ENABLED")), "true"),
		HTTPAddr:       strings.TrimSpace(getenv("CONSUL_HTTP_ADDR")),
		HTTPToken:      strings.TrimSpace(getenv("CONSUL_HTTP_TOKEN")),
		HTTPCAFile:     strings.TrimSpace(getenv("CONSUL_HTTP_CA_FILE")),
		KVPrefix:       strings.TrimSpace(getenv("CONSUL_KV_PREFIX")),
		ServiceAddress: strings.TrimSpace(getenv("CONSUL_SERVICE_ADDRESS")),
		ServiceName:    strings.TrimSpace(getenv("CONSUL_SERVICE_NAME")),
		ServiceID:      strings.TrimSpace(getenv("CONSUL_SERVICE_ID")),
		CheckURL:       strings.TrimSpace(getenv("CONSUL_SERVICE_CHECK_URL")),
		Tags:           splitTags(getenv("CONSUL_SERVICE_TAGS")),
	}
	if !settings.Enabled {
		return settings, nil
	}
	for _, required := range []struct {
		key   string
		value string
	}{
		{key: "CONSUL_HTTP_ADDR", value: settings.HTTPAddr},
		{key: "CONSUL_HTTP_TOKEN", value: settings.HTTPToken},
		{key: "CONSUL_KV_PREFIX", value: settings.KVPrefix},
	} {
		if required.value == "" {
			return ConsulSettings{}, fmt.Errorf("%s must be set and non-empty when CONSUL_ENABLED=true", required.key)
		}
	}
	return settings, nil
}

// splitTags separa por ';'. Las comas son parte del valor (por ejemplo la lista
// de middlewares de Traefik), por lo que no deben usarse como separador.
func splitTags(raw string) []string {
	var tags []string
	for _, tag := range strings.Split(raw, ";") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
