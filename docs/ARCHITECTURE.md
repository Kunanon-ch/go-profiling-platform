# Architecture and design decisions

## Outcome

This package provides continuous profiling for Go applications without a dependency on a public-cloud profiling service. A consuming application embeds the Apache-2.0 `pyroscope-go` push SDK through this wrapper and sends profiles to a company-operated Grafana Pyroscope OSS backend.

The selected production path is:

```text
Go application
  -> profilerprobe (in-process, push mode)
  -> internal authenticated TLS gateway
  -> Pyroscope distributor
  -> Pyroscope storage and query components
  -> internal S3-compatible object storage

Operator
  -> company SSO / authenticated Grafana
  -> Pyroscope data source / query gateway
  -> flame graph and profile comparison
```

No Google Cloud, AWS, Azure, or other cloud-provider API is required. "No cloud-provider dependency" does not mean "no infrastructure": the company must provide compute, memory, network, DNS, TLS certificates, persistent volumes, object storage, monitoring, backups, and operators.

## Why push-mode Go instrumentation

This handoff selects the official Pyroscope Go push SDK instead of exposing application `pprof` endpoints or relying on host-level eBPF collection.

- It captures Go CPU, heap/allocation, goroutine, mutex, and block profiles according to the enabled configuration.
- It does not register an application HTTP handler or create a public `pprof` endpoint.
- It works with ordinary container isolation and does not require privileged pods, host PID access, kernel capabilities, or node agents.
- It keeps application identity and immutable release labels explicit.
- It sends directly to an internal ingestion endpoint, avoiding a required per-node collector in the baseline architecture.

The tradeoff is that every profiled Go binary must import the module, call it once during startup, and be rebuilt. SDK and server upgrades are separate release responsibilities.

## Alternatives considered

| Option | Strength | Reason it is not the baseline |
| --- | --- | --- |
| Pyroscope Go push SDK | Full Go runtime profile selection, explicit identity, no listener/privilege, low moving-part count | Selected. Requires an application rebuild. |
| Pull from application `pprof` | Uses native Go endpoints and can be scraped by a collector | Creates an HTTP profile surface that needs per-workload authentication/network controls and exposes sensitive runtime data if misconfigured. |
| Node-level eBPF/Alloy | Can profile binaries without source changes | Primarily CPU-focused for this use case and requires host visibility/capabilities plus a node collector; it does not replace the required Go heap/goroutine views. |
| OpenTelemetry Profiles | Emerging vendor-neutral signal | Alpha at this baseline; not selected for a critical production path. |
| Managed cloud profiler | Transfers backend operations to a provider | Conflicts with the requirement to remove runtime cloud-provider dependency. |

"Most efficient" here means the best balance of diagnostic coverage, low application overhead, small security surface, and scalable platform operation. It does not mean enabling every available profile type or using the fewest pods regardless of availability.

## Evaluation versus production

### Evaluation

The evaluation deployment runs Pyroscope as a single process and may use one persistent volume. It is intended only to verify application integration, profile visibility, query behavior, overhead, and rollback.

It is not production-ready because it has a single failure domain, cannot scale components independently, and does not provide the production durability model. Do not treat a successful evaluation as an HA or disaster-recovery test.

### Production

Production should use the official Pyroscope Helm chart in microservices/HA mode, pinned to an approved version and image digest. The production design separates ingestion, query, metadata, and compaction responsibilities so they can scale and fail independently. It requires:

- at least two ingestion and query replicas where the selected Pyroscope architecture supports them;
- an odd-sized metastore quorum as required by the selected release;
- persistent volumes for stateful metadata components;
- internal S3-compatible object storage for profile blocks;
- pod disruption budgets, topology spreading or anti-affinity, and controlled rolling updates;
- an authenticated TLS gateway for both write and query paths;
- resource requests, limits, autoscaling signals, dashboards, alerts, and capacity forecasts;
- tested backup, restore, retention, and disaster-recovery procedures.

Use the exact component topology documented for the pinned Pyroscope release. Do not copy replica counts or storage assumptions from a different major architecture.

## Storage decision

The production baseline is vendor-neutral S3-compatible object storage operated or approved by the company, such as an internal MinIO, Ceph, or another compatible implementation. It must be reachable from Pyroscope without a public-cloud account.

The storage owner must provide:

- an internal TLS endpoint and bucket;
- a dedicated least-privilege identity scoped to that bucket and required operations;
- credentials through the company secret-delivery system, never literal values in Git, Helm values, logs, or container images;
- capacity, availability, replication, encryption, backup, restore, and lifecycle guarantees;
- retention settings coordinated with Pyroscope compaction and deletion behavior.

An object-store lifecycle rule must not delete active Pyroscope objects before Pyroscope retention and compaction have completed. Validate the implementation against the pinned release because S3-compatible products can differ at edge cases.

## Security boundaries

Pyroscope OSS does not provide an authentication layer by itself. Production traffic must terminate at a company-controlled gateway or reverse proxy that:

- authenticates application uploads and human queries;
- authorizes the caller for the intended tenant;
- sets or overwrites `X-Scope-OrgID` when multitenancy is enabled;
- applies TLS, request-size limits, rate limits, audit logging, and denial by default;
- prevents direct network access to backend services.

The supplied values use the safer single-tenant baseline, `auth.multitenancy-enabled=false`. A reviewed multi-tenant deployment may set it to `true` only when the gateway owns tenant selection end to end and negative tests prove missing/spoofed headers and cross-tenant writes/queries are denied.

NetworkPolicy is defense in depth, not authentication. The backend must not be exposed directly through an unauthenticated Ingress, LoadBalancer, NodePort, or public address.

## Data model and cardinality

Profiles contain stack traces, package paths, function names, filenames or line information, service identity, and release labels. Treat them as internal engineering telemetry or a higher classification required by company policy.

Use stable, low-cardinality labels such as service, environment, namespace, and immutable release. User-supplied tag keys deliberately use the conservative underscore-style syntax documented by Pyroscope; dotted SDK-internal labels are not a reason to accept dotted user keys. Never add request IDs, user IDs, session IDs, pod UIDs, raw URLs, customer data, tokens, or unrestricted request attributes. High-cardinality labels increase index, memory, storage, and query cost.

## OpenTelemetry decision

OpenTelemetry Profiles is Alpha at this handoff baseline. Its interfaces and production support can change, and the OpenTelemetry project does not recommend Alpha components for critical workloads. It is therefore not the production transport selected here.

Re-evaluate an OTLP Profiles path only through a new architecture decision after the signal, required collector components, and chosen backend are production-ready for the company's risk class. This does not prevent using stable OpenTelemetry traces, metrics, or logs alongside Pyroscope.

## License boundary

- This wrapper repository is Apache License 2.0.
- The `github.com/grafana/pyroscope-go` client is Apache License 2.0 and is linked into each consuming Go binary.
- The Grafana Pyroscope server is GNU Affero General Public License v3.0 (AGPL-3.0) and is deployed as a separate network service.

Legal or the Open Source Program Office must approve the exact versions in [THIRD_PARTY-NOTICES.md](../THIRD_PARTY-NOTICES.md), deployment, modification policy, distribution model, source-availability obligations, notices, and SBOM before production. Do not assume that internal hosting, lack of license fees, or separation into containers removes license obligations.

## References

- [Configure Pyroscope clients](https://grafana.com/docs/pyroscope/latest/configure-client/)
- [Go push SDK](https://grafana.com/docs/pyroscope/latest/configure-client/language-sdks/go_push/)
- [Pyroscope deployment modes](https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/deployment-modes/)
- [S3-compatible object storage](https://grafana.com/docs/pyroscope/latest/configure-server/storage/configure-object-storage-backend/)
- [Pyroscope server authentication note](https://grafana.com/docs/pyroscope/latest/reference-server-api/)
- [OpenTelemetry Profiles status](https://opentelemetry.io/docs/specs/otel/profiles/)
- [Pyroscope server license](https://github.com/grafana/pyroscope/blob/main/LICENSE)
- [Pyroscope Go client license](https://github.com/grafana/pyroscope-go/blob/main/LICENSE)
