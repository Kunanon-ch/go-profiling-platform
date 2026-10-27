# Senior DevOps handoff

## Handoff outcome

This repository is a production-oriented adoption package for self-hosted Grafana Pyroscope OSS. It contains an in-process Go bootstrap library, tests, CI/security policy, deployment references, an architecture decision, an operations runbook, and production acceptance gates.

It has **no runtime dependency on a cloud provider**. It does require company-operated compute, memory, network, DNS, TLS, Kubernetes or equivalent scheduling, persistent volumes, internal S3-compatible object storage, an authenticated gateway, Grafana, monitoring, backup/restore, and on-call ownership.

The Go package is not a sidecar or standalone server. Every target application must import the approved module release, call `profilerprobe.StartFromEnv()` once near startup, call `profilerprobe.Stop()` during graceful shutdown, and build a new application binary/image.

The supplied single-binary Pyroscope topology is evaluation-only. Production must use the pinned official Helm chart in a reviewed HA/microservices topology with internal S3-compatible object storage and the security controls below.

## Design summary

```text
Go application + profilerprobe
  -> authenticated internal HTTPS write gateway
  -> self-hosted Pyroscope OSS
  -> internal S3-compatible object store + metadata persistence

Authorized operator
  -> company SSO / Grafana
  -> authenticated Pyroscope query gateway
  -> flame graphs and profile comparisons
```

The design deliberately does not expose application `pprof`, require privileged/eBPF node access, call a cloud profiling API, or use cloud IAM/service accounts. See [Architecture and design decisions](ARCHITECTURE.md).

## Artifact map

| Artifact | Purpose |
| --- | --- |
| `profiler.go` and `profiler_test.go` | Disabled-by-default Go push SDK wrapper, validation, single-start guard, graceful stop, and tests. |
| `.env.example` | Non-secret application configuration contract and safe defaults. |
| `.github/workflows` and `Makefile` | Code quality, race, coverage, cross-build, dependency, and vulnerability gates. |
| [Deployment overview](../deploy/README.md) | Routes operators to all local, Helm, and Kubernetes assets. |
| [Docker Compose evaluation](../deploy/docker-compose/README.md) | Loopback-only Pyroscope/Grafana stack with a real Go smoke workload. |
| [Helm deployment](../deploy/helm/README.md) | Pinned single-binary values, HA/microservices production example, chart checksum, and operator commands. |
| [Kubernetes integration](../deploy/kubernetes/README.md) | Disabled application patch, secret overlay, local identities, and deny-by-default NetworkPolicies. |
| [Architecture](ARCHITECTURE.md) | Product choice, topology, storage, security boundaries, OTel decision, and license boundary. |
| [Operations runbook](RUNBOOK.md) | Evaluate, deploy, enable, observe, troubleshoot, stop, roll back, restore, and respond. |
| [Security and compliance](SECURITY-AND-COMPLIANCE.md) | Data, identity, TLS, secret, network, Kubernetes, storage, supply-chain, audit, and approval controls. |
| [Release validation evidence](VALIDATION.md) | Point-in-time library, manifest, and fresh-volume end-to-end smoke-test results. |
| [Security policy](../SECURITY.md) | Vulnerability reporting and release security gates. |
| [Support](../SUPPORT.md) | Support boundary and ownership requirements. |
| [Third-party notices](../THIRD_PARTY-NOTICES.md) | Pinned Pyroscope Go client, server image, chart checksums, and license inventory. |

Deployment files are references, not authorization. They do not create a company's Kubernetes cluster, DNS zone, certificate authority, credential-delivery controller, compatible object-storage service, production authentication gateway, Grafana instance, monitoring system, backup system, support team, or production SLO. The production values intentionally require an external `pyroscope-s3` Secret and a replaced storage-class sentinel; the package contains no secret values.

## Required site inputs

Every row needs a real, reviewed value and named owner before production. Secret **values** must remain in the approved secret system and outside the handoff/change evidence.

| Input or decision | Used by | Required owner |
| --- | --- | --- |
| Corporate Git repository and Go module path | `go.mod`, consumers, CI | Repository/platform owner |
| Supported wrapper release and upgrade policy | Consumers and support | Library owner |
| Kubernetes cluster/namespace and environment | Helm/application overlays | Platform/application owner |
| Internal Pyroscope write/query DNS names | Application, gateway, Grafana | Network/observability owner |
| TLS certificates, CA trust, and rotation | Gateway, clients, Grafana | PKI/network owner |
| Gateway product, routes, authentication, authorization, limits, and audit | Write/query boundary | Security/platform owner |
| Application credential secret reference and rotation | Application overlay | Application/security owner |
| Multitenancy mode and tenant IDs | Wrapper, gateway, Pyroscope | Security/data/observability owner |
| Stable application name | `PYROSCOPE_APPLICATION_NAME` | Application owner |
| Environment and immutable release labels | `PYROSCOPE_TAGS` | Release/application owner |
| Enabled profile types, upload rate, and sampling controls | Application overlay | Application/SRE owner |
| Production Pyroscope/chart/image versions and digests | Helm release | Observability/OSPO owner |
| Component replicas, requests, limits, topology, and autoscaling | Production values | SRE/capacity owner |
| Internal S3-compatible endpoint, bucket, region/style, and CA | Pyroscope storage | Storage owner |
| Object-store credential secret reference and least-privilege policy | Pyroscope storage | Storage/security owner |
| Metadata persistent volume class, size, snapshots, and recovery | Stateful components | Storage/platform owner |
| Retention, deletion delay, lifecycle, legal hold, and residency | Pyroscope/object storage | Data/security/legal owner |
| Backup/restore RPO and RTO | Metadata and object storage | Storage/SRE owner |
| Grafana URL, data source, SSO roles, and viewers | Query path | Observability/security owner |
| Application and backend canary thresholds/duration | Rollout record | Service/SRE owner |
| On-call, escalation, incident, support, and change record | Operations | Operations owner |
| Apache-2.0/AGPL-3.0 decision, SBOM, and source obligations | Release evidence | Legal/OSPO owner |
| Approved Go 1.25.13 toolchain distribution plus internal mirrors for Go modules, chart, and images | Build/install | Supply-chain/platform owner |

## Air-gapped and dependency-mirror decision

The runtime design requires no public-cloud API and should require no internet egress. However, a default build or installation can fetch the Go toolchain, Go modules, vulnerability data, a Helm chart, and container images from public registries.

For an air-gapped or controlled environment, the platform team must:

1. preinstall or internally distribute the exact approved Go 1.25.13 toolchain; set `GOTOOLCHAIN=local` where runners must reject automatic public toolchain downloads;
2. mirror the approved `pyroscope-go` module and all transitive modules into the internal Go proxy;
3. preinstall an approved `govulncheck v1.6.0` binary and invoke the gate with `make verify GOVULNCHECK=govulncheck`, or mirror `golang.org/x/vuln/cmd/govulncheck@v1.6.0` and its build-time transitive modules for the default Make target; these modules are separate from this repository's `go.mod` graph;
4. mirror the exact Pyroscope, gateway, and optional Grafana images by digest into the approved internal registry;
5. mirror and provenance-check the pinned Helm chart;
6. provide an internal vulnerability database/update process or an approved scanning boundary;
7. replace public registry locations only through reviewed site values;
8. verify checksums, signatures/provenance, SBOM, and licenses before promotion;
9. deny optional runtime internet egress and verify anonymous usage statistics remain disabled.

## Implementation sequence

### 1. Publish the Go module

- Move the source into the approved corporate repository.
- From the repository root, replace the temporary module and its internal imports in one verified operation: `GOWORK=off go run ./cmd/set-module-path -- <approved-corporate-module-path>`. Disabling workspace mode prevents an enclosing corporate `go.work` from changing this module's verification inputs.
- Confirm `GOWORK=off go list -m` prints the approved path. Do not use `go mod edit -module` alone: the build-tagged smoke command imports this module and must be migrated with `go.mod`. The supplied command validates module tidiness/checksums, normal tests, and smoke-tag tests and rolls back its edits if any check fails.
- Run module tidy/verify, tests, race tests, coverage, vulnerability, SBOM, and license review.
- Tag a semantic release and require consumers to pin it.

### 2. Integrate one non-production application

- Import the module and call `StartFromEnv()` once before business servers/workers start.
- Inventory `runtime/pprof.Labels`, `runtime/pprof.Do`, `runtime/pprof.WithLabels`, and `pyroscope.TagWrapper` use in the consuming service. These sample labels bypass static `PYROSCOPE_TAGS` validation; remove sensitive/unbounded values and record a canary export review.
- Log startup errors through the service's sanitized structured logger and choose fail-open unless service policy explicitly says otherwise.
- Call `Stop()` in the established graceful-shutdown path; it performs a synchronous final flush. At the default `PYROSCOPE_HTTP_TIMEOUT=5s`, reserve at least 45 seconds for the profiler in addition to business draining, and measure the complete shutdown budget.
- Build and scan a new immutable application image.
- Leave the shared/base configuration disabled.

### 3. Complete isolated evaluation

- Deploy the single-binary evaluation stack privately.
- Use only non-production credentials/data and an isolated namespace/PVC.
- Enable one application canary under representative load.
- Prove profiles are visible, labels are correct, profile types are useful, overhead stays within thresholds, and graceful stop/disable works.
- Do not approve production availability or durability from this result.

### 4. Build the production platform

- Review the pinned Pyroscope release's current architecture and migration notes.
- Deploy the official chart in the approved HA/microservices topology.
- Configure internal S3-compatible object storage and metadata persistence.
- Put all ingestion/query paths behind the authenticated TLS gateway.
- Configure Grafana through the authenticated query route.
- Apply restricted pod security, network isolation, PDB/topology, resources, monitoring, alerts, backup, and restore.
- Verify anonymous usage statistics are disabled and the runtime has no unapproved external egress.

### 5. Validate security and recovery

- Prove unauthorized uploads, queries, direct backend access, and cross-tenant requests fail.
- Keep the server baseline at `auth.multitenancy-enabled=false`; if multiple trust boundaries are required, enable it only after the gateway authorizes and overwrites `X-Scope-OrgID`, then prove missing/spoofed headers and tenant-A-to-tenant-B writes/queries are denied.
- Prove credentials rotate without source changes or manual distribution.
- Run a restore into an isolated environment and query restored profiles.
- Review profile content and labels under the data-classification policy.
- Archive SBOM/license/security evidence and owner approvals.

### 6. Canary and progressive production rollout

- Deploy the application disabled baseline first.
- Enable a small canary by creating new processes/pods.
- Record application and backend metrics, query evidence, storage growth, and errors for the approved duration.
- Perform a timed client disable/restart rehearsal and verify server rollback compatibility.
- Promote in controlled stages only after every acceptance gate passes.

## Application preflight

- The approved module release is present in the binary/SBOM.
- Exactly one code path calls `Start` or `StartFromEnv`.
- The graceful-shutdown path calls `Stop` and stays within its termination grace period.
- No application `pprof` listener or unrelated CPU profiler is exposed or scheduled without review.
- `PYROSCOPE_ENABLED=false` remains the base value.
- `PYROSCOPE_ADHOC_SERVER_ADDRESS` is absent, not merely empty, so the SDK cannot override the validated destination.
- The production server address uses internal HTTPS; insecure HTTP is permitted only by the explicit evaluation flag in an isolated environment.
- Application name is stable across replicas and labels are bounded/non-sensitive.
- Runtime/pprof and `pyroscope.TagWrapper` sample-label call sites have been inventoried, and actual canary exports contain no sensitive, user-controlled, or unbounded labels.
- Password-file/secret delivery is used according to policy; no credentials appear in a URL, manifest value, image, or log.
- Higher-overhead mutex/block types remain disabled unless approved.
- Image is immutable, signed/provenanced as required, scanned, and runnable on the target OS/architecture.

## Platform preflight

- Production uses the supported HA topology for the pinned Pyroscope release; the evaluation single binary is not promoted.
- Internal object storage compatibility, least privilege, TLS, capacity, lifecycle, backup, and restore are proven.
- Stateful metadata components have the required quorum, PVC, disruption, and topology controls.
- Gateway authenticates/authorizes both paths and owns the tenant header trust boundary.
- Backend services are not directly reachable by applications/users/public networks.
- Grafana viewer/admin roles are separated and the data source points to the query gateway.
- Required NetworkPolicy, DNS, CA trust, time synchronization, metrics, logs, dashboards, and alerts are operational.
- All placeholders are replaced; rendered manifests pass schema, policy, secret, and server-side dry-run checks.
- Runtime has no optional external/cloud-provider dependency or egress; usage reporting is disabled.

## Acceptance record

Record a before/after window with representative traffic. Thresholds must come from the owning service and capacity plan.

| Gate | Evidence | Pass criterion |
| --- | --- | --- |
| Application health | Readiness, errors, restarts | Within approved service thresholds. |
| Application overhead | CPU, RSS, GC, p50/p95/p99 latency | Within approved profiling budget. |
| Graceful lifecycle | Start/stop logs and shutdown duration | One start, clean stop, no termination regression. |
| Profile correctness | Grafana query for each enabled type | Correct app/environment/version, expected stacks, no sensitive/unbounded labels. |
| Ingestion | Gateway/distributor requests, rejects, retries | No sustained auth, rate, timeout, or 5xx failure. |
| Query | Query success and latency | Within platform objective under expected concurrency. |
| Storage | Growth, latency, errors, compaction | Within forecast, no errors/backlog/pressure. |
| Tenant/access isolation | Negative tests and audit evidence | Unauthorized/direct/cross-tenant access denied. |
| Availability | Pod disruption/failure test as approved | Meets the platform availability objective. |
| Recovery | Isolated restore and query | Meets documented RPO/RTO. |
| Rollback | Client disable/restart and server plan | Completes within rollback objective without data corruption. |

## Definition of Done

The handoff is production-ready only when all items are true:

- corporate module path, owner, `CODEOWNERS`, supported versions, and tagged release are established;
- exact release passes CI, race, coverage, vulnerability, image, manifest, provenance, SBOM, and license gates;
- application integration, graceful stop, immutable image, and fail-open/fail-closed decision are owned;
- all site inputs and placeholders are resolved without static secrets;
- evaluation passes but is not reused as the production topology;
- HA Pyroscope, gateway, Grafana query path, internal S3-compatible storage, persistence, monitoring, backup, and restore are operational;
- runtime makes no unapproved cloud-provider or internet calls and usage statistics are disabled;
- security/data/Legal or OSPO approvals are recorded, including AGPL-3.0 obligations;
- application and backend canary thresholds pass under representative load;
- isolation, credential rotation, restore, and rollback tests pass;
- on-call, incident, support, retention, upgrade, and capacity procedures are active;
- release evidence is attached to the company change record.

## Known handoff boundaries

This package cannot supply or decide company identities, DNS, certificates, secrets, storage credentials, cluster capacity, SLO thresholds, retention, support contacts, or legal approval. Senior DevOps/Platform must provide those values through owned systems; the package deliberately contains safe placeholders and no credential material.
