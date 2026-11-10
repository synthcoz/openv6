<p align="center"><img src="site/assets/logo-256.png" width="72" alt="openv6"></p>
<h1 align="center">openv6</h1>
<p align="center"><b>Proxies IPv6 estáticos (HTTP + SOCKS5) desde tu propia red, con un solo panel web.</b></p>
<p align="center"><a href="https://thephuc.xyz/openv6/">Sitio web</a> · <a href="https://github.com/synthcoz/openv6/releases/latest">Descargar</a> · <a href="CHANGELOG.md">Cambios</a></p>
<p align="center"><a href="README.md">English</a> · <a href="README.vi.md">Tiếng Việt</a> · <a href="README.zh-CN.md">简体中文</a> · <b>Español</b> · <a href="README.ru.md">Русский</a> · <a href="README.pt-BR.md">Português</a></p>

![openv6 dashboard](site/assets/shot-proxies.webp)

openv6 convierte el prefijo IPv6 que te da tu proveedor de internet o de VPS en cientos de proxies fijos, uno por cuenta. Está hecho para navegadores antidetección (Multilogin, GoLogin, Dolphin Anty, ixBrowser, AdsPower, MoreLogin…).

- **Una IPv6 fija por proxy.** Cada proxy tiene su propio puerto, usuario, contraseña e IPv6 de salida, que se mantienen hasta que tú los cambies.
- **Cualquier red IPv6.** Conexiones domésticas (un /64 en la LAN; si el proveedor cambia el prefijo, openv6 lo sigue y conserva puertos y contraseñas) y servidores VPS o homelab con un /64, /56 o /48 enrutado.
- **Los clientes se conectan por IPv4** (la IP LAN del equipo o la IP pública del VPS); el tráfico sale por IPv6.
- **Windows, macOS y Linux.** Cualquiera puede ejecutar proxies (un "nodo"). Varios equipos se gestionan desde un mismo panel.
- **Se revisa y se repara solo.** Comprobaciones periódicas de la IP, causas de error claras, arreglos con un clic y alertas por Telegram.
- **Funciona con una VPN activa**, y se puede compartir fuera de la LAN mediante un relay en un VPS, UPnP o Tailscale.
- **Un solo binario.** Sin Docker ni servidor proxy aparte. Panel en inglés y vietnamita, modo claro y oscuro, cómodo en el móvil.

## Instalación

| Sistema | Cómo |
|---|---|
| **Windows 10/11** | Descarga `openv6-setup-windows-amd64.exe` de [Releases](https://github.com/synthcoz/openv6/releases/latest) y pulsa Next → Install. |
| **macOS** (Intel y Apple silicon) | Descarga `openv6-macos.pkg` de Releases. |
| **Ubuntu / Debian / Zorin** (amd64, arm64, armhf) | El comando de abajo, o el archivo `.deb` de Releases. |

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
```

openv6 se ejecuta en segundo plano y arranca con el equipo. El icono de la bandeja muestra el estado y abre el panel.

1. Abre el panel (`http://<IP-LAN-del-equipo>:8080`, o el icono de la bandeja) y crea la cuenta de administrador.
2. **Proxies → Create proxies** y luego **Export** en el formato de tu navegador antidetección.
3. Todos los dispositivos de la red usan `IP-LAN-del-equipo:puerto` (o la IP pública del VPS).

Para añadir otro equipo (VPS, mini PC, otro PC): **Nodes → Add node** y ejecuta en ese equipo el comando que aparece.

## Free y Pro

| | Free | Pro (pago único) |
|---|---|---|
| Proxies | 3 | Ilimitados |
| Equipos que ejecutan proxies | 1 | Varios (VPS, mini PC, PC) |
| HTTP + SOCKS5, exportación, autocomprobación | ✓ | ✓ |
| Compartir por relay / UPnP / Tailscale, API, cuentas de clientes, Telegram, tarjeta de red por proxy |  | ✓ |

Compra una licencia en el [sitio web](https://thephuc.xyz/openv6/#pricing) e introdúcela en **Settings → License**.

## Comandos

```
openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
openv6 agent   -panel URL -token TOKEN [-data DIR]
openv6 install server|agent [flags]   # instalar como servicio (root / Administrador)
openv6 uninstall                      # quitar el servicio y todas las IPv6 añadidas por openv6
openv6 tray                           # icono de la bandeja
openv6 reset-password [-data DIR] [-user NAME]
openv6 version
```

## Documentación

Las guías detalladas están en vietnamita por ahora.

| Archivo | Tema |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Windows 10/11: icono de bandeja, LAN/Wi-Fi, firewall |
| [docs/ZORIN.md](docs/ZORIN.md) | Instalación paso a paso en Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Guía completa: comprobar IPv6, conexiones domésticas, VPS, solución de problemas |
| [docs/API.md](docs/API.md) | API JSON y tokens de API |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arquitectura, asignación de IPv6, protocolo panel ↔ agente |
| [docs/RELEASE.md](docs/RELEASE.md) | Publicar una versión nueva, actualizaciones firmadas |

## Desarrollo

```bash
make test   # pruebas unitarias
make dev    # panel en modo -dry-run (sin cambios de red), http://localhost:18080
make dist   # compilaciones de Linux en dist/
```

La página de inicio está en [`site/`](site) y se despliega en Cloudflare (ver [deploy/cloudflare](deploy/cloudflare)).
