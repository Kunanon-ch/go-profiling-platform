# Operations runbook

## Scope

This runbook covers the Go profiling wrapper, a company-operated Grafana Pyroscope OSS backend, its authenticated gateway, Grafana data source, and internal S3-compatible storage. It does not authorize a production change by itself; use the company's change, incident, and access procedures.

The application SDK pushes profiles asynchronously. A successful `StartFromEnv()` means the local profiling session initialized; it does **not** prove that authentication, networking, ingestion, storage, or query is working. A visible, queryable profile is the end-to-end success signal.

## Roles

Record real contacts in the change record before rollout:

| Responsibility | Required owner |
| --- | --- |
| Consuming Go service and SLO | Application team |
| Pyroscope, gateway, Grafana data source | Observability/platform team |
| Kubernetes/compute and network | Platform team |
| S3-compatible object storage and restore | Storage team |
| Data access, security, and license approval | Security/data/Legal or OSPO |
| Rollout and incident command | SRE/on-call |

## Preflight

Before enabling an application canary:

1. Pin the wrapper module, Pyroscope Go client, Helm chart, gateway, and server images to approved immutable versions or digests.
2. Confirm CI, vulnerability, image, manifest, SBOM, and license gates passed for the exact release.
3. Render the complete deployment and confirm no `REPLACE_WITH_*`, example passwords, plaintext keys, or unapproved external endpoints remain.
4. Confirm Pyroscope anonymous usage statistics are disabled so the stack does not initiate optional external reporting.
5. Confirm the production backend is reachable only through the authenticated TLS gateway; backend Services are not public.
6. Confirm tenant routing, authentication, authorization, rate limiting, request-size limits, and audit logging at the gateway.
7. Confirm application credentials and object-store credentials are delivered by the approved secret system and are not present in Git, images, ordinary Helm values, or logs.
8. Confirm NetworkPolicy/firewall allows only required DNS, gateway, Grafana/query, object-store, and intra-cluster component flows.
9. Confirm persistent volumes, bucket capacity, encryption, retention, backup, restore, and object lifecycle settings.
10. Confirm Pyroscope/Grafana dashboards and alerts plus application CPU, RSS, latency, error, and restart baselines are available.
11. Confirm the rollback manifest is ready and the canary population, duration, traffic, and acceptance thresholds are approved.

## Evaluation deployment

For the quickest functional proof, use the loopback-only [Docker Compose evaluation](../deploy/docker-compose/README.md). For a Kubernetes rehearsal, use the [single-binary Helm values](../deploy/helm/README.md). Both are evaluation-only.

1. Deploy to a developer workstation or isolated non-production namespace using the matching guide and an approved persistent volume class when Kubernetes is used.
2. Keep ingress private. Prefer `kubectl port-forward` for a short operator test rather than creating public exposure.
3. Leave the target application disabled initially and verify Pyroscope readiness, storage write access, metrics, and logs.
4. Configure a single non-production application canary with the evaluation endpoint.
5. Exercise representative workload long enough to cover multiple upload intervals.
6. Confirm CPU, allocation, in-use heap, and goroutine profiles are visible and filterable by application, environment, and immutable version.
7. Stop the client through the application's graceful shutdown path, then confirm the application still shuts down within its termination grace period.
8. Delete evaluation data according to the approved test-data procedure.

Passing evaluation does not validate HA, object-store durability, production scale, upgrades, backups, or disaster recovery.

## Production backend rollout

1. Deploy or upgrade the pinned official Pyroscope Helm chart in the approved microservices/HA topology.
2. Configure internal S3-compatible object storage and persistent volumes through secret references and site-owned values.
3. Verify stateful metadata quorum and all readiness probes before allowing ingestion.
4. Verify the gateway's write route reaches the ingestion component and its query route reaches the query frontend required by the pinned release.
5. Configure the self-managed Grafana Pyroscope data source through the authenticated query route, then run **Save & test**.
6. Run a synthetic non-production upload and query. Record server version, tenant, timestamp, and query result without attaching sensitive profile contents.
7. Confirm dashboards and alerts for ingestion failures, rejected samples, gateway 4xx/5xx, object-store errors/latency, metadata quorum, compaction backlog/failures, disk pressure, restarts, saturation, and query latency.
8. Confirm backup and restore evidence is current before accepting application traffic.

The supplied values keep `auth.multitenancy-enabled=false`. If the approved design requires multiple tenants, first make the gateway authenticate and authorize every writer/reader, remove caller-provided tenant headers, and set the authorized `X-Scope-OrgID`; then set `auth.multitenancy-enabled=true` in the reviewed server values. Before accepting traffic, prove missing/spoofed headers fail and a tenant-A credential cannot write to or query tenant B.

## Enable one application canary

The exact names and defaults are documented in the root `README.md` and `.env.example`.

1. Keep the shared base configuration at `PYROSCOPE_ENABLED=false`.
2. In a canary-only overlay, set `PYROSCOPE_ENABLED=true`, the internal HTTPS server address, stable application name, immutable release/environment tags, and approved tenant/authentication secret references.
3. Confirm `PYROSCOPE_ADHOC_SERVER_ADDRESS` is absent from the final process environment; the wrapper rejects its presence because the upstream SDK could otherwise override the validated destination.
4. Keep higher-overhead mutex and block profiles disabled unless the investigation specifically requires them.
5. Keep the default 15-second upload rate initially. A shorter interval is rejected below 10 seconds; decreasing the interval increases request and processing load.
6. Render and policy-check the complete workload manifest, then create new pods/processes. Environment changes are read at process startup.
7. Confirm one profiler startup message through the application's normal logging policy. Do not print passwords, headers, unrestricted environment contents, or secret-file content.
8. Exercise representative traffic and wait for at least several upload intervals.
9. In Grafana Profiles Drilldown or Explore, select the intended application, environment, version, profile type, and time range.
10. Compare canary and control metrics against the approved thresholds below.

## Canary acceptance record

Use service-owned numbers; generic percentages are not production approval.

| Gate | Evidence | Pass condition |
| --- | --- | --- |
| Application availability | Readiness, restarts, errors | No regression beyond approved SLO threshold. |
| Application performance | CPU, RSS, GC, p50/p95/p99 latency | All deltas within the approved capacity and latency budget. |
| Profile correctness | Query screenshot/link and labels | Expected application, environment, immutable version, and profile types are visible; no sensitive or high-cardinality labels. |
| Ingestion | Gateway and distributor metrics/logs | Successful uploads with no sustained auth, rejection, retry, or 5xx errors. |
| Backend performance | Component saturation, compaction, query latency | No sustained backlog, pressure, or threshold breach. |
| Storage | Object growth, errors, latency, free capacity | Within forecast and retention assumptions; no write/read errors. |
| Isolation | Network and authorization tests | Unauthorized upload/query and cross-tenant access are denied. |
| Rollback | Timed rehearsal | Client disable/restart and backend rollback complete within the approved objective. |

## Profile controls and performance

The safe baseline enables CPU, allocated bytes, in-use bytes, and goroutine profiles. Allocated-object count and in-use-object count are off to avoid duplicate views unless specifically needed. Mutex and block profiles are off because they alter runtime sampling and can add overhead.

`PYROSCOPE_DISABLE_GC_RUNS=true` avoids the SDK-triggered garbage collection that can distort application latency. The tradeoff is that heap views can include objects not yet reclaimed by a natural GC cycle, so they can be less precise at an instant. Set it to `false` only for an approved investigation after measuring latency and CPU impact.

When mutex profiling is enabled, `PYROSCOPE_MUTEX_PROFILE_FRACTION` controls sampling. When block profiling is enabled, `PYROSCOPE_BLOCK_PROFILE_RATE` controls event sampling. Higher sampling precision can increase runtime cost. Change one control at a time, give the change a new immutable release/config identity, and compare with a control. `Stop()` restores the prior mutex fraction, but Go exposes no block-rate getter, so it disables block sampling with rate `0` rather than restoring a prior value. Enable block profiling only when this wrapper is the sole owner of that runtime setting or after explicit lifecycle coordination with the other owner.

Do not start an unrelated CPU profiler concurrently unless its interaction with the pinned SDK has been tested. Go supports one active CPU profiler in a process, and arbitrary profiling libraries can interfere.

## Graceful application shutdown

The wrapper exposes `Stop()` and the consuming service should call it in its established graceful-shutdown path after traffic draining begins and before process exit. `Stop()` synchronously runs `Flush(true)` before SDK shutdown, so it can wait for queued requests. Each request is bounded by `PYROSCOPE_HTTP_TIMEOUT` (`5s` by default). Reserve at least 45 seconds for the profiler at defaults in addition to business draining, increase the budget if that timeout is raised, and prove the total shutdown duration in the canary. Treat `ErrNotStarted` according to the service's shutdown policy; do not turn it into an outage during an already disabled deployment. A nil stop result is not proof of backend persistence; confirm the gateway/server received the tail profile when that evidence is required.

Disabling through environment configuration still requires a new process or pod because configuration is read at startup:

1. Set `PYROSCOPE_ENABLED=false` in the owning deployment source.
2. Render, review, and deploy the change.
3. Wait for old enabled processes to terminate gracefully.
4. Confirm new processes remain healthy and do not initiate new profiling sessions.
5. Confirm ingestion falls only for the intended application/version and application SLOs recover.

## Application rollback

Rollback immediately if application CPU, memory, GC, latency, errors, restarts, or shutdown behavior breaches the approved threshold.

1. Disable profiling in the canary overlay and replace enabled processes.
2. Do not delete historical profiles; preserve the incident window under the approved access policy.
3. Record wrapper/client version, image digest, rendered non-secret configuration, enabled profile types, upload rate, sampling settings, and timestamps.
4. If the problem is limited to a higher-overhead type, keep profiling fully disabled until the service owner approves a narrower re-test.

Profiler upload failures should normally fail open and must not terminate business traffic. A consuming service may adopt fail-closed startup only through an explicit service-specific risk decision.

## Backend rollback

For a faulty Pyroscope/gateway release:

1. Stop progressive rollout and preserve healthy replicas.
2. If ingestion threatens applications or the backend, deny new writes at the gateway or disable application canaries through the normal change path.
3. Roll back only to a version proven compatible with the current metadata and object format. Review the pinned release's upgrade/migration notes first.
4. Do not restore or delete the bucket as a routine rollback action.
5. Verify readiness, metadata quorum, ingestion, compaction, and queries after rollback.
6. If compatibility is uncertain, isolate the affected deployment and escalate to the platform/storage owner rather than forcing an in-place downgrade.

## Troubleshooting

| Symptom | Checks | Action |
| --- | --- | --- |
| `started=false`, no error | `PYROSCOPE_ENABLED` value in the running process | Enable only in the intended overlay and replace the process. |
| Startup validation error | Required server/application values, URL shape, tag/tenant bounds, auth pairing, duration/rate bounds, or presence of `PYROSCOPE_ADHOC_SERVER_ADDRESS` | Correct the named variable; remove the upstream override entirely, and never weaken validation or put credentials in the URL. |
| `ErrAlreadyStarted` | Dependency wiring and duplicate bootstrap paths | Keep exactly one owner of `Start`/`StartFromEnv`. |
| `ErrNotStarted` on shutdown | Whether profiling was disabled or start failed | Handle disabled shutdown idempotently in the application. |
| Start succeeds but no profiles | Time range, upload interval, application/version tags, gateway routes, DNS/TLS, auth, tenant, server readiness | Follow the request from application to gateway, ingestion, storage, and query. Startup is not an upload probe. |
| HTTP 401/403 | Secret version, proxy policy, tenant authorization, clock/certificate state | Rotate/fix through the secret and identity owners; do not log credentials. |
| HTTP 404 | Gateway path rewrite and selected ingestion/query service | Compare routes with the pinned chart documentation. |
| HTTP 429 | Gateway/server limits and cardinality/ingestion spikes | Preserve backpressure, reduce canary/profile load, then size or tune through review. |
| HTTP 5xx/timeouts | Gateway/server saturation, restarts, object-store latency/errors, metadata quorum | Contain rollout, inspect component metrics, and escalate to platform/storage owner. |
| TLS/x509 error | Certificate chain, SAN, CA bundle, time, proxy interception | Fix trust and certificate delivery; never disable certificate verification. |
| Profiles under wrong tenant | Gateway overwrite policy and application tenant configuration | Stop affected writes, treat as a data-isolation incident, correct routing, and assess exposure. |
| Excessive backend memory/storage | Label cardinality, duplicate profile types, upload rate, retention, compaction | Disable offending canary, remove unbounded labels in a new release, and reforecast capacity. |
| Heap view seems high | Natural GC timing and `PYROSCOPE_DISABLE_GC_RUNS=true` | Correlate with runtime metrics; test forced SDK GC only in an approved canary. |
| Application overhead | Profile types, upload interval, mutex/block sampling, logging | Disable first; re-enable minimum required types one at a time under measurement. |
| Query is slow or empty | Time range, tenant, query frontend, compaction/storage health | Narrow the query, validate routing, then inspect query and storage components. |

## Storage, retention, backup, and restore

- Set retention from the approved data policy and capacity forecast; do not leave an accidental unlimited retention.
- Coordinate Pyroscope deletion with object-store lifecycle rules. The object store must not independently expire active blocks earlier than Pyroscope expects.
- Monitor raw and compacted object growth, request rates, error rates, compaction temporary space, and persistent-volume capacity.
- Encrypt traffic and data at rest under company policy.
- Back up required metadata/configuration and protect object-store durability according to the recovery objectives.
- Perform a scheduled restore into an isolated environment and prove profiles can be queried. A successful backup job without restore evidence is insufficient.
- Treat bulk deletion, bucket policy changes, retention reductions, and restore operations as reviewed high-risk changes.

## Security incident containment

If credentials, tenant routing, profile data, or an endpoint may be exposed:

1. Protect business availability and stop the affected upload/query path at the authenticated gateway.
2. Disable the affected application profilers and replace processes if necessary.
3. Revoke/rotate only the affected credentials through the secret owner; do not distribute replacements manually.
4. Preserve gateway audit logs, Kubernetes events, immutable deployment versions, access-policy changes, and relevant storage access evidence.
5. Determine affected tenants, applications, profile types, users, and time range.
6. Follow the company security/data incident process for notification, retention hold, and remediation.
7. Re-enable only after root cause, access review, credential rotation, tenant-isolation test, and owner approvals are complete.

## Routine maintenance

- Review Pyroscope/client/chart/gateway releases, vulnerabilities, and licenses on a defined cadence.
- Forecast compute, memory, object storage, and query growth from actual ingestion rather than vendor examples.
- Review gateway users, application credentials, Grafana roles, tenant mappings, and secret age.
- Exercise application rollback and storage restore at the agreed frequency.
- Verify anonymous usage reporting stays disabled after upgrades.
- Reassess OpenTelemetry Profiles only through an architecture review; its Alpha status is not a production migration trigger.
