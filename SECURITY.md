# Security policy

## Supported versions

Until the first tagged release, only the current default branch receives security fixes. Unversioned copies and locally modified forks are not supported by this repository.

Before the first production release, the owning organization must document its supported release window, patch SLA, and deprecation policy. Consumers must pin a reviewed semantic release tag rather than a branch or pseudo-version.

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public issue, discussion, pull request, or chat channel.

Use GitHub private vulnerability reporting from the repository's **Security** tab. A repository administrator must verify that it is enabled and monitored before the first release. If company policy requires a different intake system, replace this section with the approved channel; this package intentionally does not invent a company address.

Include, when available:

- the affected version or commit;
- impact and prerequisites;
- minimal reproduction steps without production data or credentials;
- a suggested mitigation;
- whether the issue is already public.

The security owner should acknowledge a report within two business days and provide an initial severity assessment within five business days. These are response targets, not a resolution guarantee.

## Handling reports

Limit access to the people required for triage and remediation. Reproduce the issue in an isolated environment, agree on a disclosure timeline with the reporter, and publish a security advisory plus a patched release when remediation is available.

Never attach profile payloads, object-store credentials, gateway tokens, unrestricted environment dumps, or production configuration to an issue. Sanitize application names, internal endpoints, package paths, tenant IDs, and stack traces according to company policy.

## Release security gates

Every release must:

1. pass `go test -race`, `go vet`, module verification, and `govulncheck` for the exact commit;
2. review direct and transitive dependency changes, checksums, provenance, and licenses;
3. generate and archive an SBOM for the Go module, consuming application image, Pyroscope server image, Helm chart, gateway, and any bundled Grafana components;
4. scan container images and deployment manifests under company policy;
5. pin approved Go modules, Helm charts, Actions, and container image digests;
6. document any reachable, imported-package, image, or module-only finding and its owner, expiry, and mitigation;
7. obtain Open Source Program Office or Legal approval for the license boundary described below.

Do not suppress a scanner finding globally. An exception must be scoped, time-limited, evidence-backed, and re-evaluated whenever the dependency graph, call graph, container image, or deployment topology changes.

## Current library scan baseline

On 2026-07-24, `govulncheck v1.6.0` reported no vulnerabilities in the current Go package, `go mod verify` passed, and the resolved module graph no longer contained Google Cloud libraries. This is point-in-time evidence for the untagged handoff source, not a permanent guarantee. Re-run the full release gates for the exact corporate commit/tag and scan every consuming/deployed container separately.

## License and source-availability boundary

This repository is Apache License 2.0. The embedded `github.com/grafana/pyroscope-go` client is also Apache License 2.0. The separately deployed Grafana Pyroscope server is AGPL-3.0. Exact pinned components are recorded in [THIRD_PARTY-NOTICES.md](THIRD_PARTY-NOTICES.md).

The organization must have Legal or its Open Source Program Office review the exact versions and planned use. In particular, modifications to an AGPL-covered network service can create source-availability obligations for users interacting with that modified service. This document is an engineering control, not legal advice. Preserve upstream licenses and notices, retain corresponding source and build provenance as required by the approved policy, and do not copy server code into this Apache-licensed wrapper.

## Runtime security boundary

The wrapper uses in-process push profiling and does not register an HTTP `pprof` endpoint. Pyroscope OSS does not provide its own authentication layer, so production deployments must place an authenticating and authorizing TLS gateway in front of all ingestion and query endpoints. Direct backend access must be denied by network policy and infrastructure controls.

Credentials must come from the company secret manager or an approved workload-secret delivery mechanism. Never commit, bake into images, write into ordinary Helm values, or log:

- gateway passwords, bearer tokens, or client certificates;
- object-store access keys;
- TLS private keys;
- Grafana data-source credentials.

Use dedicated, least-privilege identities and rotate them through the owned secret system. The supplied server values keep `auth.multitenancy-enabled=false`. Enable it only after the trusted gateway authenticates and authorizes every writer and reader, removes any caller-supplied tenant header, and sets its own `X-Scope-OrgID`. A release gate must prove missing/spoofed headers are denied and tenant A cannot write to or query tenant B; never rely on a client-supplied tenant header by itself.

Static `PYROSCOPE_TAGS` validation does not inspect labels introduced elsewhere in the process. Before enabling profiling, inventory `runtime/pprof.Labels`, `runtime/pprof.Do`, `runtime/pprof.WithLabels`, and `pyroscope.TagWrapper` call sites; reject sensitive or unbounded values and verify the actual exported canary labels under the data policy.

The dedicated upload client rejects redirects, ignores process proxy variables, caps response headers at 32 KiB, and caps decoded response bodies at 64 KiB. The body boundary applies after automatic gzip decompression and also covers chunked or otherwise unknown-length error responses, preventing the upstream SDK's response read from becoming an unbounded application-memory allocation.

See [Security and compliance controls](docs/SECURITY-AND-COMPLIANCE.md) for the full production checklist.
