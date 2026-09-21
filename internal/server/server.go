package server

import (
	"log/slog"
	"net/http"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
)

func New(cfg config.Config, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           Routes(cfg, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
