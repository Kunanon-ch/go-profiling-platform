# Kubernetes integration

These manifests contain no cloud-provider resources or credentials. Pyroscope
runs on the company's existing Kubernetes compute and storage.

## What each file does

| File | Purpose |
| --- | --- |
| `namespace.yaml` | Dedicated namespace with the Restricted Pod Security Standard enforced. |
| `deployment-profiler-patch.yaml` | Disabled-by-default application patch using the exact `PYROSCOPE_*` contract. |
| `deployment-auth-overlay.example.yaml` | Optional file-mounted Basic Auth secret for an approved HTTPS gateway. |
| `networkpolicy-pyroscope.yaml` | Default deny; permits peers/control-plane traffic and gateway access only to HA distributor/query-frontend Pods. |
| `networkpolicy-application-egress.example.yaml` | Restricts an application to the gateway's named HTTPS port, never backend port 4040. |
| `networkpolicy-grafana-egress.example.yaml` | Restricts Grafana to the same authenticated HTTPS gateway. |
| `networkpolicy-gateway.example.yaml` | Allows clients into the gateway and limits its backend egress to HA distributor/query-frontend Pods. |
| `networkpolicy-single-binary-gateway.example.yaml` | Non-production exception for the chart's one `component=all` Pod. |
| `networkpolicy-monitoring-ingress.example.yaml` | Optional fail-closed exception for one trusted Prometheus/monitoring identity. |
| `networkpolicy-object-storage-egress.example.yaml` | HA allowance to a label-selected self-hosted S3-compatible service. |

## Application patch

The patch is not a standalone Deployment. Merge it into the application's
existing Helm/Kustomize/GitOps source after replacing:

- `REPLACE_WITH_DEPLOYMENT_NAME`
- `REPLACE_WITH_APPLICATION_NAMESPACE`
- `REPLACE_WITH_APPLICATION_NAME`
- `REPLACE_WITH_CONTAINER_NAME`
- `REPLACE_WITH_IMMUTABLE_RELEASE`
- `REPLACE_WITH_ENVIRONMENT`
- `REPLACE_WITH_INTERNAL_PYROSCOPE_GATEWAY`
- `REPLACE_WITH_TERMINATION_GRACE_SECONDS`

Keep `PYROSCOPE_ENABLED=false` in the shared base. The HTTPS endpoint must be an
internal, authenticated gateway or service-mesh address. Enable only a reviewed
staging/canary overlay after the gateway, NetworkPolicy, retention, and service
SLO checks are ready. A rollout is required to start or stop profiling.

The efficient baseline records CPU, allocated bytes, in-use bytes, and
goroutines. Object-count profiles are off to avoid duplicate heap views; mutex
and block profiles are off because enabling runtime sampling changes overhead.
`PYROSCOPE_DISABLE_GC_RUNS=true` prevents forced garbage collection before heap
snapshots. `PYROSCOPE_HTTP_TIMEOUT=5s` bounds each upload call. The supplied
termination-grace sentinel must be replaced with an integer equal to the
measured application drain plus at least 45 seconds for the profiler's
synchronous final flush at the default timeout. Never lower a larger grace value
already owned by the application. Increase the profiler budget when using a
higher HTTP timeout, and verify a terminating Pod drains and flushes cleanly
before rollout. Change one profiling flag at a time during a bounded
investigation.

The library rejects clear-text HTTP unless
`PYROSCOPE_ALLOW_INSECURE_HTTP=true`. That exception is used only by the isolated
Docker Compose smoke test. Do not enable it in a production overlay.

## Authentication secret

Pyroscope OSS does not authenticate users or writers by itself. Terminate TLS
and enforce identity at a company gateway/service mesh. If the gateway requires
Basic Auth, create `pyroscope-client-auth` through the approved secret manager
with keys `username` and `password`, then merge
`deployment-auth-overlay.example.yaml`. The password is mounted as a read-only
file and is never placed directly in an environment variable or this repository.
The Secret volume uses read-only mode `0444` because Kubernetes mounts Secret
files as root-owned and the consuming application UID is intentionally unknown;
the volume is mounted only into the application container. If company policy
requires group-only `0440`, set a reviewed pod `fsGroup` that matches the
application's runtime group instead of copying an arbitrary group ID.

Do not use both `PYROSCOPE_BASIC_AUTH_PASSWORD` and
`PYROSCOPE_BASIC_AUTH_PASSWORD_FILE`; the wrapper rejects that ambiguous setup.

## Network policy

Edit the gateway namespace/name selector in `networkpolicy-pyroscope.yaml`,
render the complete release, and then apply it. Unmodified placeholders match no
real namespace, so client traffic remains blocked. Confirm the cluster CNI
enforces Kubernetes NetworkPolicy.

The production path is deliberately one-way:

```text
application / Grafana --HTTPS + auth--> gateway --HTTP :4040--> distributor / query-frontend
```

Use `networkpolicy-application-egress.example.yaml`,
`networkpolicy-grafana-egress.example.yaml`, and
`networkpolicy-gateway.example.yaml` together. The gateway Pod must declare its
TLS listener as the named container port `https`; this lets the policies work
when the site maps that name to 443, 8443, or another reviewed port. No supplied
policy permits application or Grafana Pods to reach backend port 4040. If the
gateway needs OIDC/introspection, add only the identity provider's exact egress;
do not add unrestricted internet egress. Confirm bypass prevention with a denied
direct-backend test from both client namespaces before enabling profiling.

Replace every site selector, including `REPLACE_WITH_GATEWAY_NAMESPACE`,
`REPLACE_WITH_GATEWAY_NAME`, `REPLACE_WITH_APPLICATION_NAMESPACE`,
`REPLACE_WITH_APPLICATION_NAME`, `REPLACE_WITH_GRAFANA_NAMESPACE`, and
`REPLACE_WITH_GRAFANA_NAME`. The monitoring and object-storage examples add
their own explicit sentinels. Run the preflight `rg` command and require zero
matches in the rendered company overlay; do not assume a corporate workload
uses the example `app.kubernetes.io/name` value.

For production HA, the gateway must route ingest/push endpoints to the
`distributor` Service and query/render endpoints to the `query-frontend` Service.
Both gateway ingress and egress policies match only those two component labels;
they do not grant gateway access to metastore, segment-writer, admin, compaction,
or query-backend Pods. Verify those denied paths from the gateway namespace.
The single-binary chart renders one `app.kubernetes.io/component=all` Pod, so an
isolated non-production install also needs
`networkpolicy-single-binary-gateway.example.yaml`. Never apply that exception
to the HA release.

Metrics scraping is disabled by default. If the company enables a ServiceMonitor
or Prometheus scrape, replace both selectors in
`networkpolicy-monitoring-ingress.example.yaml` and apply it only for a tightly
controlled monitoring identity. Kubernetes NetworkPolicy cannot authorize an
HTTP path: TCP 4040 also exposes ingest/query/admin endpoints, not only
`/metrics`. Prefer an mTLS service-mesh policy or metrics gateway that permits
only `GET /metrics`; otherwise treat the selected monitoring Pods as trusted
backend principals and prove unselected Pods remain denied.

Pyroscope v2 uses `kubernetes:///` discovery for its metastore. The official
chart therefore creates a dedicated ServiceAccount plus a namespace-scoped
Role/RoleBinding with read-only `get`, `list`, and `watch` access to Pods,
Services, Endpoints, and EndpointSlices. Its projected API token must remain
mounted; this is Kubernetes control-plane access, not cloud IAM. The egress
rule's `192.0.2.0/32` value is a deliberately unreachable documentation address.
Before applying the policy, replace it with the exact Kubernetes API Service IP
(`/32`) or the required control-plane CIDR for the target cluster. Replace the
port as well: `443` is the fail-closed API Service default, while a CNI enforcing
after DNAT may observe the control-plane endpoint port (commonly `6443`). Because
Service-IP DNAT and NetworkPolicy enforcement order differs by CNI, confirm both
the destination and port observed by that CNI; where needed, use its native
service/FQDN egress policy instead. Never broaden this rule to `0.0.0.0/0`.

The DNS rule assumes CoreDNS pods carry `k8s-app=kube-dns`. Clusters using
NodeLocal DNSCache or different labels need a reviewed equivalent. For external
S3-compatible endpoints, enforce exact egress through the company's CNI,
firewall, or egress gateway because standard NetworkPolicy cannot select FQDNs.

## Required preflight

```sh
# Inventory the sentinels shipped in these reference templates.
rg -n 'REPLACE_WITH_' deploy/kubernetes deploy/helm
# The final company render must contain neither a sentinel nor the fail-closed
# documentation-only API address.
rg -n 'REPLACE_WITH_|192[.]0[.]2[.]0/32' <rendered-company-manifests>  # must print nothing
kubectl apply --dry-run=client -f deploy/kubernetes/namespace.yaml
kubectl apply --dry-run=server -f <rendered-and-approved-manifests>
```

The final command requires access to the target cluster and its admission
policies. A local YAML parse cannot prove StorageClass availability, CNI
enforcement, gateway certificates, DNS behavior, Secret injection, scheduling,
or S3 connectivity.
