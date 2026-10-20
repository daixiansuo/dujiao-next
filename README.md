# Dujiao-Next

Dujiao-Next is a digital goods e-commerce platform. This repository contains the complete
application: the Go backend, the customer storefront, and the admin panel.

## ❤️ Brand Partners (Sponsors)

<table>

<tr>
<td width="180"><a href="https://www.vmrack.net/?ref_code=5iXmGUMf5f5"><img src="assets/partners/vmrack.jpeg" alt="CCTK.AI" width="150"></a></td>
<td><a href="https://www.vmrack.net/?ref_code=5iXmGUMf5f5">Vmrack.com</a> 全球自动化云基础设施服务商 提供先进的云服务器、裸金属、CDN、媒体处理、对象存储和网络解决方案，助力企业轻松上云。
⚡️官方合作闪购款，仅需76刀/年，三网优化线路，助力您业务起飞，<a href="https://www.vmrack.net/vps/flash-deals/2082383856451452928?ref_code=5iXmGUMf5f5">👉点我直达</a>
</td>
</tr>

<tr>
<td width="180"><a href="https://www.99cdn.com/"><img src="assets/partners/99cdn.jpg" alt="openmodel" width="150"></a></td>
<td>99CDN 自建 CDN 平台，自主管理节点 · 智能流量调度 · 多级缓存加速。 <a href="https://www.99cdn.com/">99CDN</a> 是 EasyLink 旗下的商业化自建 CDN 与 DNS 智能调度平台，支持边缘缓存、分片缓存、多级回源、GTM 调度与边缘计算能力。</td>
</tr>

<tr>
<td width="180"><a href="https://niub.me"><img src="assets/partners/niub.png" alt="openmodel" width="150"></a></td>
<td> <a href="https://niub.me">NIUB — 数字服务，一站直达(DujiaoNext自营旗舰店)</a> 正在寻找更便捷的 AI 服务、社交账号或数字礼品卡？NIUB（niub.me）专注提供多种虚拟商品与数字服务，让不同类型的数字需求都能在一个站点完成选购。
我们重视清晰的商品信息、明确的交付方式和负责任的售后支持。每件商品的账号类型、适用地区、有效期限、使用条件与售后范围，均以对应商品页面说明为准。
访问 NIUB，探索更多数字服务与虚拟商品。</td>
</tr>



</table>

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26 · Gin · GORM · SQLite / PostgreSQL |
| Auth | JWT (separate admin / user realms) · Casbin RBAC · TOTP 2FA |
| Async | asynq on Redis (optional — the server runs without it) |
| Config | Viper (`config.yml`) |
| Frontend | Vue 3 · Vite · TypeScript · Tailwind CSS v4 · pnpm 10 |
| Admin UI | shadcn-vue / reka-ui |

## Repository Layout

```
.
├── cmd/server/               # entry point; also hosts the `admin` operator subcommands
├── internal/
│   ├── app/                  # composition root
│   │   ├── container/        # dependency-injection container
│   │   ├── httpserver/       # Gin router, route groups, middleware
│   │   └── jobs/             # asynq worker service and consumers
│   ├── bootstrap/            # per-module wiring (adapters.go + wiring.go)
│   ├── modules/              # 35 business modules — one vertical slice per domain
│   ├── workflows/            # use cases that span several modules
│   ├── platform/             # framework-facing infrastructure
│   │   ├── database/gormdb/  # connection, auto-migration
│   │   └── http/             # response envelope, Gin helpers
│   ├── shared/               # dependency-free primitives (money, jsonmap, serial …)
│   ├── authz/                # Casbin RBAC: policy model, built-in role seeds
│   ├── web/                  # SPA embedding and mounting (build-tag gated)
│   ├── architecture/         # architecture guard tests — no production code
│   ├── cache/ config/ constants/ crypto/ i18n/ logger/ queue/ version/
│   └── admincmd/ htmltext/ persistence/ telegramidentity/ testkit/ upstream/
├── frontend/
│   ├── admin/                # admin panel SPA        (dev :5174)
│   └── user/                 # customer storefront SPA (dev :5173)
├── config.yml.example
├── Dockerfile                # single full-stack image
└── .github/workflows/        # CI and container image publishing
```

Runtime directories created on first start: `db/` (SQLite), `uploads/`, `logs/`.

## Architecture

A modular monolith. Each domain under `internal/modules/<name>/` is a vertical slice with its
own layers:

| Layer | Holds | May import |
| --- | --- | --- |
| `domain/` | entities, value objects, business invariants | nothing from the other layers |
| `application/` | use cases, port interfaces | `domain`, `contract` |
| `infrastructure/` | GORM stores, gateways, queue adapters | `domain`, `application` ports |
| `transport/` | HTTP handlers, presenters | `application` contracts |
| `contract/` | port interfaces the application layer depends on, and the module's public surface for other modules | — |

**These rules are enforced by tests, not convention.** `internal/architecture/` parses every
import in the tree and fails the build on violations. The main ones:

- `domain` must not reach into `application`, `infrastructure`, or `transport`
- `application` must not import Gin or asynq — no transport libraries in use cases
- only a module's `infrastructure/gormstore` adapter may import GORM
- `transport` depends on application contracts, never on concrete stores
- `internal/shared` stays free of modules, GORM, Gin, and asynq
- `internal/platform` must not depend on business modules

Run them with the rest of the suite: `go test ./internal/architecture/...`

Modules never import each other's internals — they talk through `contract/`, and the wiring
lives in `internal/bootstrap/<module>/`.

### RBAC

Every `/api/v1/admin/...` route passes through Casbin. The permission catalog is generated
from the live route table, but the **built-in roles are hand-maintained** in
`internal/authz/bootstrap.go`. Adding an admin route without adding it to a role seed leaves
that route reachable only by the super admin. `internal/app/httpserver/rbac_coverage_test.go`
checks that every registered route is covered.

## Build Tags

| Tag | Effect |
| --- | --- |
| *(none)* | API only. No SPAs mounted — the default for local development. |
| `fullstack` | Embeds `internal/web/dist/{admin,user}` into the binary via `go:embed`. |
| `release` | Production behavior for outbound URL building. |

`go:embed all:dist/admin all:dist/user` requires **both** directories to exist, so a
`fullstack` build fails outright if the frontends were not built first. A plain `go build`
does not compile `embed_fullstack.go` — after touching `internal/web/`, verify with
`go build -tags release,fullstack ./cmd/server`.

## Run Modes

```bash
./dujiao-next                 # all    — HTTP server + background worker (default)
./dujiao-next -mode api       # HTTP server only
./dujiao-next -mode worker    # background worker only
```

Operator subcommands ship in the same binary, so a container needs no extra tooling:

```bash
./dujiao-next admin list-admins
./dujiao-next admin reset-password
./dujiao-next admin reset-2fa
```

## Frontend Notes

Two independent SPAs, both built with Vite and embedded at release time.

**Mount points.** The storefront is served at `/`; the admin panel at `web.admin_path`
(default `/admin`). `/api`, `/uploads`, and `/health` are reserved prefixes — an unmatched
path under them returns 404 instead of falling through to the SPA shell. Adding a new
top-level backend prefix means updating `reservedPaths` in `internal/web/handler.go`.

**The admin base path is resolved at runtime, not at build time.** Since `web.admin_path` is
configurable, `pnpm run build:fullstack` only injects a `<base href="__DJ_ADMIN_BASE__/">`
placeholder, which the server rewrites on startup. Consequences for admin code:

- native `<a href>` and `window.location` navigation must go through `adminUrl()` in
  `src/utils/adminBase.ts`
- `<router-link :to>` and `router.push()` must **not** — vue-router already carries the base,
  and prefixing again yields `/admin/admin/...`

**Storefront templates.** The customer frontend ships more than one look, selected by the
`storefront_template` site setting (`classic`, `vault`). Template pages live in
`src/templates/<name>/` and fall back to `src/views/` when a page has no template-specific
version; see `src/templates/registry.ts`. Append `?template=vault` to preview one locally.

**i18n.** Both frontends and all API responses are localized — Simplified Chinese, Traditional
Chinese, and English. Do not hard-code user-facing strings on either side.

## Quick Start (Deploy)

Container images are published to `ghcr.io/<owner>/<repository>` for `linux/amd64`
and `linux/arm64`. The image path follows the GitHub repository name, in lowercase.

Copy `config.yml.example` to `config.yml`, then configure at least `jwt.secret`,
`user_jwt.secret`, and `web.admin_path`. Point both `redis.host` and `queue.host`
to a Redis instance reachable from the container; the default `127.0.0.1` points
to the application container itself.

```bash
cp config.yml.example config.yml
# Edit config.yml before starting the container.
mkdir -p db uploads logs
IMAGE="ghcr.io/<owner>/<repository>:latest"
docker run -d --name dujiao-next --restart unless-stopped \
  -p 8080:8080 \
  -v "$PWD/config.yml:/app/config.yml:ro" \
  -v "$PWD/db:/app/db" \
  -v "$PWD/uploads:/app/uploads" \
  -v "$PWD/logs:/app/logs" \
  "$IMAGE"
```

Replace `<owner>/<repository>` with your image path. A private GHCR package
requires authentication before pulling. To upgrade, pull the desired image and
recreate the container with the same configuration and data mounts.

The single `docker-build.yml` workflow runs Go formatting, vet, and tests alongside
both frontends' tests. Both verification jobs must pass before Docker builds and
pushes the image. The Dockerfile handles frontend type checks and builds, embeds
the assets, and compiles the server; these builds are not repeated in separate jobs.
The legacy binary installer is outside this image publishing workflow.

The workflow publishes images on these events:

| Trigger | Image tags |
| --- | --- |
| Push to `idid` | `idid`, `latest`, `sha-<full commit SHA>` |
| Push to an `id-*` branch, e.g. `id-payment` | `id-payment`, `sha-<full commit SHA>` |
| Manual dispatch | Selected branch or tag name, plus `sha-<full commit SHA>`; `latest` only on `idid` |

`idid` is the core branch where completed feature versions are merged; only builds
of this branch update `latest`. Feature versions are developed on `id-*` branches,
whose images do not update `latest`. `main` is reserved for syncing upstream.
Pushes to other branches, tag pushes, and pull requests do not trigger this workflow.
No GitHub Releases binary archives are generated.

To build an image locally:

```bash
docker build --build-arg APP_VERSION=local -t dujiao-next:local .
```

## PostgreSQL Backups to Cloudflare R2

With `database.driver: postgres` (or `postgresql`), open the database backup tab in
admin settings. Configure the R2 account endpoint
`https://<account-id>.r2.cloudflarestorage.com`, bucket, access key ID, secret access
key, and optional object prefix. Use credentials restricted to the backup bucket
with object read, write, and delete permissions. The bucket must already exist;
use its S3 API endpoint, rather than an R2 public download domain. SQLite databases
are not supported by this feature.

Test the connection, create a manual backup, then enable a daily schedule with an
explicit time zone if needed. The default schedule is 03:00 in `Asia/Shanghai`,
with 14 successful backups retained. The backup list shows export/upload status,
archive size, and SHA-256. Downloads use signed links valid for 15 minutes. Removing
a backup deletes its object and then its record; retention removes older successful
backups. Run only one application instance with backup scheduling enabled against
a given database.

Backups are official `pg_dump --format=custom` archives. The process reads the same
PostgreSQL DSN as the application, preserves its libpq SSL options, and passes its
password through the subprocess environment. Both URL and libpq `key=value` DSNs
are accepted. Install the PostgreSQL client tools, including `pg_dump` and `psql`
in the same directory. Set `backup.pg_dump_path` to the dump executable (default
`pg_dump`); an absolute path is also supported. A readiness check uses the adjacent
`psql` with the same libpq connection options to query the actual server version
and rejects a dump client whose major version is older than the server. Prefer a
client matching the source server, and keep a compatible `pg_restore` available
for recovery.

The container uses the official `postgres:${POSTGRES_MAJOR}-alpine` runtime so its
`pg_dump`, `pg_restore`, and client libraries match. `POSTGRES_MAJOR` defaults to 16;
set it to the PostgreSQL major version used by your deployment. The inherited database initializer is disabled: starting the
image runs the application. Both `linux/amd64` and `linux/arm64` remain supported.

```bash
docker build --build-arg APP_VERSION=local --build-arg POSTGRES_MAJOR=16 \
  -t dujiao-next:local .
```

Export finishes on local disk before upload starts. Reserve enough space for at
least one complete archive in `backup.temp_dir` (the system temporary directory by
default). For large databases, mount a dedicated writable disk there rather than
using a memory-backed temporary directory or a small container writable layer.
`backup.timeout_seconds` bounds the entire job; its configuration default is 3600
seconds. Normal completion, failure, and cancellation clean up temporary files.
Uploads stream from the file with one multipart request at a time.

Keep the original **`app.secret_key`** in a secure copy outside the database and R2
bucket. It decrypts stored R2 credentials and other encrypted application data;
restoring the database with a different key does not recover those secrets. Also
save `config.yml` securely and back up `uploads/` separately. Database archives do
not include configuration files, uploaded files, Redis queues, or PostgreSQL roles
and other cluster-wide objects.

### Restore into a New Database

1. Download a successful `.dump` archive and compare its SHA-256 with the backup
   record, for example with `sha256sum database-backup.dump`.
2. Use `pg_restore` from the same client major that created the archive, or a newer
   compatible client. Restore into the same PostgreSQL server major as the source
   when using a matching dump client, or a supported newer server. Dumps produced
   by newer clients are not guaranteed to restore into older server versions.
3. Create an empty database owned by the application's database role. Configure
   `PGHOST`, `PGPORT`, and `PGUSER` (and a password file or interactive authentication)
   for the recovery server, then run:

   ```bash
   createdb --template=template0 dujiao_restored
   pg_restore --exit-on-error --no-owner --no-privileges \
     --dbname=dujiao_restored database-backup.dump
   ```

4. Stop the application before switching its DSN to `dujiao_restored`. Restore the
   original `app.secret_key`, configuration, and uploaded files, then start the same
   application version used for the backup and verify the data before upgrading.
5. Check the R2 configuration and backup schedule after recovery. Keep the original
   database intact until the restored application has been verified.

For an adapter integration check against an existing PostgreSQL database, run:

```bash
BACKUP_TEST_POSTGRES_DSN='your-test-dsn' \
BACKUP_TEST_PG_DUMP=/absolute/path/to/pg_dump \
  go test ./internal/modules/backup/infrastructure/pgdump -run TestIntegrationPostgres -count=1
```

The test exports a temporary archive and inspects it with the adjacent `pg_restore`;
it does not modify the source database. Keep test credentials out of shell history.

## Quick Start (Develop)

Run the backend and the two frontends separately for hot reload:

```bash
go mod tidy && go run ./cmd/server   # :8080 — API only, no SPAs mounted

cd frontend/user  && pnpm install && pnpm run dev   # :5173
cd frontend/admin && pnpm install && pnpm run dev   # :5174
```

Both dev servers proxy `/api`, `/uploads`, `/sitemap.xml`, and `/robots.txt` to
`localhost:8080`. In production everything is same-origin, so these proxies are a
development-only concern.

> Use `pnpm` via corepack. `pnpm --dir X` does not read the `packageManager` field of the
> target directory and will pick the wrong version — `cd` into the package first.

## Building the Full-Stack Binary

Docker is the release artifact. For local development, you can also build a
standalone full-stack binary after installing both frontends' dependencies:

```bash
(cd frontend/admin && pnpm run build:fullstack)   # injects the <base> placeholder
(cd frontend/user  && pnpm run build)
rm -rf internal/web/dist && mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
go build -tags release,fullstack -o dujiao-next ./cmd/server
```

Note that admin uses `build:fullstack`, not `build`. Plain `build` produces a bundle pinned to
`/`, which silently breaks a custom `web.admin_path`.

## Testing

```bash
go test ./...                              # full suite
go test ./internal/architecture/...        # dependency and layering guards
go test ./internal/modules/order/...       # one module

cd frontend/user  && pnpm run build        # includes vue-tsc type checking
cd frontend/admin && pnpm run build
```

Health check endpoint: `GET /health`

## Notes on Data Access

SQLite runs with `MaxOpenConns=1`. A store opens a transaction through
`WithinTransaction(func(tx contract.Transaction) error)`, and every query inside the closure
must go through that `tx` handle or a store bound to it via `WithTx(tx)`. Reaching for the
global DB handle instead asks for a second connection that will never be granted, deadlocking
the process — including indirectly, by calling a service that queries on its own. Read any
settings you need *before* opening the transaction, and keep outbound HTTP calls (payment
gateways and the like) outside it.

## Online Documentation

- https://dujiao-next.com
