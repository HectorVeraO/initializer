# Local stack

Host ports 10100-10199. Every published port comes from this range and binds `127.0.0.1`.

<!-- initializer:stack go-srv -->
| Port | Service | Runs on |
|---|---|---|
| 10100 | `go-srv` HTTP API (`GO_SRV_ADDR`) | Docker or host |
| 10132 | `postgres` | Docker |
<!-- initializer:end go-srv -->
<!-- initializer:module identity -->
| Port | Service | Runs on |
|---|---|---|
| 10133 | `kratos` public API (`go-srv` on 10100 also proxies `/kratos/*` to it) | Docker |
| 10134 | `kratos` admin API (never proxied) | Docker |
<!-- initializer:end identity -->

<!-- initializer:stack ng-web -->
The console adds:

| Port | Service | Runs on |
|---|---|---|
| 10120 | `ng-web` console, the public origin: nginx in Docker, or the dev server on the host | Docker or host |
<!-- initializer:end ng-web -->

## Run

```bash
mise run up      # build and start everything in Docker
mise run down    # stop everything; volumes are kept
mise run infra   # infrastructure only, for host-dev
```

- Without mise: `docker compose -f .deploy/local/docker-compose.yml --profile app up -d --build`, and the same with `down`.
- Reset the data: `docker compose -f .deploy/local/docker-compose.yml --profile app down -v`.
<!-- initializer:stack go-srv -->
- No `.env` is needed: the defaults are local-only placeholders. Overrides go in `.deploy/local/.env` (optional, gitignored, keys as in `.example.env`); compose reads it for its `${...}` values and passes it to `go-srv`, and the host-dev tasks read `.example.env`, then `.env`.
- go-srv applies its own migrations at startup.
- Both modes run go-srv from `go-srv/` (the container's working directory is `/project/go-srv`), so relative path values resolve against `go-srv/`.
<!-- initializer:end go-srv -->
<!-- initializer:module identity -->
- `postgres-init` runs on every `up`: an existing volume gains the `kratos` role and database without `down -v`.
- `kratos-migrate` applies the Kratos migrations before `kratos` starts.
- Kratos readiness: `http://localhost:10133/health/ready` (public), `http://localhost:10134/admin/health/ready` (admin).
<!-- initializer:end identity -->

## Edit a component on the host

Stop its container, then run it on the host on the same port:

<!-- initializer:stack go-srv -->
```bash
docker compose -f .deploy/local/docker-compose.yml stop go-srv
mise run dev:srv
```
<!-- initializer:end go-srv -->
<!-- initializer:stack ng-web -->

The console in Docker reaches only the `go-srv` container, so editing `go-srv` runs the console on the host too: stop `go-srv ng-web`, then run `mise run dev:srv` and `mise run dev:web`, each in its own terminal. Editing only the console: stop `ng-web`, then `mise run dev:web`. Once per clone: `npm --prefix ng-web ci`.
<!-- initializer:end ng-web -->
<!-- initializer:module feature-flags -->

## Feature flags

`APP_FLAGS_FILE` is the path of the `flags.yaml` manifest, required when the `feature-flags` module is installed (`../flags.yaml` from `go-srv/`, set by `.example.env`). A relative path resolves against the process working directory. A missing, unreadable or invalid file stops startup with exit `1` and every problem listed.
<!-- initializer:end feature-flags -->

<!-- initializer:module identity -->
## Session keys

`APP_SESSION_KEYS` is a comma-separated keyring of `<kid>:<seed>` entries (`kid` matches `^[A-Za-z0-9._-]{1,32}$`, seed is 32 random bytes in unpadded base64url). The first entry signs; every entry verifies.

- Generate a seed: `openssl rand -base64 32 | tr '+/' '-_' | tr -d '='`
- Local only: `<kid>:passphrase:<24 to 128 characters>` derives the seed from the text (no commas); `Build` rejects it when `APP_PUBLIC_BASE_URL` is `https`.
- Rotation: add the new key as a second entry and deploy; move it first and deploy; after `APP_SESSION_REFRESH_MAX_AGE` remove the old key.
- Emergency rotation: replace the whole keyring; every token answers `401 SESSION_INVALID`.
<!-- initializer:end identity -->
