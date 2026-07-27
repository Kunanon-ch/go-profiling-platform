# Release validation evidence

This file records the maintainer-side checks completed for the untagged
handoff source on 2026-07-24. It is evidence for this package snapshot, not a
substitute for the receiving company's CI, image scanning, policy checks,
representative-load canary, security review, or production approval.

## Go library

The following checks passed with Go 1.25.12:

- `gofmt` produced no changed-file list;
- `go mod tidy -diff` produced no module change;
- `go mod verify` verified every downloaded module;
- `go vet ./...` passed;
- `go test ./...` and the smoke-tag compile passed;
- the module-path migration command changed a copied tree to
  `git.company.example/platform/profilerprobe`, updated the smoke import, and
  passed its module/checksum/ordinary/smoke verification without changing the
  source tree;
- atomic statement coverage was 98.0% for the profiler library and 87.8%
  across the complete repository including the module-path migration command;
- `go test -race -shuffle=on ./...` passed in the pinned Linux Go builder;
- Linux `amd64` and `arm64` test/smoke binaries compiled;
- `govulncheck v1.6.0 ./...` reported no reachable vulnerability.

The module graph contains the Pyroscope Go client and its two resolved
transitive modules; it contains no Google Cloud client module.

## Deployment references

- Docker Compose model validation and the pinned smoke image build passed.
- Both pinned Pyroscope 2.2.0 Helm value files passed `helm lint` and rendered.
- The render contained one namespace-scoped ServiceAccount, Role, and
  RoleBinding and no ClusterRole or ClusterRoleBinding.
- All nine HA workloads rendered hard hostname spread and best-effort zone
  spread constraints.
- Kubeconform v0.7.0 validated 64 rendered Helm and standalone reference
  resources against the declared Kubernetes 1.30 schema: 64 valid, zero
  invalid, zero errors, zero skipped. This is a minimum API-compatibility check,
  not a recommendation to operate an unsupported Kubernetes release; the
  receiving company must repeat validation for its supported target version.
- The application Deployment files are strategic-merge fragments, so they are
  intentionally excluded from standalone schema validation. The receiving
  team must validate the fully merged workload with its GitOps renderer,
  policy engine, and Kubernetes server-side dry run.
- GitHub Actions workflow syntax passed actionlint.

## Fresh-volume end-to-end smoke test

The loopback-only Compose stack was recreated with empty Pyroscope and Grafana
volumes and the pinned image digests. The following checks passed:

| Check | Observed result |
| --- | --- |
| Pyroscope gateway readiness | HTTP 200 from `/ready` |
| Grafana health | database `ok`, version `13.1.0` |
| Provisioned Pyroscope data source | `status: OK`, `Data source is working` |
| Uploaded service discovery | `profilerprobe-smoke` returned by `LabelValues` |
| CPU render | HTTP 200 |
| Recognizable workload frames | `main.cpuWorker`, `main.burnCPU`, `main.mix`, `main.allocateBuffer` |
| Graceful smoke shutdown | final-flush log present, exit code 0 |

Runtime inspection also confirmed that the smoke container runs as
`65532:65532`, has a read-only root filesystem, drops all capabilities, uses
`no-new-privileges`, and is attached only to the internal profiling network.
Only the unprivileged local gateway publishes ports, bound to `127.0.0.1`.

## Checks the receiving company still owns

Before production, repeat every release gate for the final corporate commit,
module path, application image, internal registry copies, chosen gateway,
storage implementation, and complete rendered manifests. Then complete load
and overhead measurement, access/tenant negative tests, credential rotation,
backup/restore, failure and rollback rehearsals, vulnerability/SBOM/provenance
review, and Legal/OSPO approval. See [Senior DevOps handoff](HANDOFF.md) for the
site inputs and Definition of Done.
