.DEFAULT_GOAL := test

GO ?= go
GOFMT ?= gofmt
COVERAGE_FILE ?= coverage.out
GOVULNCHECK_VERSION ?= v1.6.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

UNFORMATTED = $(strip $(shell $(GOFMT) -l .))

.PHONY: fmt-check tidy-check module-verify vet test smoke-build race coverage vuln compose-config verify

fmt-check:
	$(if $(UNFORMATTED),$(error Unformatted Go files: $(UNFORMATTED)),@echo "Go formatting is clean")

tidy-check:
	$(GO) mod tidy -diff

module-verify:
	$(GO) mod verify

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

smoke-build:
	$(GO) test -tags smoke ./cmd/pyroscope-smoke

race:
	$(GO) test -race -shuffle=on ./...

coverage:
	$(GO) test -covermode atomic -coverprofile $(COVERAGE_FILE) ./...
	$(GO) tool cover -func $(COVERAGE_FILE)

vuln:
	$(GOVULNCHECK) ./...

compose-config:
	GRAFANA_ADMIN_PASSWORD=compose-validation-only docker compose -f deploy/docker-compose/compose.yaml --profile smoke config --quiet

verify: fmt-check tidy-check module-verify vet smoke-build race coverage vuln
