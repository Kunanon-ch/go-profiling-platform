# Pyroscope Helm deployment

This directory pins the official `grafana/pyroscope` chart and application at
`2.2.0`. The image is pinned to multi-platform manifest digest
`sha256:7f019d866e474609c7f112d2632d75bdf8ad418c08461fcb5193444bb6dc476d`.
The expected chart archive SHA-256 is in `SHA256SUMS`.

## Verify the chart supply chain

```sh
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm pull grafana/pyroscope --version 2.2.0
sha256sum --check SHA256SUMS
mkdir -p .chart-cache
tar -xzf pyroscope-2.2.0.tgz -C .chart-cache
helm show chart grafana/pyroscope --version 2.2.0
```

The chart must report both `version: 2.2.0` and `appVersion: 2.2.0`. Store the
chart in the company's approved artifact registry if public-registry access is
not permitted during deployment.

## Single-binary baseline

This is the lowest-operational-cost option. It persists to one PVC but is not
highly available and must remain at one replica.

```sh
kubectl apply -f ../kubernetes/namespace.yaml
helm lint .chart-cache/pyroscope \
  --values values-single-binary.yaml
helm template pyroscope .chart-cache/pyroscope \
  --namespace pyroscope-system --values values-single-binary.yaml
helm upgrade --install pyroscope .chart-cache/pyroscope \
  --namespace pyroscope-system --values values-single-binary.yaml \
  --atomic --timeout 10m
```

After installation, apply `../kubernetes/networkpolicy-pyroscope.yaml` only
after replacing its gateway selector and the fail-closed Kubernetes API CIDR.
Verify:

```sh
kubectl -n pyroscope-system rollout status statefulset/pyroscope --timeout=10m
kubectl -n pyroscope-system get pods,service,pvc,networkpolicy
kubectl -n pyroscope-system port-forward service/pyroscope 4040:4040
curl --fail http://127.0.0.1:4040/ready
```

If the rendered controller is a Deployment rather than StatefulSet for a future
chart, use `kubectl rollout status deployment/pyroscope` after reviewing the
chart diff. Never increase the replica count while filesystem storage is used.

## Production HA

`values-production-ha.example.yaml` uses Pyroscope v2 microservices. It requires:

- a company-operated S3-compatible endpoint reachable over validated HTTPS;
- `PYROSCOPE_S3_ENDPOINT` in `hostname:port` form only, without `https://` or a
  URL path; `insecure: false` in the values file enables TLS;
- a dedicated bucket with lifecycle, backup, quota, and restore ownership;
- an externally managed `pyroscope-s3` Secret containing exactly
  `PYROSCOPE_S3_ENDPOINT`, `PYROSCOPE_S3_REGION`, `PYROSCOPE_S3_BUCKET`,
  `PYROSCOPE_S3_ACCESS_KEY_ID`, and `PYROSCOPE_S3_SECRET_ACCESS_KEY`;
- a real replacement for `REPLACE_WITH_STORAGE_CLASS`;
- a currently vendor-supported Kubernetes release that is compatible with the
  pinned chart; CI checks the Kubernetes 1.30 API schema as the minimum
  compatibility floor required by the HA overlay's stable `minDomains`, but the
  complete render must also be validated against the company's actual target
  cluster version;
- at least three schedulable nodes for the three-replica components, with
  consistent zone/rack labels strongly recommended;
- load-tested requests/limits, retention, and capacity alarms;
- internal TLS/auth gateway or service-mesh policy for ingest and query access.

The object-store certificate must have a matching DNS SAN and chain to a CA
trusted inside every Pyroscope container. If the company uses a private CA,
mount the approved CA bundle read-only through a reviewed overlay and point the
Go runtime at that bundle with `SSL_CERT_FILE`; do not work around trust failures
by changing `insecure` to `true`.

Every replicated component has component-specific topology constraints. The
hostname rule is hard (`DoNotSchedule` + `minDomains`), so three-replica
components require three eligible nodes and two-replica components require two.
The zone rule is best-effort (`ScheduleAnyway`) so an on-premises cluster without
zone labels does not leave the whole stack Pending. Sites that require strict
cross-zone or cross-rack placement must replace that topology key and promote
the rule to `DoNotSchedule` with the required `minDomains` in their reviewed
overlay. Validate node labels and capacity before installation.

The bundled MinIO subchart is intentionally disabled: its chart defaults are an
evaluation topology and include static example credentials. Operate MinIO, Ceph
RGW, or another S3-compatible store as a separate governed service.

Before installation, copy the example to a company overlay, replace all
sentinels, inject the Secret through the approved secret manager, and render it:

```sh
helm template pyroscope .chart-cache/pyroscope \
  --namespace pyroscope-system --values values-production-ha.yaml
```

Review every Service, StatefulSet, PVC, PodDisruptionBudget, resource value,
security context, and object-storage setting before `helm upgrade --install`.
Without the external Secret, Pyroscope containers fail configuration/startup.
An unknown storage-class sentinel is normally accepted by Kubernetes but leaves
PVCs, and therefore dependent Pods, `Pending`; admission rejects it only when a
site policy explicitly does so. The release preflight and policy engine must
reject every unresolved `REPLACE_WITH_*` value before installation rather than
relying on Kubernetes admission to catch it.

## Security boundary

The chart keeps every Service as `ClusterIP` and creates no Ingress. Pyroscope
OSS is not the identity/authentication boundary. In production, clients must use
an approved internal HTTPS endpoint protected by an authenticating reverse
proxy, API gateway, or service mesh. The proxy may forward Basic Auth to the Go
client configuration, but TLS must not be disabled. Multi-tenancy headers are
tenant routing, not user authentication.

Pyroscope v2 also uses the Kubernetes API for `kubernetes:///` metastore
discovery. Both values files intentionally enable the official chart's
ServiceAccount and namespace-scoped discovery Role/RoleBinding. The mounted
token and read-only `get/list/watch` permission for Pods, Services, Endpoints,
and EndpointSlices are required for startup; they do not grant provider IAM.
When deny-by-default egress is enabled, replace the fail-closed TEST-NET API CIDR
and port in `../kubernetes/networkpolicy-pyroscope.yaml` with the exact API
destination observed by the target CNI before applying that policy. The Service
path usually uses TCP 443; post-DNAT enforcement commonly sees TCP 6443.

Both supplied modes explicitly start in single-tenant mode with
`auth.multitenancy-enabled: "false"`. For multiple company trust boundaries,
set it to `"true"` only in a reviewed site overlay. The gateway must authenticate
every writer/reader and overwrite (not merely forward) `X-Scope-OrgID` from the
trusted identity mapping; the application `PYROSCOPE_TENANT_ID` and Grafana data
source must match that mapping. Before rollout, prove that tenant A cannot push
or query tenant B, and that missing, forged, and mismatched tenant headers are
rejected. A tenant header by itself is not authentication.

Apply the deny-by-default NetworkPolicies in `../kubernetes`. Application writers
and Grafana readers must connect to the authenticating gateway's named HTTPS port;
in HA, that gateway may reach only distributor and query-frontend Pods on port
4040. Route writes to distributor and reads to query-frontend; metastore,
segment-writer, admin, compaction-worker, and query-backend remain unreachable
from the gateway. Also allow only DNS, Pyroscope peer/API discovery traffic, and
the exact object-store endpoint. The non-production single-binary baseline needs
the separate `networkpolicy-single-binary-gateway.example.yaml` exception for
its one `component=all` Pod.
If the S3 endpoint is selected by IP or FQDN rather than Kubernetes labels,
implement the equivalent egress rule with the company's CNI/firewall; standard
NetworkPolicy cannot safely select an FQDN.

`serviceMonitor.enabled` remains `false` until the company's monitoring owner
supplies selectors and ownership. When enabling a scrape, use the fail-closed
`networkpolicy-monitoring-ingress.example.yaml` or an mTLS mesh policy restricted
to `GET /metrics`. A plain NetworkPolicy allowance to port 4040 grants the
selected monitoring Pods network access to more than metrics, so that identity
must be tightly controlled and trusted.
