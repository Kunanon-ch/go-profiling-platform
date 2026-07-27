# Third-party components

This repository's own source code is distributed under the Apache License 2.0
in `LICENSE`. The receiving organization must complete its normal open-source
and dependency-license review before the first internal production release.

## Linked Go dependencies

| Component | Pinned version | License | Use |
| --- | --- | --- | --- |
| `github.com/grafana/pyroscope-go` | `v1.4.1` | Apache-2.0 | In-process Go continuous-profiling client. |
| `github.com/grafana/pyroscope-go/godeltaprof` | `v0.1.11` | Apache-2.0 | Delta heap/profile encoding linked through the client. |
| `github.com/klauspost/compress` | `v1.18.6` | BSD-3-Clause | Compression implementation linked through the client. |

`make vuln`/`make verify` invokes `golang.org/x/vuln/cmd/govulncheck@v1.6.0`
(BSD-3-Clause) as a CI/release tool. It is not linked into the library and is
therefore not recorded in `go.mod`; its own build-time module graph must be
mirrored and reviewed separately, or the company must preinstall an approved
binary at that exact version and run the Make gate with
`GOVULNCHECK=govulncheck`.

The complete resolved Go dependency graph and integrity hashes are recorded in
`go.mod` and `go.sum`. Generate and archive the company's approved SBOM and
license report for the exact release commit and consuming application image.

## Deployment references

These components are referenced by deployment configuration; their binaries
or source code are not copied into this repository.

| Component | Pinned version or digest | Primary upstream license | Use |
| --- | --- | --- | --- |
| Grafana Pyroscope server | `2.2.0`; image manifest `sha256:7f019d866e474609c7f112d2632d75bdf8ad418c08461fcb5193444bb6dc476d` | AGPL-3.0 | Self-hosted profile ingestion, storage, query, and visualization backend. |
| Grafana Pyroscope Helm chart | `2.2.0`; chart SHA-256 `7fbbdce70b105aae0c57c48658ae19537396eef955e9eec480ce41e14a82b675` | Apache-2.0 | Kubernetes deployment reference. |
| Grafana | `13.1.0`; image manifest `sha256:121a7a9ece6dc10b969f1f96eed64b4f07dfac0d0b8abc070f7cb83bbde86f63` | AGPL-3.0 | Optional UI used only by the local Compose evaluation. |
| NGINX Unprivileged image | `1.29.5-alpine`; image manifest `sha256:42a7d7f2ee23e9f5a1dcdf3647ba5c585bbd18f79e79cd817e70e8cd61c55779` | Apache-2.0 image packaging; NGINX is BSD-2-Clause | Loopback publishing gateway used only by the local Compose evaluation. |
| Docker Official Go image | `1.25.12-bookworm`; image manifest `sha256:ea341baa9bd5ba6784f6d7161ace70544349a6242d54d34a0fbfd2c4d51c9d58` | BSD-3-Clause image packaging/Go toolchain | Build stage for the local smoke binary; absent from the final `scratch` runtime image. |
| kubeconform | `v0.7.0`; Linux AMD64 archive SHA-256 `c31518ddd122663b3f3aa874cfe8178cb0988de944f29c74a0b9260920d115d3` | Apache-2.0 | CI-only Kubernetes schema validation tool. |

AGPL-3.0 obligations and company policy must be reviewed by the legal or
open-source program owner before Pyroscope is deployed. Pinning a version and
digest does not replace image scanning, signature/provenance verification, or
license approval. Container base distributions and bundled packages can have
additional licenses; use the exact-image SBOM as the authoritative review
input. Preinstall or internally distribute the approved Go 1.25.12 toolchain,
and mirror approved modules, images, and charts into internal registries for
controlled or air-gapped environments. Set `GOTOOLCHAIN=local` on runners that
must not use Go's automatic public toolchain download.

Project and license references:

- <https://github.com/grafana/pyroscope>
- <https://github.com/grafana/pyroscope-go>
- <https://github.com/grafana/pyroscope-go/tree/main/godeltaprof>
- <https://github.com/klauspost/compress>
- <https://github.com/grafana/helm-charts>
- <https://github.com/grafana/grafana>
- <https://github.com/nginx/docker-nginx-unprivileged>
- <https://github.com/nginx/nginx>
- <https://github.com/docker-library/golang>
- <https://github.com/yannh/kubeconform>
