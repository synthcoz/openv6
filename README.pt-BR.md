<p align="center"><img src="site/assets/logo-256.png" width="72" alt="openv6"></p>
<h1 align="center">openv6</h1>
<p align="center"><b>Proxies IPv6 estáticos (HTTP + SOCKS5) a partir da sua própria rede, com um único painel web.</b></p>
<p align="center"><a href="https://thephuc.xyz/openv6/">Site</a> · <a href="https://github.com/thephux/openv6-releases/releases/latest">Baixar</a> · <a href="CHANGELOG.md">Novidades</a></p>
<p align="center"><a href="README.md">English</a> · <a href="README.vi.md">Tiếng Việt</a> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.es.md">Español</a> · <a href="README.ru.md">Русский</a> · <b>Português</b></p>

![openv6 dashboard](site/assets/shot-proxies.webp)

O openv6 transforma o prefixo IPv6 que o seu provedor de internet ou de VPS entrega em centenas de proxies fixos, um por conta. Feito para navegadores antidetecção (Multilogin, GoLogin, Dolphin Anty, ixBrowser, AdsPower, MoreLogin…).

- **Um IPv6 fixo por proxy.** Cada proxy tem porta, usuário, senha e IPv6 de saída próprios, mantidos até você mudá-los.
- **Qualquer rede IPv6.** Internet residencial (um /64 na LAN; se o provedor mudar o prefixo, o openv6 acompanha e mantém portas e senhas) e servidores VPS ou homelab com /64, /56 ou /48 roteado.
- **Os clientes conectam por IPv4** (o IP da LAN da máquina ou o IP público da VPS); o tráfego sai por IPv6.
- **Windows, macOS e Linux.** Qualquer um pode rodar proxies (um "nó"). Várias máquinas são gerenciadas em um só painel.
- **Verifica e corrige sozinho.** Checagens periódicas do IP, causas de erro claras, correção com um clique e alertas no Telegram.
- **Funciona com VPN ligada**, e pode ser compartilhado fora da LAN por relay em uma VPS, UPnP ou Tailscale.
- **Um único executável.** Sem Docker e sem servidor proxy separado. Painel em inglês e vietnamita, claro e escuro, ótimo no celular.

## Instalação

| Sistema | Como |
|---|---|
| **Windows 10/11** | Baixe `openv6-setup-windows-amd64.exe` em [Releases](https://github.com/thephux/openv6-releases/releases/latest) e clique em Next → Install. |
| **macOS** (Intel e Apple silicon) | Baixe `openv6-macos.pkg` em Releases. |
| **Ubuntu / Debian / Zorin** (amd64, arm64, armhf) | O comando abaixo, ou o arquivo `.deb` em Releases. |

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/thephux/openv6-releases/main/install.ps1 | iex
```

**macOS / Linux** (Terminal):

```bash
curl -fsSL https://raw.githubusercontent.com/thephux/openv6-releases/main/install.sh | sudo bash
```

O openv6 roda em segundo plano e inicia com a máquina. O ícone da bandeja mostra o status e abre o painel.

1. Abra o painel (`http://<IP-da-LAN-da-máquina>:8080`, ou o ícone da bandeja) e crie a conta de administrador.
2. **Proxies → Create proxies** e depois **Export** no formato do seu navegador antidetecção.
3. Todos os dispositivos da rede usam `IP-da-LAN-da-máquina:porta` (ou o IP público da VPS).

Para adicionar outra máquina (VPS, mini PC, outro PC): **Nodes → Add node** e rode nessa máquina o comando exibido.

## Free e Pro

| | Free | Pro (pagamento único) |
|---|---|---|
| Proxies | 3 | Ilimitados |
| Máquinas rodando proxies | 1 | Várias (VPS, mini PCs, PCs) |
| HTTP + SOCKS5, exportação, autoverificação | ✓ | ✓ |
| Compartilhamento por relay / UPnP / Tailscale, API, contas de clientes, Telegram, placa de rede por proxy |  | ✓ |

Compre uma licença no [site](https://thephuc.xyz/openv6/#pricing) e informe-a em **Settings → License**.

## Comandos

```
openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
openv6 agent   -panel URL -token TOKEN [-data DIR]
openv6 install server|agent [flags]   # instalar como serviço (root / Administrador)
openv6 uninstall                      # remover o serviço e todos os IPv6 adicionados pelo openv6
openv6 tray                           # ícone da bandeja
openv6 reset-password [-data DIR] [-user NAME]
openv6 version
```

## Documentação

Os guias detalhados estão em vietnamita por enquanto.

| Arquivo | Assunto |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Windows 10/11: ícone da bandeja, LAN/Wi-Fi, firewall |
| [docs/ZORIN.md](docs/ZORIN.md) | Instalação passo a passo no Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Guia completo: verificar IPv6, internet residencial, VPS, solução de problemas |
| [docs/API.md](docs/API.md) | API JSON e tokens de API |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Arquitetura, atribuição de IPv6, protocolo painel ↔ agente |
| [docs/RELEASE.md](docs/RELEASE.md) | Lançar uma nova versão, atualizações assinadas |

## Desenvolvimento

```bash
make test   # testes unitários
make dev    # painel em modo -dry-run (sem alterar a rede), http://localhost:18080
make dist   # builds Linux em dist/
```

A landing page fica em [`site/`](site) e é publicada no Cloudflare (veja [deploy/cloudflare](deploy/cloudflare)).
