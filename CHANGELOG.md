# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Self-hosted Grafana Pyroscope OSS integration through the Apache-2.0 Go push SDK.
- Disabled-by-default environment configuration for the internal Pyroscope endpoint, logical application identity, immutable release/environment labels, authentication references, upload interval, and profile-type selection.
- Graceful, concurrent-safe, idempotent `Stop()` behavior with synchronous final flush, bounded direct HTTP requests, and runtime sampling cleanup.
- CPU, allocation, in-use heap, and goroutine profiling defaults, with explicit opt-in controls for higher-overhead mutex and block profiles.
- Evaluation and production deployment references, including a single-binary evaluation path and an HA production path with internal S3-compatible object storage.
- Architecture, Senior DevOps handoff, operations runbook, security/compliance, support, contribution, canary, rollback, storage, and license guidance.
- CI quality gates, Linux cross-builds, race tests, vulnerability scanning, dependency review, and dependency-update automation.
- A verified module-path migration command that updates internal imports, tests the build-tagged smoke command, repairs a prior `go.mod`-only edit, and rolls back on failure.

### Changed

- Replaced the Google Cloud Profiler-specific bootstrap and operational model with a cloud-provider-independent Pyroscope OSS design.
- Replaced Google Cloud Project, Profiler API, IAM, GSA, KSA-to-GSA, and Google ADC assumptions with internal endpoint, gateway, TLS, tenant, secret-delivery, and storage controls.
- Kept the integration in-process and push-based so it does not register or expose an HTTP `pprof` endpoint.
- Separated evaluation topology from the production HA/object-storage topology.
- Tightened configuration validation, URL safety, HTTPS-by-default policy, duplicate-start handling, profile-type validation, bounded labels/tenant IDs, and low-cardinality identity guidance.
- Rejected the presence of upstream `PYROSCOPE_ADHOC_SERVER_ADDRESS`, including an explicitly empty value, so it cannot bypass the validated destination.

### Removed

- Google Cloud Profiler client dependency and Cloud-specific environment variables.
- GCP Terraform resources, API enablement, IAM roles, Workload Identity bindings, and cloud-provider credentials.

### Security

- Updated the Go toolchain and smoke builder to 1.25.13 and `github.com/klauspost/compress` to v1.18.7 to address the September 30 scheduled vulnerability scan findings.
- Defined an authenticated TLS gateway as mandatory for production because Pyroscope OSS does not provide its own authentication layer.
- Prohibited literal application, gateway, object-store, and TLS secrets in source, images, manifests, and ordinary Helm values.
- Documented profile-data classification, label restrictions, tenant-header trust, network isolation, retention, backup, and restore controls.
- Bounded upload responses after decompression (32 KiB headers and 64 KiB body), including chunked and gzip responses, so the upstream SDK cannot read an unbounded gateway error into application memory.
- Added a required inventory and canary inspection for labels introduced through Go runtime/pprof contexts or `pyroscope.TagWrapper`, which do not pass through static `PYROSCOPE_TAGS` validation.
- Documented the Apache-2.0 wrapper/client and AGPL-3.0 Pyroscope server license boundary and made Legal/OSPO approval plus SBOM evidence release gates.
- Recorded OpenTelemetry Profiles as Alpha and not selected for the production path.
- Removed the previous Google Cloud dependency graph; the 2026-07-24 `govulncheck v1.6.0` baseline reports no Go vulnerabilities.
