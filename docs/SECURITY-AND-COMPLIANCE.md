# Security and compliance controls

## Baseline

This design is self-hosted and cloud-provider-independent. It removes Google Cloud Project, Cloud Profiler API, cloud IAM, GSA, and cloud metadata/ADC requirements. The organization still operates a network service that receives sensitive engineering telemetry and therefore must provide identity, transport security, storage security, access governance, and operational ownership.

No deployment is production-approved until every control marked as required below has an owner and evidence in the change record.

## Data classification

Pyroscope profiles generally contain sampled stack traces rather than request or response bodies, but they can expose:

- package, module, class, function, method, filename, and line information;
- internal service names, versions, environments, namespaces, and architecture;
- execution hot paths, blocking relationships, and workload behavior;
- custom labels configured by the application or gateway.

Classify profiles as internal engineering telemetry at minimum, or at the higher class required by source-code, security, customer, or regulatory policy. Profiles, screenshots, exported files, and query links inherit that classification.

Required decisions:

- permitted applications, environments, profile types, and users;
- data residency and approved storage location;
- retention, deletion, legal hold, backup, and restore policy;
- whether filenames, symbol names, or labels create regulated-data concerns;
- rules for screenshots, exports, support attachments, and cross-team sharing;
- incident notification and audit retention.

## Data minimization and labels

Only stable, low-cardinality labels approved by the data owner are allowed. The baseline uses logical application identity plus immutable release and environment context.

Do not send:

- user, customer, account, device, session, request, trace, or order identifiers;
- email addresses, names, IP addresses, raw URLs, query strings, or headers;
- pod UID, process ID, hostname, or other per-instance values unless a separately approved bounded use case exists;
- tokens, passwords, keys, certificates, or secret references;
- unrestricted user-controlled input.

Label keys and values must pass the wrapper validation: at most 32 tags, 128-byte underscore-style keys, and 256-byte values matching `^[A-Za-z0-9][A-Za-z0-9_.:/@+\-]*$`. This excludes label-set delimiters, whitespace, and control characters. The tenant ID also has a restricted 128-byte syntax. Cardinality is a security and availability control because unbounded series can exhaust memory, storage, compaction, and query capacity.

That validation applies only to static `PYROSCOPE_TAGS`. Go applications can independently attach sample labels through `runtime/pprof.Labels`, `runtime/pprof.Do`, and `runtime/pprof.WithLabels`; Pyroscope integrations can also use `pyroscope.TagWrapper`. Inventory all such call sites before rollout, prohibit sensitive, user-controlled, or unbounded label values, and inspect the labels actually present in canary exports. Source/configuration review alone is not sufficient evidence.

## Network and authentication architecture

Pyroscope OSS does not include an authentication layer. Production requires a company-controlled authenticating reverse proxy or gateway in front of every upload and query route.

The gateway must:

- authenticate workloads through an approved machine identity and humans through company SSO or the Grafana access layer;
- authorize application/tenant writes and user/tenant reads separately;
- terminate TLS with an approved certificate and TLS policy;
- set or overwrite `X-Scope-OrgID` after authorization when multitenancy is enabled;
- reject caller attempts to choose an unauthorized tenant;
- enforce request-size, timeout, connection, and rate limits;
- produce sanitized, access-controlled audit logs;
- deny all unrecognized methods and routes.

The backend must not be reachable directly from application namespaces, user networks, public ingress, NodePorts, or external load balancers. NetworkPolicy and firewall rules should allow only:

- application workloads to the gateway write endpoint;
- Grafana/authorized operators to the gateway query endpoint;
- gateway to the exact Pyroscope services it proxies;
- Pyroscope components to their documented peers, DNS, metrics collector, persistent volumes, and object store;
- no optional internet egress, including anonymous usage reporting.

The client uses a direct transport: proxy environment variables are ignored and HTTP redirects are rejected. Response headers are limited to 32 KiB and decoded response bodies to 64 KiB; the body limit is enforced after transparent gzip decompression and covers chunked/unknown-length responses. This bounds the upstream SDK's response read even if a compromised or faulty gateway returns an oversized error. Validate DNS, the internal CA trust chain, firewall/NetworkPolicy, and the observed destination so a process-wide proxy cannot become an unreviewed profile-data path.

The supplied deployment disables Pyroscope anonymous usage statistics. Verify this after every upgrade.

The supplied Pyroscope values also keep `auth.multitenancy-enabled=false`. If the organization needs multiple trust boundaries, enable `auth.multitenancy-enabled=true` only after the gateway controls the entire tenant-header boundary described above. The production security test must verify that missing and spoofed `X-Scope-OrgID` values are rejected and that a credential authorized for tenant A cannot write to or query tenant B.

## TLS

Use HTTPS for production uploads and queries. Certificates must have the correct DNS SAN, be rotated through the company certificate system, and chain to a CA trusted by the application image and Grafana.

Never:

- set TLS insecure-skip-verify;
- put credentials in a URL;
- downgrade to plaintext because of a certificate incident;
- expose an internal plaintext service beyond its tightly controlled namespace/mesh boundary.

If a service mesh terminates mTLS, document both trust boundaries and prove traffic cannot bypass the sidecar/gateway path.

## Secrets

No static secret belongs in this repository, a ZIP handoff, ordinary Helm values, rendered evidence, logs, infrastructure state, container layers, or shell history.

Use the company's approved secret manager and delivery controller. Kubernetes manifests may reference an existing Secret by name/key but must not contain the secret value. Prefer the wrapper's password-file option over a plaintext process environment variable when Basic authentication is the approved gateway mechanism. Mount the file read-only with least privilege and rotate it through a rolling workload replacement.

Separate credentials for:

- each application or trust boundary uploading profiles;
- Pyroscope access to the S3-compatible bucket;
- Grafana/query access;
- gateway administration and TLS private keys.

Do not reuse object-store credentials as application credentials. Scope each identity to the minimum route, tenant, bucket, prefix, and operation set. Record owner, creation, rotation, revocation, and break-glass procedure.

## Kubernetes controls

Production manifests should meet the company restricted-workload baseline:

- dedicated namespace and service accounts without unnecessary token mounts;
- non-root user/group, no privilege escalation, dropped capabilities, and seccomp `RuntimeDefault`;
- read-only root filesystem where component behavior permits;
- explicit resource requests/limits and ephemeral-storage planning;
- topology spread/anti-affinity and pod disruption budgets for HA components;
- NetworkPolicy default deny plus exact ingress/egress;
- immutable image digest, approved registry, signature/provenance verification, and vulnerability scan;
- dedicated persistent volume claims with approved encryption, access mode, snapshot, and recovery policy;
- no privileged pods, host PID/network, hostPath, or kernel capabilities for this push-SDK architecture.

RBAC to change Pyroscope, gateway, tenant, retention, storage, or Grafana data-source configuration must be limited and audited.

## Object storage and persistence

Production uses a company-operated or approved internal S3-compatible service. The storage owner must validate compatibility with the pinned Pyroscope release.

Required controls:

- internal TLS endpoint and encrypted storage;
- dedicated bucket/prefix and least-privilege identity;
- versioning/immutability decisions aligned with deletion and legal hold policy;
- redundancy and availability targets;
- monitored capacity, latency, errors, request rates, and replication health;
- bucket policy and access logging;
- retention coordinated with Pyroscope compaction and deletion;
- backup and isolated restore tests against documented RPO/RTO.

Do not use an object-store lifecycle rule to delete blocks earlier than Pyroscope retention expects. Do not use local filesystem storage for the production microservices topology. Single-node local/PVC storage is evaluation only.

## Availability and abuse controls

Protect business workloads and the profiling platform from overload:

- keep profiling disabled in the shared base deployment;
- canary before broad enablement;
- keep mutex and block profiling opt-in;
- use the default upload rate until measured evidence justifies a change;
- enforce gateway request and rate limits while preserving clear 429 telemetry;
- monitor high cardinality, ingestion growth, queue/backlog, compaction, storage, and query fan-out;
- define per-tenant limits where multiple teams share the backend;
- separate write and query scaling and prevent expensive queries from starving ingestion.

Application profiler failures should fail open unless a service-specific availability policy explicitly requires otherwise. Backend compromise or overload must be containable by disabling uploads at the gateway and replacing enabled application processes.

## Supply chain

Pin and review:

- the Go toolchain and all modules;
- `github.com/grafana/pyroscope-go`;
- Pyroscope server and gateway images by digest;
- the official Helm chart version and its rendered images;
- GitHub Actions by immutable commit;
- Grafana and data-source/plugin versions if managed by this rollout.

A normal build or Helm install can fetch these artifacts from public registries even though the runtime has no cloud-provider dependency. Because `go.mod` selects Go 1.25.13, an older Go installation with automatic toolchain selection can also download that toolchain. The Makefile's pinned `govulncheck v1.6.0` command can separately download `golang.org/x/vuln` and build-time transitive modules that are not part of this library's `go.mod` graph. Controlled and air-gapped environments must preinstall or internally distribute the exact approved toolchain and vulnerability scanner (using `make verify GOVULNCHECK=govulncheck` for a preinstalled binary), or mirror that scanner's tool module graph for the default target; set `GOTOOLCHAIN=local` where public automatic downloads are forbidden, mirror the approved application module graph, chart, images, and vulnerability metadata into internal systems, verify checksums/signatures/provenance before promotion, disable usage statistics, and deny unapproved runtime internet egress.

For every release archive:

- source commit and semantic tag;
- module checksums and `govulncheck` result;
- container scan and signature/provenance result;
- rendered manifest and policy result;
- SBOMs for the library/application and every deployed image;
- third-party license inventory and Legal/OSPO decision;
- approved exceptions with owner and expiry.

## License compliance

The wrapper and `pyroscope-go` client are Apache License 2.0. The separately deployed Pyroscope server is AGPL-3.0. Grafana components can have their own license terms and must be inventoried separately. The pinned baseline is recorded in [THIRD_PARTY-NOTICES.md](../THIRD_PARTY-NOTICES.md).

Legal/OSPO must decide the organization's policy for running, modifying, distributing, and offering network access to AGPL-covered components. Preserve upstream license and attribution files and maintain corresponding-source/build records required by that decision. Do not merge or copy AGPL server code into this Apache-licensed package without a separately approved legal and architectural change.

This document does not interpret license obligations for a particular jurisdiction or deployment.

## Access and audit

- Give profile viewing access through managed groups and least-privilege Grafana roles.
- Separate upload identity, query identity, platform administration, storage administration, and security audit roles.
- Review memberships, inactive credentials, and tenant mappings on a defined cadence.
- Audit gateway access, authorization denials, tenant changes, retention changes, exports, storage policy changes, and administrative queries as required by policy.
- Protect audit logs from the platform they audit and retain them for the approved period.
- Test that unauthorized uploads, queries, and cross-tenant requests fail before production.

## Production approval evidence

Production approval requires all of the following:

- completed site-input table and named owners in [HANDOFF.md](HANDOFF.md);
- architecture/security/data/license approval;
- exact-version CI, vulnerability, image, SBOM, and manifest evidence;
- authenticated TLS gateway and isolation tests;
- S3-compatible storage compatibility, capacity, backup, and restore evidence;
- representative-load canary within application and backend thresholds;
- profile content/cardinality review;
- application disable/restart rehearsal and compatible backend rollback plan;
- on-call dashboards, alerts, support, and incident procedures.

## External references

- [Pyroscope server API authentication warning](https://grafana.com/docs/pyroscope/latest/reference-server-api/)
- [Pyroscope deployment modes](https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/deployment-modes/)
- [Object storage configuration](https://grafana.com/docs/pyroscope/latest/configure-server/storage/configure-object-storage-backend/)
- [Pyroscope server AGPL-3.0 license](https://github.com/grafana/pyroscope/blob/main/LICENSE)
- [Pyroscope Go client Apache-2.0 license](https://github.com/grafana/pyroscope-go/blob/main/LICENSE)
- [OpenTelemetry Profiles Alpha specification](https://opentelemetry.io/docs/specs/otel/profiles/)
