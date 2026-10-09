# ValidationMicroservices-pdf-extractext — orquestador pdf-extractext.
#
# Atajos para compilar, levantar (local o docker) y probar el microservicio.
#
#   make            # ayuda
#   make run        # levanta local con la config de abajo
#   make smoke      # levanta local, espera /healthz y corre pruebas de humo
#   make up         # levanta con docker compose
#   make test       # vet + tests

SHELL := /bin/bash

BINARY  := bin/orchestrator
CMD     := ./cmd/orchestrator
CORPUS  ?= tests/stress/pdfs/2020-Scrum-Guide-Spanish-Latin-South-American.pdf
LOGFILE ?= bin/orchestrator.log

# Config local. Override desde la línea de comandos, p. ej. `make run PORT=9000`.
# Los tokens no tienen default en config.Config (a propósito): acá se dan valores
# de desarrollo para que `make run`/`make smoke` arranquen sin exportar nada.
PORT                  ?= 8099
EXTRACT_BASE_URL      ?= http://127.0.0.1:8080
PERSISTENCE_BASE_URL  ?= http://127.0.0.1:8000
AUDIT_LOG_BASE_URL    ?= http://127.0.0.1:8083
AUDIT_LOG_API_TOKEN   ?= audit-secret
PERSISTENCE_API_TOKEN ?= persistence-secret
HTTP_TIMEOUT          ?= 35s
MAX_PDF_PAGES         ?= 1000

BASE_URL := http://127.0.0.1:$(PORT)

# Config exportada al binario.
APP_ENV := \
	PORT=$(PORT) \
	EXTRACT_BASE_URL=$(EXTRACT_BASE_URL) \
	PERSISTENCE_BASE_URL=$(PERSISTENCE_BASE_URL) \
	AUDIT_LOG_BASE_URL=$(AUDIT_LOG_BASE_URL) \
	AUDIT_LOG_API_TOKEN=$(AUDIT_LOG_API_TOKEN) \
	PERSISTENCE_API_TOKEN=$(PERSISTENCE_API_TOKEN) \
	HTTP_TIMEOUT=$(HTTP_TIMEOUT) \
	MAX_PDF_PAGES=$(MAX_PDF_PAGES)

.DEFAULT_GOAL := help

.PHONY: help build run vet fmt test test-race cover smoke up down logs ps clean

help: ## Muestra esta ayuda
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Compila el binario en bin/orchestrator
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o $(BINARY) $(CMD)

run: ## Levanta el microservicio en local (PORT=$(PORT))
	$(APP_ENV) go run $(CMD)

vet: ## Analiza el código con go vet
	go vet ./...

fmt: ## Formatea el código con gofmt
	gofmt -l -w .

test: ## go vet + tests
	go vet ./...
	go test -count=1 ./...

test-race: ## Tests con el detector de carreras (como CI)
	go test -race -count=1 ./...

cover: ## Cobertura de tests
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

smoke: build ## Levanta el ms local y lo prueba: healthz + validaciones (+ extract real si el Extract responde)
	@mkdir -p bin
	@set -uo pipefail; \
	$(APP_ENV) $(BINARY) >$(LOGFILE) 2>&1 & \
	pid=$$!; \
	stop() { kill $$pid 2>/dev/null || true; wait $$pid 2>/dev/null || true; }; \
	trap stop EXIT; \
	printf 'esperando a %s/healthz ' '$(BASE_URL)'; \
	up=0; \
	for _ in $$(seq 1 50); do \
	  if curl -fsS -o /dev/null '$(BASE_URL)/healthz'; then up=1; printf 'ok\n'; break; fi; \
	  printf '.'; sleep 0.2; \
	done; \
	if [ $$up -ne 1 ]; then printf '\nERROR: el servicio no respondio. Log:\n'; cat $(LOGFILE); exit 1; fi; \
	pass=0; fail=0; \
	check() { \
	  desc="$$1"; want="$$2"; shift 2; \
	  got=$$(curl -s -o /dev/null -w '%{http_code}' "$$@" || echo 000); \
	  if [ "$$got" = "$$want" ]; then \
	    printf '  \033[32mok\033[0m   %-38s %s\n' "$$desc" "$$got"; pass=$$((pass+1)); \
	  else \
	    printf '  \033[31mFAIL\033[0m %-38s got=%s want=%s\n' "$$desc" "$$got" "$$want"; fail=$$((fail+1)); \
	  fi; \
	}; \
	echo '--- validaciones (no requieren el Extract) ---'; \
	printf 'generando PDF cifrado   ... '; go run ./scripts/encryptpdf "$(CORPUS)" bin/smoke-encrypted.pdf || { printf 'FAIL\n'; exit 1; }; printf 'ok\n'; \
	check 'sin firma PDF           -> 400' 400 -X POST -H 'Content-Type: application/pdf' --data-binary 'no soy un pdf' '$(BASE_URL)/api/v1/pdfs/extract'; \
	check 'PDF corrupto            -> 400' 400 -X POST -H 'Content-Type: application/pdf' --data-binary '%PDF-1.7 basura' '$(BASE_URL)/api/v1/pdfs/extract'; \
	check 'extension no permitida  -> 415' 415 -X POST -H 'Content-Type: application/pdf' -H 'X-Filename: informe.exe' --data-binary '%PDF-1.7' '$(BASE_URL)/api/v1/pdfs/extract'; \
	check 'PDF cifrado             -> 422' 422 -X POST -H 'Content-Type: application/pdf' --data-binary @bin/smoke-encrypted.pdf '$(BASE_URL)/api/v1/pdfs/extract'; \
	echo '--- extraccion real (requiere el Extract) ---'; \
	code=$$(curl -s -o bin/smoke-extract.json -w '%{http_code}' -X POST -H 'Content-Type: application/pdf' --data-binary @"$(CORPUS)" '$(BASE_URL)/api/v1/pdfs/extract' || echo 000); \
	if [ "$$code" = "200" ]; then \
	  want=$$(sha256sum "$(CORPUS)" | cut -d' ' -f1); \
	  got=$$(grep -o '"checksum":"[0-9a-f]*"' bin/smoke-extract.json | head -1 | cut -d'"' -f4); \
	  if [ "$$got" = "$$want" ]; then \
	    printf '  \033[32mok\033[0m   checksum == SHA-256(bytes) %s\n' "$$got"; pass=$$((pass+1)); \
	  else \
	    printf '  \033[31mFAIL\033[0m checksum got=%s want=%s\n' "$$got" "$$want"; fail=$$((fail+1)); \
	  fi; \
	else \
	  printf '  \033[33mskip\033[0m Extraccion no ejecutada (HTTP %s). Levanta el Extract en %s.\n' "$$code" '$(EXTRACT_BASE_URL)'; \
	fi; \
	echo; \
	printf 'Resultado: %d ok, %d fail\n' $$pass $$fail; \
	[ "$$fail" -eq 0 ]

up: ## Levanta el stack con docker compose
	docker compose up -d --build

down: ## Baja el stack de docker compose
	docker compose down

logs: ## Sigue los logs del stack
	docker compose logs -f

ps: ## Estado de los contenedores del stack
	docker compose ps

clean: ## Borra bin/, logs y cobertura
	rm -rf bin coverage.out
