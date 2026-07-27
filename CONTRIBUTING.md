# Contributing

This repository is intended to remain a small, stable Go library with production-oriented self-hosted deployment references. Changes must preserve opt-in profiling, fail-open application control, bounded label cardinality, and a private-by-default network design.

## Development environment

Use the Go version pinned in `go.mod` and CI. Do not require a live Pyroscope server, Kubernetes cluster, object store, or credentials in unit tests.

Run the local quality gates before opening a pull request:

```sh
gofmt -w .
go mod tidy
go mod verify
go vet ./...
go test -race -shuffle=on -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
govulncheck ./...
```

Total statement coverage must remain at or above 80%. Tests must use an injected starter or an isolated test server and must not send profiles outside the test process.

## Pull requests

Keep changes focused and explain their application-overhead, ingestion, storage, query, security, compatibility, and license consequences. A pull request that changes exported behavior or environment variables must include tests, `.env.example`, user documentation, migration notes, and an `Unreleased` changelog entry.

Dependency changes must include `go.mod` and `go.sum`, pass dependency review, and record license/provenance changes. Deployment changes must pin chart and image versions, preserve secret references rather than literal secrets, pass rendering and policy checks, and update the handoff/runbook when operator behavior changes.

Module-path changes must use `GOWORK=off go run ./cmd/set-module-path -- <new-module-path>` rather than editing `go.mod` alone. Disabling workspace mode keeps the migration independent from an enclosing corporate `go.work`. The command migrates internal Go imports, verifies the normal and `smoke` build-tag test paths, and rolls back on failure. CI repeats this operation in a copied checkout with an example corporate path so the bootstrap path cannot silently diverge from `cmd/pyroscope-smoke`.

Changes that enable an additional profile type, alter sampling/upload behavior, or add labels must provide representative benchmarks or canary evidence for:

- application CPU, memory, latency, error rate, and restarts;
- profile ingestion rate and failures;
- Pyroscope CPU, memory, storage growth, compaction, and query latency;
- label-cardinality impact.

Never introduce request IDs, user IDs, session IDs, pod UIDs, raw URLs, tokens, or other unbounded/sensitive values as profile labels.

Use [Semantic Versioning](https://semver.org/) for releases. Breaking environment-variable or exported-API changes require a major version after `v1.0.0`. Move completed entries from `Unreleased` to a dated version section when tagging.

Do not merge directly to the default branch. Repository administrators must require the CI and security jobs supplied in `.github/workflows` through a branch ruleset. Configure the real owning team in `CODEOWNERS` before the first production release; this package intentionally does not invent a company team handle.

## Architecture rules

- Keep the Go SDK in push mode; do not expose an HTTP `pprof` handler as part of this wrapper.
- Do not add a public-cloud profiling API or provider-specific runtime identity to the baseline.
- Keep profiling disabled by default and make duplicate initialization visible.
- Production Pyroscope must remain behind an authenticated TLS gateway.
- Keep object storage S3-compatible and site-configurable; do not embed access keys.
- Treat single-binary/local-disk deployment as evaluation only.
- Do not adopt OpenTelemetry Profiles as the production transport while its required signal path remains Alpha without a separately approved architecture decision.
- Keep AGPL-covered server code separate from this Apache-2.0 wrapper and preserve license/SBOM evidence.

See [Architecture and design decisions](docs/ARCHITECTURE.md), [Security and compliance controls](docs/SECURITY-AND-COMPLIANCE.md), and [Senior DevOps handoff](docs/HANDOFF.md).

## Security

Do not report suspected vulnerabilities in public issues. Follow [SECURITY.md](SECURITY.md).
