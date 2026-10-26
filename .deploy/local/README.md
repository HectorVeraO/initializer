# Local stack

Port band `101xx` (10100-10199). Every host port of this repo comes from this band.

| Port | Service | Runs on |
|---|---|---|
| 10100 | `go-srv` HTTP API (`GO_SRV_ADDR`) | host |
| 10120 | `ng-web` dev server (`ng serve --port 10120`) | host |
| 10132 | `postgres` | Docker |
| 10133-10134 | reserved: identity provider public / admin API | Docker |

## Start and stop

```bash
cp .deploy/local/.example.env .deploy/local/.env
docker compose -f .deploy/local/docker-compose.yml up -d
docker compose -f .deploy/local/docker-compose.yml down -v
```
