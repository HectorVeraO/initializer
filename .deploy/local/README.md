# Local stack

Port band `101xx` (10100-10199). Every host port of this repo comes from this band.

| Port | Service | Runs on |
|---|---|---|
| 10100 | `go-srv` HTTP API and `/kratos/*` proxy (`GO_SRV_ADDR`) | host |
| 10120 | `ng-web` dev server (`ng serve --port 10120`), the default public origin | host |
| 10132 | `postgres` | Docker |
| 10133 | `kratos` public API (`127.0.0.1` only) | Docker |
| 10134 | `kratos` admin API (`127.0.0.1` only, never proxied) | Docker |

## Start and stop

```bash
cp .deploy/local/.example.env .deploy/local/.env
docker compose -f .deploy/local/docker-compose.yml up -d
set -a; . .deploy/local/.env; set +a
mise exec -- go -C go-srv run ./cmd/server
docker compose -f .deploy/local/docker-compose.yml down -v
```

- go-srv applies its own migrations at startup.
- `postgres-init` runs on every `up`: an existing volume gains the `kratos` role and database without `down -v`.
- `kratos-migrate` applies the Kratos migrations before `kratos` starts.
- `go run` from `go-srv/` resolves relative path config values against `go-srv/`.
- Kratos readiness: `http://localhost:10133/health/ready` (public), `http://localhost:10134/admin/health/ready` (admin).
