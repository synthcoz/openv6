<p align="center"><img src="site/assets/logo-256.png" width="72" alt="openv6"></p>
<h1 align="center">openv6</h1>
<p align="center"><b>Proxy IPv6 tĩnh (HTTP + SOCKS5) từ chính mạng của bạn, quản lý trên một dashboard.</b></p>
<p align="center"><a href="https://thephuc.xyz/openv6/">Trang chủ</a> · <a href="https://github.com/synthcoz/openv6/releases/latest">Tải về</a> · <a href="CHANGELOG.md">Nhật ký thay đổi</a></p>
<p align="center"><a href="README.md">English</a> · <b>Tiếng Việt</b> · <a href="README.zh-CN.md">简体中文</a> · <a href="README.es.md">Español</a> · <a href="README.ru.md">Русский</a> · <a href="README.pt-BR.md">Português</a></p>

![openv6 dashboard](site/assets/shot-proxies.webp)

openv6 biến dải IPv6 mà nhà mạng hoặc nhà cung cấp VPS cấp cho bạn thành hàng trăm proxy cố định, mỗi tài khoản một proxy. Được làm cho trình duyệt anti-detect (Multilogin, GoLogin, Dolphin Anty, ixBrowser, AdsPower, MoreLogin…).

- **Mỗi proxy một IPv6 cố định.** Mỗi proxy có port, user, mật khẩu và IPv6 đi ra riêng, giữ nguyên cho tới khi bạn đổi.
- **Mọi mạng có IPv6.** Mạng nhà (dải /64 trong LAN; nhà mạng đổi prefix thì openv6 tự đổi theo, giữ nguyên port và mật khẩu) và VPS hoặc homelab có dải /64, /56, /48 được route về.
- **Thiết bị kết nối bằng IPv4** (IP LAN của máy, hoặc IP public của VPS); proxy đi ra Internet bằng IPv6.
- **Windows, macOS và Linux.** Máy nào cũng chạy proxy được (gọi là node). Nhiều máy quản lý chung trên một dashboard.
- **Tự kiểm tra và tự sửa lỗi.** Kiểm tra IP định kỳ, báo rõ nguyên nhân, sửa bằng một nút bấm, cảnh báo Telegram.
- **Ổn định khi bật VPN**, chia sẻ ra ngoài mạng nhà qua relay trên VPS, UPnP hoặc Tailscale.
- **Một file chạy duy nhất.** Không cần Docker, không cần proxy server khác. Dashboard tiếng Việt và tiếng Anh, sáng và tối, dùng tốt trên điện thoại.

## Cài đặt

| Hệ điều hành | Cách cài |
|---|---|
| **Windows 10/11** | Tải `openv6-setup-windows-amd64.exe` ở [Releases](https://github.com/synthcoz/openv6/releases/latest), bấm Next → Install. |
| **macOS** (Intel và Apple silicon) | Tải `openv6-macos.pkg` ở Releases. |
| **Ubuntu / Debian / Zorin** (amd64, arm64, armhf) | Lệnh bên dưới, hoặc file `.deb` ở Releases. |

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
```

openv6 chạy nền và tự khởi động cùng máy. Icon ở khay hệ thống cho biết trạng thái và mở dashboard.

1. Mở dashboard (`http://<IP-LAN-của-máy>:8080`, hoặc bấm icon khay) và tạo tài khoản admin.
2. **Proxy → Tạo proxy**, rồi **Xuất danh sách** theo định dạng của trình duyệt anti-detect.
3. Mọi thiết bị trong mạng dùng `IP-LAN-của-máy:port` (hoặc IP public của VPS).

Thêm máy khác (VPS, mini PC, máy tính khác): **Máy chủ (node) → Thêm node**, rồi chạy lệnh hiện ra trên máy đó.

## Free và Pro

| | Free | Pro (trả một lần) |
|---|---|---|
| Số proxy | 3 | Không giới hạn |
| Số máy chạy proxy | 1 | Nhiều máy (VPS, mini PC, máy tính) |
| HTTP + SOCKS5, xuất danh sách, tự kiểm tra | ✓ | ✓ |
| Chia sẻ qua relay / UPnP / Tailscale, API, tài khoản khách, Telegram, card mạng riêng từng proxy |  | ✓ |

Mua license trên [trang chủ](https://thephuc.xyz/openv6/#pricing) rồi nhập vào **Cài đặt → License**.

## Lệnh

```
openv6 server  [-listen :8080] [-data DIR] [-no-local-agent] [-tls-cert FILE -tls-key FILE]
openv6 agent   -panel URL -token TOKEN [-data DIR]
openv6 install server|agent [flags]   # cài làm service (root / Administrator)
openv6 uninstall                      # gỡ service và xoá mọi IPv6 openv6 đã thêm
openv6 tray                           # icon khay
openv6 reset-password [-data DIR] [-user NAME]
openv6 version
```

## Tài liệu

| File | Nội dung |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Windows 10/11: icon khay, LAN/Wi-Fi, tường lửa |
| [docs/ZORIN.md](docs/ZORIN.md) | Cài từng bước trên Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Hướng dẫn đầy đủ: kiểm tra IPv6, mạng nhà, VPS, xử lý lỗi |
| [docs/API.md](docs/API.md) | API JSON và API token |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Kiến trúc, cách gán IPv6, giao thức panel ↔ agent |
| [docs/RELEASE.md](docs/RELEASE.md) | Phát hành phiên bản mới, cập nhật có chữ ký |

## Phát triển

```bash
make test   # unit test
make dev    # dashboard ở chế độ -dry-run (không đụng cấu hình mạng), http://localhost:18080
make dist   # bản build Linux vào dist/
```

Trang landing nằm trong [`site/`](site) và được deploy lên Cloudflare (xem [deploy/cloudflare](deploy/cloudflare)).
