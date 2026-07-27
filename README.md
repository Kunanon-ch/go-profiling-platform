# Self-hosted continuous profiling for Go

This repository is a production-oriented bootstrap and Senior DevOps handoff package for profiling Go applications with Grafana Pyroscope OSS hosted inside the company.

It removes the Google Cloud Profiler dependency. Runtime operation does not require a Google Cloud, AWS, Azure, or other cloud-provider account/API. The company must still provide internal CPU, memory, network, DNS, TLS, Kubernetes or VM capacity, persistent storage, S3-compatible object storage, Grafana, monitoring, backups, and operators.

The selected design uses the Apache-2.0 Pyroscope Go push SDK inside each application. It captures Go profiles without registering or exposing an HTTP `pprof` endpoint and sends them through an authenticated internal HTTPS gateway to a self-hosted Pyroscope backend.

## Package status

The reusable code, tests, CI/security policy, deployment references, runbook, and handoff checklist are ready for company adoption. Production approval still requires corporate repository values, an internally hosted platform, real owners, representative-load canary evidence, security/data approval, and Legal/Open Source Program Office review.

Current verification passes formatting, module integrity/tidiness, vet, unit tests, Linux race tests, Linux `amd64`/`arm64` compilation, and `govulncheck`; statement coverage is 98.0% for the profiler library and 87.8% across the complete repository including the migration command. These results validate the package, not a future consuming application image or production platform.

Start with:

| Artifact | Purpose |
| --- | --- |
| [Senior DevOps handoff](docs/HANDOFF.md) | Site inputs, implementation sequence, evidence, and Definition of Done. |
| [Architecture](docs/ARCHITECTURE.md) | Technical choice, topology, evaluation/production split, storage, security, OTel, and licenses. |
| [Operations runbook](docs/RUNBOOK.md) | Deploy, enable, observe, stop, troubleshoot, roll back, restore, and respond. |
| [Security and compliance](docs/SECURITY-AND-COMPLIANCE.md) | Data, identity, TLS, secrets, network, storage, supply chain, audit, and approvals. |
| [Release validation evidence](docs/VALIDATION.md) | Commands and observed results for the handed-off source and fresh-volume smoke test. |
| [Third-party notices](THIRD_PARTY-NOTICES.md) | Pinned Pyroscope client/server/chart versions and license boundary. |
| [Deployment overview](deploy/README.md) | Routes operators to the local evaluation, Helm, and Kubernetes assets. |
| [Docker Compose evaluation](deploy/docker-compose/README.md) | Loopback-only Pyroscope/Grafana stack and real smoke profile. |
| [Helm deployment](deploy/helm/README.md) | Pinned single-binary evaluation and production HA/S3-compatible examples. |
| [Kubernetes integration](deploy/kubernetes/README.md) | Disabled application patch, secret overlay, service accounts, and NetworkPolicies. |

## How it works

```text
Go application
  -> profilerprobe.StartFromEnv()
  -> Pyroscope Go SDK (push; no HTTP listener)
  -> internal authenticated HTTPS gateway
  -> self-hosted Pyroscope OSS
  -> internal S3-compatible object storage

Authorized operator
  -> company SSO / Grafana
  -> authenticated query gateway
  -> flame graph, profile comparison, and drilldown
```

The Go module is an **in-process library**, not a sidecar, daemon, node agent, or standalone workload. It can profile only the process into which it is compiled. Each consuming service must import a released module version, call it once during startup, and release a new binary/image.

## Why this path

- No runtime cloud-provider profiling API, project, IAM role, or provider credential.
- Full Go-focused CPU, allocation/heap, goroutine, mutex, and block profile selection.
- No public `pprof` handler and no privileged pod, host PID access, kernel capability, or eBPF node agent.
- Safe opt-in configuration, HTTPS enforcement, bounded labels, secret-file support, duplicate-start protection, and graceful stop.
- Production components can scale independently and store blocks in an internal S3-compatible system.
- Existing self-managed Grafana can query Pyroscope and display flame graphs.

The tradeoff is operational ownership: self-hosting avoids provider lock-in but transfers availability, capacity, upgrades, security, storage, backup, recovery, and incident response to the company.

## Evaluation and production are different

### Evaluation

Use the supplied single-binary deployment to validate integration and overhead in an isolated non-production environment. It can use one persistent volume and has a single failure domain. Insecure HTTP is allowed only when explicitly enabled for an isolated local/Compose-style test.

Evaluation is not an HA, durability, scale, backup, or disaster-recovery proof and must not be promoted unchanged to production.

The quickest local proof is:

```powershell
cd deploy\docker-compose
Copy-Item .env.example .env
# Set a unique GRAFANA_ADMIN_PASSWORD in .env.
docker compose config --quiet
docker compose pull
docker compose up -d pyroscope grafana gateway
$ready = $false
for ($attempt = 1; $attempt -le 24; $attempt++) {
    curl.exe --fail --silent http://127.0.0.1:4040/ready
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Seconds 5
}
if (-not $ready) { docker compose logs pyroscope gateway; throw "Pyroscope did not become ready" }
docker compose --profile smoke up --build --no-deps smoke
```

Open `http://127.0.0.1:3000`, sign in with the local evaluation credentials, open **Drilldown > Profiles**, and select `profilerprobe-smoke`. This Compose stack is loopback-only, unauthenticated behind that local boundary, and never a production topology. Full cleanup and persistence behavior are in the [Compose guide](deploy/docker-compose/README.md).

### Production

Use the pinned official Pyroscope Helm chart in the topology supported for production by that release. The intended architecture is HA/microservices mode with:

- independent ingestion, query, metadata, and compaction components;
- internal S3-compatible object storage;
- persistent metadata volumes;
- authenticated TLS write/query gateway;
- replica, disruption, topology, resource, monitoring, alert, retention, backup, and restore controls.

Pyroscope OSS does not provide an authentication layer. NetworkPolicy alone is not authentication; do not expose backend Services directly. See [Security and compliance](docs/SECURITY-AND-COMPLIANCE.md).

The supplied server values deliberately keep `auth.multitenancy-enabled=false`. Enable `auth.multitenancy-enabled=true` only after the production gateway authenticates and authorizes every writer and reader, removes any caller-supplied tenant header, and sets its own trusted `X-Scope-OrgID`. Before rollout, prove that a missing or spoofed tenant header is denied and that tenant A cannot write to or query tenant B. Never enable server multitenancy while trusting the client header by itself.

The handoff pins Pyroscope server/chart `2.2.0`; exact digests and checksums are in [THIRD_PARTY-NOTICES.md](THIRD_PARTY-NOTICES.md) and `deploy/helm/SHA256SUMS`. The [production values example](deploy/helm/values-production-ha.example.yaml) is deliberately incomplete until the company supplies its storage class, externally managed `pyroscope-s3` Secret, TLS/auth gateway, capacity settings, and approvals. It must not be installed as-is.

## Finalize and publish the Go module

`go.mod` intentionally uses the temporary path `profiler-probe`. Before the first tag, replace it with the approved, reachable corporate repository path:

```sh
GOWORK=off go run ./cmd/set-module-path -- git.company.example/platform/profilerprobe
GOWORK=off go list -m
GOWORK=off make verify
```

Replace the example domain with the real corporate path. `GOWORK=off` isolates this module from any enclosing corporate workspace while the command runs. The migration command validates the path, updates `go.mod` and every Go import of the old module (including the build-tagged smoke workload), then runs module-tidiness, checksum, ordinary-test, and smoke-tag checks. If a check fails, it restores every file it changed. It also repairs the bootstrap import if somebody already changed only `go.mod`. Do not use `go mod edit -module`, search/replace, or `sed` alone for this repository; those approaches can leave the smoke command and CI referring to different modules.

Commit the resulting module/source files, obtain code/security/license approval, create a semantic tag, and have consumers pin the tag:

```sh
go get git.company.example/platform/profilerprobe@v0.1.0
```

For private modules, configure `GOPRIVATE` and approved repository authentication in developer/CI environments. Never embed Git credentials in source, `go.mod`, build arguments, or image layers. Prefer one versioned module over copying the source into each service so fixes, ownership, and provenance remain traceable.

## Integrate a Go application

Call `StartFromEnv` once before starting business servers, consumers, schedulers, or worker pools. Call `Stop` through the application's graceful-shutdown path so the SDK can synchronously snapshot the final interval, wait for queued uploads, and then stop collection.

```go
package main

import (
	"log/slog"

	profilerprobe "git.company.example/platform/profilerprobe"
)

func main() {
	started, err := profilerprobe.StartFromEnv()
	if err != nil {
		// Fail open: keep the business service available. Use the service's
		// sanitized structured logger; never log secrets or all environment values.
		slog.Error("continuous profiler did not start", "error", err)
	} else if started {
		defer func() {
			if err := profilerprobe.Stop(); err != nil {
				slog.Error("continuous profiler did not stop cleanly", "error", err)
			}
		}()
		slog.Info("continuous profiler started")
	}

	runApplicationUntilGracefulShutdown()
}
```

The example fails open: profiler configuration/start failure is visible but does not prevent business traffic. A service may choose fail closed only through an explicit risk decision. `StartFromEnv()` returns `(false, nil)` when disabled, and a duplicate successful initialization returns `ErrAlreadyStarted`. `Stop()` performs `Flush(true)` before SDK shutdown, is concurrent-safe, and is idempotent after a successful start; calling it before a successful start returns `ErrNotStarted`. A stopped process cannot start a second session. At the default five-second HTTP timeout, reserve at least 45 seconds of termination grace for the profiler in addition to normal application draining, measure it in the canary, and increase the budget if the timeout is raised. A clean `Stop()` does not by itself prove the backend stored every upload; verify the server/gateway path.

Changing environment values requires a new process or pod. Do not also start another Pyroscope session from a different package path.

## Minimum enabled configuration

The shared/base configuration must remain disabled. Enable only a reviewed evaluation or canary overlay:

```dotenv
PYROSCOPE_ENABLED=true
PYROSCOPE_SERVER_ADDRESS=https://profiles-write.internal.example
PYROSCOPE_APPLICATION_NAME=orders-api
PYROSCOPE_TAGS=environment=staging,version=2026_07_24_a1b2c3d
PYROSCOPE_BASIC_AUTH_USER=orders-api
PYROSCOPE_BASIC_AUTH_PASSWORD_FILE=/var/run/secrets/pyroscope/password
```

The address and names above are examples. Do not copy them as company values. Use a mounted secret file or an identity handled by the approved gateway/service mesh; do not put a real password in a source-controlled environment file.

## Configuration reference

Start from [.env.example](.env.example). When `PYROSCOPE_ENABLED` is absent, blank, or false, the loader returns a disabled configuration and ignores the other settings.

### Connection and identity

| Variable | Default | Meaning and validation |
| --- | --- | --- |
| `PYROSCOPE_ENABLED` | `false` | Explicit opt-in master switch. |
| `PYROSCOPE_SERVER_ADDRESS` | none | Required when enabled. Absolute `http` or `https` URL with a host. Credentials, query, and fragment are rejected. Production must use internal HTTPS. |
| `PYROSCOPE_ALLOW_INSECURE_HTTP` | `false` | Permits `http` only when explicitly true. Use only for isolated local/evaluation deployment; never as a production TLS workaround. |
| `PYROSCOPE_APPLICATION_NAME` | none | Required stable logical name, maximum 255 bytes, matching `^[A-Za-z0-9_][A-Za-z0-9_.\-/]*$`. Do not use instance identity. |
| `PYROSCOPE_TAGS` | empty | Comma-separated static `key=value` labels. At most 32 unique keys; keys are at most 128 bytes and match `^[A-Za-z_][A-Za-z0-9_]*$`; values are at most 256 bytes and match `^[A-Za-z0-9][A-Za-z0-9_.:/@+\-]*$`. Reserved `__name__`, delimiters/control characters, empty/duplicate keys, empty values, and malformed entries are rejected. |
| `PYROSCOPE_TENANT_ID` | empty | Optional value matching `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`. It is not authentication; the trusted gateway must authorize and overwrite tenant routing when multitenancy is enabled. |

The upstream SDK also recognizes `PYROSCOPE_ADHOC_SERVER_ADDRESS`, but this wrapper requires that variable to be entirely absent because even an explicitly empty value can override the validated destination in the SDK. Do not define it in the process environment.

Use stable, low-cardinality tags such as `environment`, `namespace`, and immutable `version`. Never use a request/user/session/order ID, pod UID, PID, hostname, raw URL, token, or unrestricted user input.

`PYROSCOPE_TAGS` validation covers only the static labels supplied to this wrapper. Existing application calls to `runtime/pprof.Labels`, `runtime/pprof.Do`, `runtime/pprof.WithLabels`, or `pyroscope.TagWrapper` can attach additional sample labels without passing through this validation. Inventory those call sites before adoption, prohibit sensitive or unbounded values, and inspect the labels actually exported by the canary rather than relying only on environment review.

### Authentication and logging

| Variable | Default | Meaning and validation |
| --- | --- | --- |
| `PYROSCOPE_BASIC_AUTH_USER` | empty | Optional gateway Basic-auth user, at most 256 bytes with no colon, CR, LF, or NUL; it must be paired with a password. |
| `PYROSCOPE_BASIC_AUTH_PASSWORD` | empty | Direct password option, at most 4 KiB with no CR, LF, or NUL. Avoid in orchestrated production; never commit it or print it. Mutually exclusive with the file option. |
| `PYROSCOPE_BASIC_AUTH_PASSWORD_FILE` | empty | Preferred read-only mounted secret file. The read guard is 64 KiB, but the effective password maximum is 4 KiB. One trailing LF/CRLF is removed. Mutually exclusive with the direct password. |
| `PYROSCOPE_LOGGING` | `false` | Enables the SDK standard logger. Use temporarily under log-classification and volume controls. |

The wrapper does not provide a TLS-skip option, bearer-token field, or client-certificate loader. Use the company CA trust in the application image and an approved gateway/mesh pattern. New authentication mechanisms require a reviewed code and threat-model change.

### Collection behavior

| Variable | Default | Meaning and validation |
| --- | --- | --- |
| `PYROSCOPE_UPLOAD_RATE` | `15s` | Go duration between uploads, from `10s` through `5m`. Shorter intervals increase gateway/server request and processing load. |
| `PYROSCOPE_HTTP_TIMEOUT` | `5s` | Per-upload HTTP timeout from `1s` through `30s`; it also bounds each queued request during synchronous shutdown flush. Proxy environment variables are deliberately ignored and redirects are rejected so profiles stay on the validated direct route. Response headers are capped at 32 KiB and the decoded response body at 64 KiB. |
| `PYROSCOPE_DISABLE_GC_RUNS` | `true` | Prevents SDK-triggered GC before heap snapshots to reduce application latency impact. Heap views may include objects awaiting natural GC; set false only in a measured investigation. |
| `PYROSCOPE_PROFILE_CPU` | `true` | CPU time profile. |
| `PYROSCOPE_PROFILE_ALLOC_OBJECTS` | `false` | Allocated object-count profile; off to avoid a duplicate allocation view by default. |
| `PYROSCOPE_PROFILE_ALLOC_SPACE` | `true` | Allocated bytes profile. |
| `PYROSCOPE_PROFILE_INUSE_OBJECTS` | `false` | In-use object-count profile; off to avoid a duplicate heap view by default. |
| `PYROSCOPE_PROFILE_INUSE_SPACE` | `true` | In-use heap bytes profile. |
| `PYROSCOPE_PROFILE_GOROUTINES` | `true` | Goroutine profile. |
| `PYROSCOPE_PROFILE_MUTEX` | `false` | Enables mutex count and duration profiles and changes Go runtime mutex sampling. Use only for a measured contention investigation. |
| `PYROSCOPE_MUTEX_PROFILE_FRACTION` | `5` | Positive runtime sampling fraction, applied only when mutex profiling is enabled. |
| `PYROSCOPE_PROFILE_BLOCK` | `false` | Enables block count and duration profiles and changes Go runtime block sampling. Use only for a measured investigation. |
| `PYROSCOPE_BLOCK_PROFILE_RATE` | `5` | Positive runtime block sampling rate, applied only when block profiling is enabled. |

At least one profile type must remain enabled. Boolean values use Go
`strconv.ParseBool` forms. During `Stop()`, the wrapper restores the previous
mutex sampling fraction. Go exposes no getter for the current block-profile
rate, so an enabled block profiler is disabled with rate `0` rather than
restored. Keep block profiling off unless this wrapper is the process's sole
owner of that runtime setting, or coordinate the lifecycle explicitly with the
other owner.

## Operational behavior

- The wrapper has no cloud-platform or Linux-only gate. Validate the pinned SDK and actual application on every supported target OS/architecture; the supplied container/Kubernetes path is Linux.
- `Start` initializes the local asynchronous session. It does not perform an end-to-end upload/query health check. Confirm a profile in Grafana before declaring success.
- If the backend is unavailable after startup, application traffic should continue; observe SDK/gateway metrics and logs and retain fail-open behavior.
- Oversized server/gateway responses, including chunked or gzip-compressed responses that exceed 64 KiB after decompression, fail that upload deterministically instead of being read without a memory bound.
- The wrapper opens no HTTP listener. It does not expose `/debug/pprof`.
- Go has one active CPU profiler per process. Do not add an unrelated CPU profiler without a compatibility test.
- Start with default profile types and upload rate. Add mutex/block or object-count views only when they answer a specific question and canary measurements remain within budget.

## Deployment and storage

The production backend uses vendor-neutral internal S3-compatible storage, for example a company-approved MinIO or Ceph implementation. Credentials are supplied only through the company secret system. The storage owner must validate TLS, least privilege, compatibility, capacity, retention, compaction, lifecycle, backups, and restore.

Do not use local filesystem storage in the production microservices topology. Do not let object-store lifecycle rules delete active blocks before Pyroscope retention/compaction. See the [runbook](docs/RUNBOOK.md) and [handoff](docs/HANDOFF.md).

## No external runtime dependency and air-gapped builds

The intended runtime has no cloud-provider dependency and disables Pyroscope anonymous usage statistics. It should need no internet egress.

Build/install tools can still fetch the Go toolchain, Go modules, the `govulncheck v1.6.0` tool module graph and vulnerability database, Helm charts, and images from public registries by default. Controlled or air-gapped environments must preinstall or internally distribute the exact Go 1.25.12 toolchain and approved vulnerability scanner, mirror their required data plus the pinned application module graph, chart, and image digests into internal registries/proxies, and verify checksums, signatures/provenance, SBOMs, and licenses. Set `GOTOOLCHAIN=local` on runners that must reject automatic public toolchain downloads, after installing the approved version. When an approved `govulncheck v1.6.0` binary is preinstalled, run `GOWORK=off make verify GOVULNCHECK=govulncheck`; otherwise the default Make target downloads/builds the pinned tool module. Details are in [HANDOFF.md](docs/HANDOFF.md).

## Verify the library release

Run:

```sh
GOWORK=off make verify
```

The aggregate gate checks formatting, module tidiness and integrity, `go vet`, race-enabled tests, atomic coverage, and reachable vulnerabilities. Race testing requires CGO and a C toolchain. The supplied CI also enforces statement coverage, Linux `amd64`/`arm64` builds, dependency review, and scheduled vulnerability scanning.

Repository CI validates the library. It does not replace application-image build/scan, Helm rendering/policy checks, evaluation, production storage/restore validation, canary evidence, or rollout approval.

## OpenTelemetry Profiles status

OpenTelemetry Profiles is Alpha at this baseline and is not the selected production transport. Alpha interfaces and components can change and are not appropriate for this critical path without a new architecture decision. Stable OTel traces, metrics, and logs can continue alongside Pyroscope.

## Licenses

- This wrapper repository: Apache License 2.0 ([LICENSE](LICENSE)).
- `github.com/grafana/pyroscope-go` client linked into applications: Apache License 2.0.
- Self-hosted Grafana Pyroscope server: AGPL-3.0, deployed separately.

Legal/OSPO must approve exact versions, modifications, distribution/network-use policy, source-availability obligations, notices, and SBOMs. "Open source" or "no license fee" does not remove operational or license obligations. See [THIRD_PARTY-NOTICES.md](THIRD_PARTY-NOTICES.md) and [SECURITY.md](SECURITY.md).

## Upstream references

- [Configure Pyroscope clients](https://grafana.com/docs/pyroscope/latest/configure-client/)
- [Go push SDK](https://grafana.com/docs/pyroscope/latest/configure-client/language-sdks/go_push/)
- [Pyroscope deployment modes](https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/deployment-modes/)
- [Object storage configuration](https://grafana.com/docs/pyroscope/latest/configure-server/storage/configure-object-storage-backend/)
- [Pyroscope server authentication](https://grafana.com/docs/pyroscope/latest/reference-server-api/)
- [Grafana Pyroscope data source](https://grafana.com/docs/grafana/latest/datasources/pyroscope/configure-pyroscope-data-source/)
- [OpenTelemetry Profiles Alpha status](https://opentelemetry.io/docs/specs/otel/profiles/)
