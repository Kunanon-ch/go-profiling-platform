# Local evaluation with Docker Compose

This stack is for a developer workstation only. It starts Pyroscope `2.2.0`
and Grafana `13.1.0`, persists their data in named Docker volumes, and publishes
both ports only on the loopback interface through an unprivileged NGINX
`1.29.5` gateway. It does not call a cloud provider.
Anonymous Pyroscope/Grafana usage reporting, update checks, automatic plugin
installation/update, and Pyroscope tracing are disabled.

## Start

From this directory:

```powershell
Copy-Item .env.example .env
```

Edit `.env` and set a unique `GRAFANA_ADMIN_PASSWORD`. Do not commit `.env`.
Then run:

```powershell
docker compose config --quiet
docker compose pull
docker compose up -d pyroscope grafana gateway
$ready = $false
for ($attempt = 1; $attempt -le 24; $attempt++) {
  curl.exe --fail --silent --show-error http://127.0.0.1:4040/ready
  if ($LASTEXITCODE -eq 0) { $ready = $true; break }
  Start-Sleep -Seconds 5
}
if (-not $ready) { throw "Pyroscope was not ready after 120 seconds" }
```

Open <http://127.0.0.1:3000>, sign in as `admin` with the password from `.env`,
and open **Drilldown > Profiles**. Grafana 13 includes Profiles Drilldown and
the Pyroscope data source is provisioned automatically.

## Generate a real profile

The optional `smoke` service uses the repository's `cmd/pyroscope-smoke` demo.
Run it after Pyroscope is ready:

```powershell
docker compose --profile smoke up --build --no-deps smoke
```

Leave it running for at least 30 seconds, refresh Grafana, and select
`profilerprobe-smoke`. Stop the foreground process with `Ctrl+C`. `--no-deps`
keeps Pyroscope running while the smoke process performs its final flush.

To verify ingestion independently of the UI, run this in another PowerShell
window after the first 30 seconds:

```powershell
$now = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$body = @{ name = "service_name"; start = $now - 3600000; end = $now } |
  ConvertTo-Json -Compress
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:4040/querier.v1.QuerierService/LabelValues `
  -ContentType application/json -Body $body
```

The response must include `profilerprobe-smoke`. Its CPU flame graph contains
recognizable `cpuWorker`, `burnCPU`, and `mix` frames; the allocation profiles
contain `allocateBuffer`.

The multi-stage build downloads modules before the runtime joins the private
Compose network. The resulting runtime is a static binary in a `scratch` image,
runs as non-root, and has no shell or package manager. It deliberately permits
clear-text HTTP only to the Pyroscope container on that network. The library's
normal default rejects insecure HTTP; do not copy this exception into a
production deployment.

The smoke container has a 60-second stop grace period so the wrapper can flush
the last profile after its workload shutdown. Keep at least 45 seconds for that
profiler flush in addition to any real application's own drain budget.

## Stop or erase

```powershell
docker compose --profile smoke stop smoke
docker compose down
```

Stopping `smoke` first preserves the backend while its final profile flushes.

`down` retains profiles and Grafana settings. To permanently delete the local
evaluation data:

```powershell
docker compose --profile smoke stop smoke
docker compose down --volumes
```

The Compose stack is not an HA or production topology. It has one Pyroscope
process, local filesystem storage, no server authentication, and a 24-hour
retention period. Pyroscope, Grafana, and the smoke workload are attached only to
an `internal: true` Docker network and therefore have no runtime internet route.
The static NGINX gateway joins that network and a normal bridge solely so Docker
Desktop can publish loopback ports; it has no telemetry, dynamic upstream, DNS
resolver, package action, or application credential. A host firewall may deny
the gateway's residual network capability when evaluation policy requires it.
