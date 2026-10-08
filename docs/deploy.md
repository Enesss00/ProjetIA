# Deploying GHOST NET

GHOST NET is a single static Go binary that serves both the WebSocket API and
the built web client, so hosting it is just "run one container". The repo ships
a multi-stage `Dockerfile` that builds the web client and the server into one
small image. The server listens on `$PORT` (falling back to `:8080`), which is
what every cloud host injects.

No database to provision, no secrets required. (Optional: set
`ANTHROPIC_API_KEY` to enable LLM-enriched incident reports; without it the
deterministic template report is used.)

## Option A — Render.com (easiest, free)

1. Push this branch to GitHub (already done).
2. Go to <https://render.com>, sign up with GitHub.
3. **New +** → **Blueprint** → select the `ProjetIA` repo → Render reads
   `render.yaml` and creates the service. Click **Apply**.
   - Or **New +** → **Web Service** → pick the repo → Runtime **Docker** →
     leave the defaults → **Create Web Service**.
4. Wait for the first build (a few minutes). You get a public URL like
   `https://ghost-net.onrender.com`. Share that with your professor.

Note: the free plan sleeps after ~15 min idle, so the first visit after a pause
takes ~30–50 s to wake. Fine for a demo.

## Option B — Fly.io (free allowance, faster cold start)

Install the CLI (`https://fly.io/docs/hands-on/install-flyctl/`), then from the
repo root:

```bash
fly auth signup        # or: fly auth login
fly launch --copy-config --now
```

`fly launch` reads `fly.toml`, builds the `Dockerfile`, and deploys. It gives
you `https://ghost-net.fly.dev` (the app name must be globally unique — change
`app = "ghost-net"` in `fly.toml` if it is taken). Re-deploy later with
`fly deploy`.

## Option C — Any Docker host / VPS

```bash
docker build -t ghost-net .
docker run -p 8080:8080 ghost-net
```

Then put it behind a reverse proxy (Caddy/Nginx) with TLS. WebSocket upgrade
must be allowed — Caddy does this automatically; for Nginx add the standard
`Upgrade`/`Connection` headers on the `/ws` location.

## Option D — Local network only

If the professor just needs to see it live during a presentation, run it on
your machine and share your screen (see the README), or expose it temporarily
with a tunnel:

```bash
# after building: server/ghostnet.exe serve -static web/dist
npx localtunnel --port 8080      # or: ngrok http 8080
```

## Checklist

- WebSocket works out of the box — the client connects to `/ws` on the same
  origin it was served from, so no cross-origin config is needed.
- Health check endpoint: `GET /healthz` returns `200 ok`.
- The server reads `$PORT`; don't hardcode a port on the host.
