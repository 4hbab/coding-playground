# Deploying PyPlayground

How the public site runs, how to set it up from scratch, and how to operate it.
Everything here is free: a Cloudflare account (Workers free plan) and a machine with Docker.

```
Visitor ──HTTPS──▶ Worker (pyplayground.<subdomain>.workers.dev)
                     │  Workers VPC service "pyplayground-app"
                     ▼
                   Cloudflare Tunnel "pyplayground"
                     │  outbound connection from the host; no open ports
                     ▼
Host (Docker) ─ cloudflared ──▶ app:8080 (Go server) ──▶ sandbox containers
                                   └─ SQLite in the app-data volume
```

- **Worker** ([`worker/`](../worker)): the public address. It overwrites `X-Client-IP` with the
  visitor's real IP (the app uses it for rate limits) and forwards the request through a
  [Workers VPC](https://developers.cloudflare.com/workers-vpc/) service binding.
- **Tunnel + cloudflared**: `cloudflared` runs in Docker Compose on the host and keeps an
  outbound connection to Cloudflare. The VPC service sends requests down it to `app:8080`.
- **App**: the Go server, started by [`docker-compose.yml`](../docker-compose.yml). It starts
  the sandbox containers through the host's Docker socket.

The live site: <https://pyplayground.crowdpulse-labs.workers.dev>, hosted on a personal Mac,
so it is up only while that Mac is on.

## One-time setup

You need Docker (Docker Desktop on a Mac), Node.js for wrangler, and a Cloudflare account.

### 1. Log in to Cloudflare

```bash
cd worker
npm ci                    # installs the wrangler version pinned in package-lock.json
npx wrangler login
```

### 2. Create the tunnel and the VPC service

```bash
npx wrangler tunnel create pyplayground
# → ID: <TUNNEL_ID>

npx wrangler vpc service create pyplayground-app --type http \
  --tunnel-id <TUNNEL_ID> --hostname app --http-port 8080
# → Created VPC service: <SERVICE_ID>
```

`--hostname app` is the app's name inside Docker Compose. `cloudflared` looks it up on the host,
so no IP addresses are involved.

Put `<SERVICE_ID>` in [`worker/wrangler.jsonc`](../worker/wrangler.jsonc) under `vpc_services`.

### 3. Deploy the Worker

```bash
npx wrangler deploy
# → https://pyplayground.<subdomain>.workers.dev
```

Until the tunnel is running, the Worker answers `503` "PyPlayground is offline".

### 4. Create a GitHub OAuth app for production

A GitHub OAuth app has a single callback URL, so production needs its own app (keep the
localhost one for development). At <https://github.com/settings/developers> → **New OAuth App**:

| Field | Value |
|---|---|
| Homepage URL | `https://pyplayground.<subdomain>.workers.dev` |
| Authorization callback URL | `https://pyplayground.<subdomain>.workers.dev/auth/github/callback` |

Then generate a client secret.

### 5. Fill in `.env.production`

```bash
cp .env.production.example .env.production
chmod 600 .env.production
```

| Setting | Value |
|---|---|
| `PUBLIC_URL` | The Worker's URL from step 3 |
| `CLIENT_IP_HEADER` | `X-Client-IP` (already set) |
| `JWT_SECRET` | `openssl rand -hex 32` |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | From step 4 |
| `TUNNEL_TOKEN` | Cloudflare dashboard → **Networking** → **Tunnels** → `pyplayground` → **Add a replica**: the `eyJ…` value in the install command |

`.env.production` is gitignored. Never commit it.

### 6. Start the stack

```bash
docker compose --profile tunnel up -d --build
docker compose ps          # app should be "healthy", cloudflared "Up"
```

### 7. Check it

```bash
URL=https://pyplayground.<subdomain>.workers.dev
curl -s $URL/healthz                  # {"status":"ok"}
curl -s $URL/api/config               # {"serverExecution":true}
curl -s -X POST $URL/api/execute -H 'Content-Type: application/json' \
  -d '{"code":"print(6 * 7)"}'        # {"stdout":"42\n",...}
```

Then open the URL, run code in both modes, and sign in with GitHub.

## Keeping it up on a Mac

- Docker Desktop → Settings → General → **Start Docker Desktop when you sign in**. The
  containers use `restart: unless-stopped`, so they come back with Docker.
- Stop the Mac from sleeping while the site should be up: System Settings → Battery (or
  Energy) → **Prevent automatic sleeping when the display is off** (on power), or run
  `caffeinate -dims` in a terminal and leave it open.
- While the Mac sleeps or is off, visitors get the Worker's "offline" page; nothing else breaks.

## Day-to-day

| Task | Command |
|---|---|
| Deploy a new version of the app | `git pull && docker compose --profile tunnel up -d --build` |
| Deploy a new version of the Worker | `cd worker && npx wrangler deploy` |
| App logs | `docker compose logs -f app` |
| Tunnel logs | `docker compose logs -f cloudflared` |
| Worker logs | `cd worker && npx wrangler tail` |
| Take the site offline | `docker compose --profile tunnel down` (data is kept in the `app-data` volume) |
| Back up the database | `docker compose stop app && docker compose cp app:/data ./backup-$(date +%F) && docker compose start app` |

The backup stops the app briefly because SQLite runs in WAL mode: copying the files while it
writes could produce an inconsistent copy.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| "PyPlayground is offline" | The host is asleep, Docker isn't running, or `cloudflared` isn't connected: `docker compose ps`, `docker compose logs cloudflared` |
| Sign-in fails with "redirect_uri is not associated" | The OAuth app's callback URL doesn't match `PUBLIC_URL` + `/auth/github/callback` |
| Sign-in button does nothing / 404 on `/auth/github/login` | `GITHUB_CLIENT_ID` or `GITHUB_CLIENT_SECRET` missing; the app logs a warning at startup |
| No Server option in the toggle | The app can't reach Docker: check the socket mount and `docker compose logs app` |
| Everyone shares one rate limit | `CLIENT_IP_HEADER` isn't `X-Client-IP`, so every request counts as the tunnel's address |

## Removing everything

```bash
docker compose --profile tunnel down -v    # -v also deletes the database volume
cd worker
npx wrangler delete pyplayground
npx wrangler vpc service delete <SERVICE_ID>
npx wrangler tunnel delete pyplayground
```
