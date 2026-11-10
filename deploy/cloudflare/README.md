# Landing page on Cloudflare (thephuc.xyz/openv6)

The landing in `site/` is served at **https://thephuc.xyz/openv6/** by the
Worker `openv6-landing` (static assets only). Its routes run in front of the
rest of thephuc.xyz, so the main site is untouched.

## Automatic deploys (Workers Builds, one-time setup)

Cloudflare dashboard → **Workers & Pages** → **Create** → **Import a repository**
→ pick this GitHub repo, then:

| Setting | Value |
|---|---|
| Project name | `openv6-landing` (must match `name` in `wrangler.jsonc`) |
| Production branch | `main` |
| Root directory | `deploy/cloudflare` |
| Build command | `./build.sh` |
| Deploy command | `npx wrangler deploy` |
| Build watch paths (optional) | include `site/*`, `deploy/cloudflare/*` |

Every push to `main` that touches the landing then redeploys it.

## Manual deploy

```bash
CLOUDFLARE_API_TOKEN=... deploy/cloudflare/deploy.sh
```
