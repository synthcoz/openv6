# openv6

Công cụ tạo **proxy IPv6 tĩnh** (HTTP + SOCKS5) từ dải IPv6 mà nhà mạng hoặc nhà cung cấp VPS cấp cho bạn. Mọi thứ được quản lý qua **một web panel chung**, kể cả khi có nhiều máy (mạng nhà, mini PC, VPS, homelab).

- Mỗi proxy có **port riêng, user/pass riêng và IPv6 đi ra riêng**, cố định cho tới khi bạn tự đổi. Phù hợp để gắn vào trình duyệt anti-detect (GoLogin, GenLogin, Hidemyacc, MoreLogin…).
- Thiết bị **kết nối vào proxy bằng IPv4** (IP LAN trong nhà, IP public với VPS), proxy **đi ra Internet bằng IPv6**.
- Hỗ trợ **mạng gia đình** (prefix /64 on-link, prefix bị nhà mạng đổi thì tự cập nhật) và **VPS hoặc homelab** có dải /48 hay /56 được route về máy (mỗi proxy nằm ở một /64 riêng).
- Giao diện **tiếng Việt và tiếng Anh**, sáng và tối, dùng được trên điện thoại.
- Chỉ **một file chạy duy nhất**, không cần Docker, không cần cài thêm proxy server khác.

## Cài đặt

| Hệ điều hành | Cách cài |
|---|---|
| **Windows 10/11** | Tải `openv6-setup-windows-amd64.exe` ở [Releases](https://github.com/synthcoz/openv6/releases/latest), bấm Next → Install. Xem [docs/WINDOWS.md](docs/WINDOWS.md) |
| **macOS** | Tải `openv6-macos.pkg` ở Releases |
| **Ubuntu / Zorin / Debian** | Lệnh bên dưới, hoặc file `.deb` ở Releases |

Sau khi cài, openv6 chạy nền và tự khởi động cùng máy. Icon ở khay hệ thống cho biết trạng thái và mở dashboard chỉ bằng một cú bấm.

### Linux: cài bằng một lệnh

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
```

Mở trình duyệt `http://<IP-LAN-của-máy>:8080` để tạo tài khoản admin. Hướng dẫn từng bước cho Zorin: [docs/ZORIN.md](docs/ZORIN.md).

Sau đó vào **Proxy → Tạo proxy**, rồi bấm **Xuất danh sách** để copy vào anti-detect. Mọi thiết bị trong nhà (Windows, điện thoại…) dùng `IP-LAN-của-máy:port`.

Thêm máy khác (VPS, mini PC): vào **Máy chủ (node) → Thêm node** để lấy lệnh cài, rồi dán lệnh đó vào máy kia.

Hướng dẫn chi tiết (FPT, VPS, Windows, xử lý lỗi): [docs/HUONG-DAN.md](docs/HUONG-DAN.md)

## Tài liệu

| File | Nội dung |
|---|---|
| [docs/WINDOWS.md](docs/WINDOWS.md) | Cài và dùng trên Windows 10/11 (icon khay, dây LAN/Wi-Fi, tường lửa) |
| [docs/ZORIN.md](docs/ZORIN.md) | Cài từng bước trên Zorin OS / Ubuntu |
| [docs/HUONG-DAN.md](docs/HUONG-DAN.md) | Cài đặt và sử dụng, kiểm tra IPv6, mạng FPT, VPS, Windows, xử lý lỗi |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Kiến trúc, luồng dữ liệu, cách gán IPv6, giao thức panel↔agent, API |
| [docs/RELEASE.md](docs/RELEASE.md) | Cách phát hành phiên bản mới (GitHub Actions) |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Tính năng nâng cao để phát triển tiếp (thương mại hoá, đa người dùng…) |

## Lệnh

```
openv6 server  [-listen :8080] [-data /var/lib/openv6] [-no-local-agent] [-tls-cert F -tls-key F]
openv6 agent   -panel URL -token TOKEN [-data /var/lib/openv6-agent] [-insecure-tls]
openv6 install server|agent [flags]   # cài systemd service
openv6 uninstall                      # gỡ service và xoá các IPv6 đã thêm
openv6 reset-password [-data DIR]     # quên mật khẩu admin
```

## Phát triển

```bash
make test        # unit test
make dev         # chạy panel ở chế độ -dry-run (không đụng cấu hình mạng), http://localhost:18080
make dist        # build linux amd64 + arm64 + armv7 vào dist/
```

Lưu ý: chỉ máy chạy proxy (node) cần Linux. Windows, macOS và điện thoại chỉ là máy dùng proxy.
