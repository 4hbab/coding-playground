# Deploying PyPlayground

How the public site runs, how to set it up from scratch, and how to operate it.
Everything here is free: a Cloudflare account (Workers free plan) and a machine with Docker.

```
Visitor ──HTTPS──▶ Worker (pyplayground.<subdomain>.workers.dev)
                     │  Workers VPC service "pyplayground-app" → 127.0.0.1:8080
                     ▼
                   Cloudflare Tunnel "pyplayground"
                     │  outbound connection from the host; no open ports
                     ▼
Host ─ cloudflared (system service) ──▶ 127.0.0.1:8080 ─ Docker: Go server ──▶ sandbox containers
                                                              └─ SQLite in the app-data volume
```

- **Worker** ([`worker/`](../worker)): the public address. It overwrites `X-Client-IP` with the
  visitor's real IP (the app uses it for rate limits) and forwards the request through a
  [Workers VPC](https://developers.cloudflare.com/workers-vpc/) service binding.
- **Tunnel + cloudflared**: `cloudflared` runs on the host as a system service (it starts at
  boot) and keeps an outbound connection to Cloudflare. The VPC service sends requests down it
  to `127.0.0.1:8080`.
- **App**: the Go server, started by [`docker-compose.yml`](../docker-compose.yml) and bound to
  `127.0.0.1:8080` only. It starts the sandbox containers through the host's Docker socket.

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
  --tunnel-id <TUNNEL_ID> --ipv4 127.0.0.1 --http-port 8080
# → Created VPC service: <SERVICE_ID>
```

`127.0.0.1:8080` is where the app listens on the host. `cloudflared` runs on the same machine,
so the address is local to it.

Put `<SERVICE_ID>` in [`worker/wrangler.jsonc`](../worker/wrangler.jsonc) under `vpc_services`.

### 3. Run cloudflared on the host

Get the tunnel token: Cloudflare dashboard → **Networking** → **Tunnels** → `pyplayground` →
**Add a replica**. The token is the long `eyJ…` value in the install command. Keep it secret.

```bash
brew install cloudflared                      # on Linux: install the cloudflared package
sudo cloudflared service install <TOKEN>      # runs it as a system service, started at boot
```

`npx wrangler tunnel info pyplayground` should now show `Status: healthy`.

### 4. Deploy the Worker

```bash
npx wrangler deploy
# → https://pyplayground.<subdomain>.workers.dev
```

Until the app is running, the Worker answers `503` "PyPlayground is offline".

### 5. Create a GitHub OAuth app for production

A GitHub OAuth app has a single callback URL, so production needs its own app (keep the
localhost one for development). At <https://github.com/settings/developers> → **New OAuth App**:

| Field | Value |
|---|---|
| Homepage URL | `https://pyplayground.<subdomain>.workers.dev` |
| Authorization callback URL | `https://pyplayground.<subdomain>.workers.dev/auth/github/callback` |

Then generate a client secret.

### 6. Fill in `.env.production`

```bash
cp .env.production.example .env.production
chmod 600 .env.production
```

| Setting | Value |
|---|---|
| `PUBLIC_URL` | The Worker's URL from step 4 |
| `CLIENT_IP_HEADER` | `X-Client-IP` (already set) |
| `JWT_SECRET` | `openssl rand -hex 32` |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | From step 5 |

`.env.production` is gitignored. Never commit it.

### 7. Start the app

```bash
docker compose up -d --build
docker compose ps          # app should be "healthy"
```

### 8. Check it

```bash
URL=https://pyplayground.<subdomain>.workers.dev
curl -s $URL/healthz                  # {"status":"ok"}
curl -s $URL/api/config               # {"serverExecution":true}
curl -s -X POST $URL/api/execute -H 'Content-Type: application/json' \
  -d '{"code":"print(6 * 7)"}'        # {"stdout":"42\n",...}
```

Then open the URL, run code in both modes, and sign in with GitHub.

## Keeping it up on a Mac

- `cloudflared` is a system service: it starts at boot and restarts if it crashes.
- Docker Desktop → Settings → General → **Start Docker Desktop when you sign in**. The app
  uses `restart: unless-stopped`, so it comes back with Docker.
- Stop the Mac from sleeping while the site should be up: System Settings → Battery (or
  Energy) → **Prevent automatic sleeping when the display is off** (on power), or run
  `caffeinate -dims` in a terminal and leave it open.
- While the Mac sleeps or is off, visitors get the Worker's "offline" page; nothing else breaks.

## Day-to-day

| Task | Command |
|---|---|
| Deploy a new version of the app | `git pull && docker compose up -d --build` |
| Deploy a new version of the Worker | `cd worker && npx wrangler deploy` |
| App logs | `docker compose logs -f app` |
| Tunnel logs (macOS) | `tail -f /Library/Logs/com.cloudflare.cloudflared.err.log` |
| Tunnel status | `cd worker && npx wrangler tunnel info pyplayground` |
| Worker logs | `cd worker && npx wrangler tail` |
| Take the site offline | `docker compose down` (data is kept in the `app-data` volume) |
| Back up the database | `docker compose stop app && docker compose cp app:/data ./backup-$(date +%F) && docker compose start app` |

The backup stops the app briefly because SQLite runs in WAL mode: copying the files while it
writes could produce an inconsistent copy.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| "PyPlayground is offline" | The host is asleep, Docker or the app isn't running (`docker compose ps`), or `cloudflared` isn't connected (`wrangler tunnel info pyplayground`, tunnel logs) |
| Sign-in fails with "redirect_uri is not associated" | The OAuth app's callback URL doesn't match `PUBLIC_URL` + `/auth/github/callback` |
| 404 on `/auth/github/login` | `GITHUB_CLIENT_ID` or `GITHUB_CLIENT_SECRET` missing; the app logs a warning at startup |
| No Server option in the toggle | The app can't reach Docker: check the socket mount and `docker compose logs app` |
| Everyone shares one rate limit | `CLIENT_IP_HEADER` isn't `X-Client-IP`, so every request counts as the tunnel's address |

## Removing everything

```bash
docker compose down -v                     # -v also deletes the database volume
sudo cloudflared service uninstall
cd worker
npx wrangler delete pyplayground
npx wrangler vpc service delete <SERVICE_ID>
npx wrangler tunnel delete pyplayground
```
