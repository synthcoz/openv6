<p align="center"><img src="site/assets/logo-256.png" width="72" alt="openv6"></p>
<h1 align="center">openv6</h1>
<p align="center"><b>Статические IPv6-прокси (HTTP + SOCKS5) из вашей собственной сети с единой веб-панелью.</b></p>
<p align="center"><a href="https://thephuc.xyz/openv6/">Сайт</a> · <a href="https://github.com/synthcoz/openv6/releases/latest">Скачать</a> · <a href="CHANGELOG.md">Изменения</a></p>
<p align="center"><a href="README.md">English</a> · <a href="README.vi.md">Tiếng Việt</a> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.es.md">Español</a> · <b>Русский</b> · <a href="README.pt-BR.md">Português</a></p>

![openv6 dashboard](site/assets/shot-proxies.webp)

openv6 превращает IPv6-префикс, который выдаёт ваш домашний провайдер или VPS-хостинг, в сотни постоянных прокси — по одному на аккаунт. Сделано для антидетект-браузеров (Multilogin, GoLogin, Dolphin Anty, ixBrowser, AdsPower, MoreLogin…).

- **Постоянный IPv6 для каждого прокси.** У каждого прокси свой порт, логин, пароль и исходящий IPv6 — они не меняются, пока вы их не измените.
- **Любая IPv6-сеть.** Домашний интернет (/64 в локальной сети; если провайдер сменит префикс, openv6 подстроится и сохранит порты и пароли) и VPS или домашние серверы с маршрутизируемой /64, /56 или /48.
- **Клиенты подключаются по IPv4** (локальный IP машины или публичный IP VPS), а трафик выходит через IPv6.
- **Windows, macOS и Linux.** Любая из них может запускать прокси («узел»). Несколько машин управляются из одной панели.
- **Сам проверяет и чинит.** Регулярная проверка IP, понятные причины ошибок, исправление в один клик, уведомления в Telegram.
- **Работает при включённом VPN**, а доступ извне локальной сети — через relay на VPS, UPnP или Tailscale.
- **Один исполняемый файл.** Без Docker и отдельного прокси-сервера. Панель на английском и вьетнамском, светлая и тёмная тема, удобна на телефоне.

## Установка

| Система | Как |
|---|---|
| **Windows 10/11** | Скачайте `openv6-setup-windows-amd64.exe` в [Releases](https://github.com/synthcoz/openv6/releases/latest) и нажмите Next → Install. |
| **macOS** (Intel и Apple silicon) | Скачайте `openv6-macos.pkg` в Releases. |
| **Ubuntu / Debian / Zorin** (amd64, arm64, armhf) | Команда ниже или файл `.deb` в Releases. |

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
```

openv6 работает в фоне и запускается вместе с системой. Значок в трее показывает состояние и открывает панель.

1. Откройте панель (`http://<локальный-IP-машины>:8080` или значок в трее) и создайте администратора.
2. **Proxies → Create proxies**, затем **Export** в формате вашего антидетект-браузера.
3. Все устройства в сети используют `локальный-IP-машины:порт` (или публичный IP VPS).

Чтобы добавить другую машину (VPS, мини-ПК, другой компьютер): **Nodes → Add node** и выполните показанную команду на той машине.

## Free и Pro

| | Free | Pro (разовая оплата) |
|---|---|---|
| Прокси | 3 | Без ограничений |
| Машин с прокси | 1 | Несколько (VPS, мини-ПК, ПК) |
| HTTP + SOCKS5, экспорт, самопроверка | ✓ | ✓ |
| Доступ через relay / UPnP / Tailscale, API, аккаунты клиентов, Telegram, сетевая карта для каждого прокси |  | ✓ |

Купите лицензию на [сайте](https://thephuc.xyz/openv6/#pricing) и введите её в **Settings → License**.

## Команды

```
openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
openv6 agent   -panel URL -token TOKEN [-data DIR]
openv6 install server|agent [flags]   # установить как службу (root / администратор)
openv6 uninstall                      # удалить службу и все IPv6, добавленные openv6
openv6 tray                           # значок в трее
openv6 reset-password [-data DIR] [-user NAME]
openv6 version
```

## Документация

Подробные руководства пока на вьетнамском.

| Файл | Тема |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Windows 10/11: значок в трее, LAN/Wi-Fi, брандмауэр |
| [docs/ZORIN.md](docs/ZORIN.md) | Пошаговая установка на Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Полное руководство: проверка IPv6, домашний интернет, VPS, решение проблем |
| [docs/API.md](docs/API.md) | JSON API и API-токены |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Архитектура, назначение IPv6, протокол панель ↔ агент |
| [docs/RELEASE.md](docs/RELEASE.md) | Выпуск новой версии, подписанные обновления |

## Разработка

```bash
make test   # модульные тесты
make dev    # панель в режиме -dry-run (без изменений сети), http://localhost:18080
make dist   # сборки для Linux в dist/
```

Лендинг находится в [`site/`](site) и развёрнут на Cloudflare (см. [deploy/cloudflare](deploy/cloudflare)).
