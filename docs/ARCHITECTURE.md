# Kiến trúc openv6

## Tổng quan

```
                    ┌─────────────────────── Panel (openv6 server) ───────────────────────┐
 Trình duyệt ──────►│  Web UI (web/, nhúng vào binary)   REST API /api/*   SQLite            │
                    │                                    /api/agent/sync ◄───────────┐      │
                    │  agent nội bộ (node "máy này") ───────────────────────────────┤      │
                    └───────────────────────────────────────────────────────────────┼──────┘
                                                                                    │ HTTP(S) poll 5s
                                    ┌───────────────── Node (openv6 agent) ────────┴──────┐
 Anti-detect ── IPv4:port ────────► │ proxy.Manager: 1 listener/port, HTTP+SOCKS5 ─► IPv6 ──┼──► Internet
                                    │ netcfg: gán /128 hoặc route anyip                     │
                                    └───────────────────────────────────────────────────────┘
```

- Một binary duy nhất (Go, không cần CGO): `server` = panel + agent nội bộ, `agent` = node ở xa.
- **Agent luôn chủ động kết nối tới panel** (polling), nên node nằm sau NAT/CGNAT vẫn hoạt động. Mất kết nối panel thì node vẫn chạy với cấu hình cuối cùng.
- Panel là nơi lưu “trạng thái mong muốn”, agent chịu trách nhiệm *reconcile* (đưa máy về đúng trạng thái đó), theo kiểu Kubernetes.

## Thư mục mã nguồn

| Đường dẫn | Vai trò |
|---|---|
| `main.go` | CLI: server, agent, install, uninstall, reset-password; nhúng `web/` |
| `internal/v6` | Ghép địa chỉ: `prefix + subnet index + suffix 64-bit` |
| `internal/store` | SQLite (modernc, pure Go): users, sessions, nodes, pools, proxies, events, settings |
| `internal/panel` | HTTP server, xác thực, REST API, endpoint sync cho agent, install script, export |
| `internal/agent` | Vòng lặp sync, áp cấu hình, gửi thống kê, chạy lệnh kiểm tra |
| `internal/netcfg` | Đọc `/proc/net/if_inet6`, `/proc/net/ipv6_route`; gọi `ip` để thêm/xoá địa chỉ, route |
| `internal/proxy` | Engine proxy: HTTP (CONNECT + forward) và SOCKS5 chung một port |
| `internal/proto` | Định nghĩa JSON giữa panel và agent |
| `web/` | SPA thuần JS (`app.js`), bản dịch (`i18n.js`), CSS |

## Cách tính IPv6 của một proxy

Mỗi proxy lưu **subnet index** và **suffix** (64 bit thấp, dạng hex), **không lưu địa chỉ đầy đủ**:

```
IP = pool.prefix | (subnet << 64) | suffix
```

- Pool `auto` (/64): subnet = 0. Khi nhà mạng đổi prefix, panel cập nhật `pool.cidr`, các proxy tự nhận IP mới mà giữ nguyên port, mật khẩu và suffix.
- Pool `/48` hoặc `/56`: chọn “mỗi proxy một /64” để mỗi proxy nhận một subnet index ngẫu nhiên, không trùng nhau.
- Suffix ngẫu nhiên, tránh các giá trị nhỏ (`::1`, `::2` thường do router dùng).

## Hai chế độ gán địa chỉ (netcfg)

| Chế độ | Khi nào | Cách làm |
|---|---|---|
| `addr` | /64 **on-link** (mạng nhà, đa số VPS) | `ip -6 addr replace IP/128 dev IF nodad valid_lft forever preferred_lft 0`. Kernel tự trả lời Neighbor Solicitation, nên **không cần ndppd**. `preferred_lft 0` đánh dấu địa chỉ “deprecated” để máy không dùng nó làm IP nguồn mặc định cho lưu lượng của chính máy, nhưng vẫn bind tường minh được. |
| `anyip` | Dải **được route** về máy (/48, /56) | `ip -6 route replace local CIDR dev lo` + `net.ipv6.ip_nonlocal_bind=1`. Bind được mọi IP trong dải mà không cần thêm từng địa chỉ. |

Agent ghi lại các địa chỉ và route đã thêm vào `<data>/netstate.json`, để chỉ xoá đúng những gì nó tạo ra. Mỗi lần sync, agent kiểm tra lại `/proc/net/if_inet6`; địa chỉ nào mất (do reboot hoặc rút cáp) sẽ được thêm lại.

## Engine proxy

- Mỗi proxy là một `net.Listener` trên `:port` (dual-stack). Byte đầu tiên `0x05` nghĩa là SOCKS5, ngược lại là HTTP.
- HTTP: `CONNECT` tạo tunnel; request dạng `http://` tuyệt đối được forward qua `http.Transport` riêng của từng kết nối (giữ keep-alive). Xác thực bằng `Proxy-Authorization: Basic`.
- SOCKS5: RFC 1928 + RFC 1929 (user/pass), lệnh CONNECT, địa chỉ IPv4/IPv6/domain. **DNS phân giải tại proxy** (dùng `socks5h` hoặc remote DNS trong anti-detect).
- Kết nối đi ra: phân giải tên miền, chỉ thử bản ghi AAAA và bind `LocalAddr` = IPv6 của proxy. Bản ghi A chỉ được dùng khi bật `ipv4_fallback`.
- Proxy không mật khẩu chỉ nhận client có IP nằm trong whitelist của node. Proxy có mật khẩu nhận mọi nguồn.
- Đổi cấu hình nóng: thay `Config` bằng `atomic.Pointer`. Nếu IP hoặc mật khẩu thay đổi, các kết nối đang mở bị ngắt để client chuyển sang danh tính mới.
- Thống kê: bộ đếm atomic (byte lên/xuống, số kết nối, đang mở). Agent gửi phần **chênh lệch** mỗi lần sync, panel cộng dồn vào DB.

## Giao thức panel ↔ agent

`POST /api/agent/sync`, header `Authorization: Bearer <node token>`, chu kỳ 5 giây.

- Agent gửi: phiên bản, hostname, trạng thái mạng (interface, prefix /64, route mặc định, IPv4 LAN/WAN), thống kê chênh lệch, trạng thái áp dụng từng proxy, kết quả lệnh, lỗi.
- Panel trả về: card mạng, whitelist, danh sách pool, danh sách proxy (đã tính sẵn IP), lệnh chờ chạy (`test_proxy`, `test_v6`).

Lệnh kiểm tra: agent gọi `https://api6.ipify.org` (dự phòng `ipv6.icanhazip.com`) với kết nối bind vào IP của proxy, rồi so IP mà website thấy với IP mong đợi.

## Bảo mật

- Mật khẩu admin dùng bcrypt; session là cookie `HttpOnly`, `SameSite=Strict`, token lưu dạng SHA-256.
- Chống CSRF: request ghi dữ liệu bắt buộc `Content-Type: application/json` (không gửi được từ form của trang lạ mà không qua preflight CORS).
- Chống dò mật khẩu: tối đa 10 lần sai mỗi 5 phút cho mỗi IP.
- Token node là ngẫu nhiên 192 bit, có thể đổi (rotate) trong giao diện. Token không bao giờ được trả về trong danh sách node.
- Hạn chế hiện tại: panel chưa tự cấp HTTPS (phải dùng `-tls-cert` hoặc reverse proxy). Mật khẩu proxy lưu dạng rõ, vì cần để xuất danh sách.

## REST API (cho UI và tích hợp sau này)

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/status` | Cần setup không? User hiện tại |
| POST | `/api/setup`, `/api/login`, `/api/logout` | Tài khoản |
| GET | `/api/dashboard` | Số liệu tổng quan + sự kiện gần đây |
| GET/POST | `/api/nodes` | Danh sách / tạo node |
| PUT/DELETE | `/api/nodes/{id}` | Sửa / xoá node |
| GET | `/api/nodes/{id}/install` | Lệnh cài agent |
| POST | `/api/nodes/{id}/rotate-token`, `/api/nodes/{id}/test` | Đổi token / kiểm tra IPv6 |
| GET/POST | `/api/pools` | Danh sách / thêm pool |
| PUT/DELETE | `/api/pools/{id}` | Sửa / xoá pool |
| GET | `/api/proxies` | Danh sách proxy kèm IP, trạng thái, thống kê |
| POST | `/api/proxies/bulk` | Tạo hàng loạt |
| PUT | `/api/proxies/{id}` | Sửa (port, auth, IP cụ thể, ghi chú, hạn…) |
| POST | `/api/proxies/action` | `enable, disable, delete, regen_ip, regen_pass, remove_auth, set_tag, set_protocol, fallback_on/off, reset_stats, test` |
| POST | `/api/proxies/export` | Xuất `host:port:user:pass`, `user:pass@host:port`, URL, CSV, JSON |
| GET | `/api/commands/{id}` | Kết quả lệnh kiểm tra |
| GET | `/api/events` | Nhật ký |
| GET/PUT | `/api/settings` | Cài đặt panel |
| POST | `/api/account/password` | Đổi mật khẩu |
