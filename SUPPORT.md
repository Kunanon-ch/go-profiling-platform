# Support

## Scope

This repository provides a Go bootstrap library and reference deployment assets for a self-hosted Grafana Pyroscope OSS stack.

Library support covers configuration parsing, validation, startup behavior, enabled profile types, compatibility with the pinned Go toolchain, and defects reproducible in this repository. Platform support covers the company-owned adaptation of the reference Helm/Kubernetes assets, gateway, storage, monitoring, and runbook.

The receiving organization remains responsible for:

- Kubernetes or VM capacity and availability;
- internal DNS, TLS, gateway authentication, authorization, and network policy;
- S3-compatible storage, persistent volumes, retention, backup, and restore;
- Grafana access and data-source configuration;
- application deployment, SLOs, incident response, and change approval;
- Pyroscope server operations and upstream support decisions;
- security, privacy, and open-source license approval.

This package does not include a managed service or an availability warranty.

## Getting help

For reproducible defects and feature requests, use the corporate repository issue tracker with secrets and company-specific identifiers removed. Include:

- wrapper version and commit;
- `pyroscope-go`, Pyroscope server, Helm chart, and image versions;
- Go version, operating system, and architecture;
- deployment mode and redacted configuration;
- enabled profile types;
- expected and actual behavior;
- sanitized logs and a minimal reproduction;
- whether the issue affects ingestion, storage, query, Grafana, or the application runtime.

Do not attach raw production profiles unless the approved support channel is authorized for that data classification. For vulnerabilities, follow [SECURITY.md](SECURITY.md) and do not open a public issue.

## Ownership required before production

This repository does not know the receiving company's team names or escalation channels. Before the first production rollout, the repository and platform administrators must:

1. assign a library owner, platform owner, storage owner, security/data owner, and backup contacts;
2. configure the real owning team in `CODEOWNERS` and require its review;
3. configure monitored support, incident, and security-escalation channels;
4. enable branch protection for the checks listed in [CONTRIBUTING.md](CONTRIBUTING.md);
5. enable dependency alerts, automated updates, private vulnerability reporting, image scanning, and secret scanning;
6. approve the Apache-2.0 and AGPL-3.0 inventory in [THIRD_PARTY-NOTICES.md](THIRD_PARTY-NOTICES.md) with Legal or the Open Source Program Office;
7. define release, patch, deprecation, backup, retention, and disaster-recovery policies;
8. define who can view, export, or share profiles and who can change tenant routing;
9. establish upstream Pyroscope upgrade and incident ownership.

These controls cannot be safely replaced with fabricated team handles or addresses.

## CI assumptions

The supplied workflows target GitHub-hosted Linux runners and pin a Go toolchain plus immutable third-party Action commits. A self-hosted runner or GitHub Enterprise installation must provide:

- a compatible Actions runner and C compiler for `go test -race`;
- the approved Go 1.25.13 toolchain preinstalled or supplied through an internal distribution path, with `GOTOOLCHAIN=local` when automatic public toolchain downloads are prohibited;
- access to approved Go module and vulnerability-database mirrors, including either a preinstalled `govulncheck v1.6.0` binary selected with `GOVULNCHECK=govulncheck` or the separately mirrored `golang.org/x/vuln` tool module graph used by the default Make target;
- the pinned Actions in its allowlist;
- dependency-graph and dependency-review capabilities;
- an isolated path for container and Helm scanning if those checks are added to the corporate pipeline.

Library CI does not connect to a live Pyroscope server. Platform validation, a rendered-manifest policy check, an evaluation deployment, representative-load canary, restore exercise, and rollback rehearsal remain separate release gates.
