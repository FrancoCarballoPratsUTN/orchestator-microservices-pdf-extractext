package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadConsulSettingsDisabledByDefault(t *testing.T) {
	t.Parallel()

	settings, err := LoadConsulSettings(envWith(nil))
	if err != nil {
		t.Fatalf("LoadConsulSettings() unexpected error: %v", err)
	}
	if settings.Enabled {
		t.Error("Enabled = true, want false when CONSUL_ENABLED is unset")
	}
}

func TestLoadConsulSettingsRequiresBootstrapValuesWhenEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		values  map[string]string
		missing string
	}{
		{
			name: "address",
			values: map[string]string{
				"CONSUL_ENABLED":    "true",
				"CONSUL_HTTP_TOKEN": "token",
				"CONSUL_KV_PREFIX":  "pdf-extractext/dev/orchestrator/",
			},
			missing: "CONSUL_HTTP_ADDR",
		},
		{
			name: "token",
			values: map[string]string{
				"CONSUL_ENABLED":   "true",
				"CONSUL_HTTP_ADDR": "https://consul:8501",
				"CONSUL_KV_PREFIX": "pdf-extractext/dev/orchestrator/",
			},
			missing: "CONSUL_HTTP_TOKEN",
		},
		{
			name: "prefix",
			values: map[string]string{
				"CONSUL_ENABLED":    "true",
				"CONSUL_HTTP_ADDR":  "https://consul:8501",
				"CONSUL_HTTP_TOKEN": "token",
			},
			missing: "CONSUL_KV_PREFIX",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := LoadConsulSettings(envWith(tc.values))
			if err == nil {
				t.Fatalf("LoadConsulSettings() expected an error missing %s", tc.missing)
			}
			if !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("error = %q, want it to name %s", err, tc.missing)
			}
		})
	}
}

func TestLoadConsulSettingsParsesTypedValues(t *testing.T) {
	t.Parallel()

	settings, err := LoadConsulSettings(envWith(map[string]string{
		"CONSUL_ENABLED":           "TRUE",
		"CONSUL_HTTP_ADDR":         " https://consul:8501 ",
		"CONSUL_HTTP_TOKEN":        " token ",
		"CONSUL_HTTP_CA_FILE":      " /certs/ca.pem ",
		"CONSUL_KV_PREFIX":         " pdf-extractext/dev/orchestrator/ ",
		"CONSUL_SERVICE_ADDRESS":   " orchestrator-1 ",
		"CONSUL_SERVICE_NAME":      " orchestrator ",
		"CONSUL_SERVICE_ID":        " orchestrator-1 ",
		"CONSUL_SERVICE_CHECK_URL": " http://orchestrator-1:8080/healthz ",
		"CONSUL_SERVICE_TAGS":      " security-headers@file,api-ratelimit@file; tls ",
	}))
	if err != nil {
		t.Fatalf("LoadConsulSettings() unexpected error: %v", err)
	}

	if !settings.Enabled {
		t.Error("Enabled = false, want true")
	}
	if settings.HTTPAddr != "https://consul:8501" {
		t.Errorf("HTTPAddr = %q", settings.HTTPAddr)
	}
	if settings.HTTPToken != "token" {
		t.Errorf("HTTPToken = %q", settings.HTTPToken)
	}
	if settings.HTTPCAFile != "/certs/ca.pem" {
		t.Errorf("HTTPCAFile = %q", settings.HTTPCAFile)
	}
	if settings.KVPrefix != "pdf-extractext/dev/orchestrator/" {
		t.Errorf("KVPrefix = %q", settings.KVPrefix)
	}
	if settings.ServiceAddress != "orchestrator-1" {
		t.Errorf("ServiceAddress = %q", settings.ServiceAddress)
	}
	if settings.ServiceName != "orchestrator" {
		t.Errorf("ServiceName = %q", settings.ServiceName)
	}
	if settings.ServiceID != "orchestrator-1" {
		t.Errorf("ServiceID = %q", settings.ServiceID)
	}
	if settings.CheckURL != "http://orchestrator-1:8080/healthz" {
		t.Errorf("CheckURL = %q", settings.CheckURL)
	}
	wantTags := []string{"security-headers@file,api-ratelimit@file", "tls"}
	if !reflect.DeepEqual(settings.Tags, wantTags) {
		t.Errorf("Tags = %v, want %v", settings.Tags, wantTags)
	}
}
