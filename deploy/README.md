# Self-hosted deployment assets

Everything under `deploy/` runs on infrastructure controlled by the company. No
Google Cloud project, cloud API, provider IAM, GSA, or provider-managed profiler
is required.

| Path | Purpose | Intended use |
| --- | --- | --- |
| `docker-compose/` | Pinned Pyroscope and Grafana containers, loopback-only ports, local persistent volumes, and an optional smoke workload. | Laptop/evaluation only. |
| `helm/values-single-binary.yaml` | One Pyroscope process with a PVC and conservative resources. | Isolated evaluation or non-production staging only. |
| `helm/values-production-ha.example.yaml` | Pyroscope v2 microservices with three-node metastore and external S3-compatible object storage. | Production design input; site values and secrets are mandatory. |
| `kubernetes/` | Namespace, application patch, auth overlay, and deny-by-default network policies. | Merge into the company's Helm/Kustomize/GitOps source. |

The selected server is Grafana Pyroscope OSS `2.2.0`. The Kubernetes path pins
the official `grafana/pyroscope` chart at `2.2.0`; the chart archive SHA-256 is
recorded in `helm/SHA256SUMS`. Container tags are also pinned to multi-platform
manifest digests.

Pyroscope v2 requires a Kubernetes ServiceAccount token and a namespace-scoped,
read-only discovery Role for its own Pods, Services, Endpoints, and
EndpointSlices. The official chart creates both; this is cluster-internal
Kubernetes RBAC and does not require a cloud-provider identity or API.

The single-binary layout is deliberately efficient but has one failure domain.
Do not scale it horizontally: replicas do not share a local filesystem. For
production HA, use the microservices example with a company-operated
S3-compatible service such as MinIO or Ceph RGW, durable metastore PVCs, and
capacity sizing based on a representative load test.

Pyroscope OSS does not provide an authentication boundary by itself. Keep its
Service private, apply the supplied NetworkPolicies, and place a company-approved
TLS/auth gateway or service mesh in front of it before enabling production
clients. Never commit object-store or gateway credentials.
