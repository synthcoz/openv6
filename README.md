<p align="center"><img src="site/assets/logo-256.png" width="72" alt="openv6"></p>
<h1 align="center">openv6</h1>
<p align="center"><b>Static IPv6 proxies (HTTP + SOCKS5) from your own network, with one web dashboard.</b></p>
<p align="center"><a href="https://thephuc.xyz/openv6/">Website</a> · <a href="https://github.com/synthcoz/openv6-releases/releases/latest">Download</a> · <a href="CHANGELOG.md">Changelog</a></p>
<p align="center"><b>English</b> · <a href="README.vi.md">Tiếng Việt</a> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.es.md">Español</a> · <a href="README.ru.md">Русский</a> · <a href="README.pt-BR.md">Português</a></p>

![openv6 dashboard](site/assets/shot-proxies.webp)

openv6 turns the IPv6 prefix your home ISP or VPS provider gives you into hundreds of fixed proxies, one per account. It is made for anti-detect browsers (Multilogin, GoLogin, Dolphin Anty, ixBrowser, AdsPower, MoreLogin…).

- **One fixed IPv6 per proxy.** Every proxy has its own port, username, password and outgoing IPv6, kept until you change it.
- **Any IPv6 network.** Home ISPs (a /64 on the LAN; when the ISP changes the prefix, openv6 follows and keeps ports and passwords) and VPS or homelab servers with a routed /64, /56 or /48.
- **Clients connect over IPv4** (the machine's LAN IP, or the public IP of a VPS); traffic leaves over IPv6.
- **Windows, macOS and Linux.** Any of them can run proxies (a "node"). Several machines are managed from one dashboard.
- **Checks and fixes itself.** Regular IP checks, clear error causes, one-click fixes, Telegram alerts.
- **Works with a VPN on**, and can be shared outside the LAN through a relay on a VPS, UPnP or Tailscale.
- **One binary.** No Docker and no separate proxy server. Dashboard in English and Vietnamese, light and dark, mobile friendly.

## Install

| System | How |
|---|---|
| **Windows 10/11** | Download `openv6-setup-windows-amd64.exe` from [Releases](https://github.com/synthcoz/openv6-releases/releases/latest) and click Next → Install. |
| **macOS** (Intel and Apple silicon) | Download `openv6-macos.pkg` from Releases. |
| **Ubuntu / Debian / Zorin** (amd64, arm64, armhf) | The command below, or the `.deb` file from Releases. |

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/synthcoz/openv6-releases/main/install.ps1 | iex
```

**macOS / Linux** (Terminal):

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6-releases/main/install.sh | sudo bash
```

openv6 runs in the background and starts with the machine. The tray icon shows the status and opens the dashboard.

1. Open the dashboard (`http://<machine-LAN-IP>:8080`, or the tray icon) and create the admin account.
2. **Proxies → Create proxies**, then **Export** in the format of your anti-detect browser.
3. Every device on the network uses `machine-LAN-IP:port` (or the VPS public IP).

To add another machine (VPS, mini PC, another PC): **Nodes → Add node**, then run the command it shows on that machine.

## Free and Pro

| | Free | Pro (one-time payment) |
|---|---|---|
| Proxies | 3 | Unlimited |
| Machines running proxies | 1 | Many (VPS, mini PCs, PCs) |
| HTTP + SOCKS5, export, self-check | ✓ | ✓ |
| Relay / UPnP / Tailscale sharing, API, customer accounts, Telegram, network card per proxy | | ✓ |

Buy a license on the [website](https://thephuc.xyz/openv6/#pricing) and enter it in **Settings → License**.

## Commands

```
openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
openv6 agent   -panel URL -token TOKEN [-data DIR]
openv6 install server|agent [flags]   # install as a service (root / Administrator)
openv6 uninstall                      # remove the service and every IPv6 openv6 added
openv6 tray                           # tray icon
openv6 reset-password [-data DIR] [-user NAME]
openv6 version
```

## Documentation

The detailed guides are in Vietnamese for now.

| File | Topic |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Windows 10/11: tray icon, LAN/Wi-Fi, firewall |
| [docs/ZORIN.md](docs/ZORIN.md) | Step-by-step install on Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Full guide: checking IPv6, home ISPs, VPS, troubleshooting |
| [docs/API.md](docs/API.md) | JSON API and API tokens |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Architecture, IPv6 assignment, panel ↔ agent protocol |
| [docs/RELEASE.md](docs/RELEASE.md) | Releasing a new version, signed updates |

## Development

```bash
make test   # unit tests
make dev    # dashboard in -dry-run mode (no network changes), http://localhost:18080
make dist   # Linux builds into dist/
```

The landing page lives in [`site/`](site) and is deployed to Cloudflare (see [deploy/cloudflare](deploy/cloudflare)).
