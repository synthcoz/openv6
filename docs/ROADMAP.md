# Lộ trình phát triển

Phiên bản 0.2 đã có kiểm tra định kỳ, cảnh báo Telegram, tự chẩn đoán, tự mở tường lửa, cập nhật một nút bấm, sao lưu/khôi phục và log trên web (xem CHANGELOG.md). Phiên bản 0.1 đủ để dùng cá nhân: nhiều node, panel chung, proxy tĩnh HTTP+SOCKS5, tạo hàng loạt, xuất danh sách, kiểm tra IP, thống kê, song ngữ. Dưới đây là các tính năng **nâng cao** để làm tiếp, xếp theo mức ưu tiên.

## Giai đoạn 1: hoàn thiện cho dùng cá nhân

| Tính năng | Ghi chú kỹ thuật |
|---|---|
| **DHCPv6 Prefix Delegation** cho mạng nhà | Xin thêm một prefix /60–/64 từ modem FPT (nếu modem cho phép PD xuống LAN), sau đó tạo pool `anyip` tự động để có **nhiều /64** ngay tại nhà. Cần client DHCPv6-PD (ví dụ thư viện `github.com/insomniacslk/dhcp`) và tự động gia hạn lease. |
| NDP proxy tích hợp | Cho chế độ `anyip` với dải on-link lớn (hàng nghìn IP): trả lời Neighbor Solicitation bằng raw ICMPv6 (`github.com/mdlayher/ndp`), thay cho việc thêm từng /128. |
| HTTPS tự động | Tích hợp Let's Encrypt (`golang.org/x/crypto/acme/autocert`) khi panel có tên miền. |
| SOCKS5 UDP ASSOCIATE | Cần cho WebRTC/QUIC qua proxy; hiện proxy trả lỗi “command not supported”. |
| ✅ Kiểm tra định kỳ tự động | Tự test mọi proxy mỗi N phút và cảnh báo khi IP sai hoặc proxy chết. |
| ✅ Cảnh báo qua Telegram/Discord | Node offline, prefix thay đổi, proxy lỗi. |
| ✅ Sao lưu và khôi phục | Xuất/nhập toàn bộ DB (JSON) trong giao diện. |
| Kiểm tra IPv6 của website đích | Nút “website này có IPv6 không?” để biết trước proxy có dùng được hay không. |
| ✅ Cập nhật agent từ panel | Panel giữ binary mới, agent tự tải về và khởi động lại. |
| Giới hạn tốc độ / dung lượng theo proxy | Token bucket trong `pipe()`, chặn khi vượt quota tháng. |

## Đợt 2 (đã chốt): giao diện

- Trình hướng dẫn lần đầu (kiểm tra IPv6 → IP LAN → tạo proxy → xuất).
- Lấy tên profile làm trung tâm, sao chép nhanh theo định dạng anti-detect trên từng dòng.
- Bảng chi tiết proxy (IP, quốc gia, nhà mạng, lịch sử kiểm tra, biểu đồ lưu lượng).
- Làm lại giao diện: icon, thanh trạng thái hệ thống, màn hình trống có hướng dẫn, nút hoàn tác, chế độ đơn giản cho người chỉ có một máy.

## Giai đoạn 2: đóng gói thành sản phẩm (mục tiêu: bán phần mềm)

| Tính năng | Ghi chú |
|---|---|
| **Nhiều người dùng và phân quyền** | Bảng `users` đã có cột `role`. Thêm vai trò `admin / operator / viewer`, gán proxy theo user (`proxies.owner_id`). |
| **Chế độ khách hàng (reseller)** | Khách hàng đăng nhập chỉ thấy proxy của mình, tự đổi IP hoặc mật khẩu trong giới hạn cho phép. |
| API token + tài liệu OpenAPI | Cho phép tool khác (anti-detect, script nuôi tài khoản) tự tạo/xoá/lấy proxy. Viết `openapi.yaml` từ bảng API trong ARCHITECTURE.md. |
| Tích hợp trực tiếp anti-detect | Gọi API của GoLogin, GenLogin, MoreLogin, Hidemyacc… để gán proxy thẳng vào profile, không cần copy/paste. |
| Nhật ký kiểm toán (audit log) | Ai đã làm gì, lúc nào. Bảng `events` hiện chỉ ghi sự kiện hệ thống. |
| Thanh toán và hạn dùng | Đã có `expires_at`. Thêm gói, gia hạn, cổng thanh toán (VNPay/MoMo/Stripe). |
| Mã hoá mật khẩu proxy trong DB | Mã hoá bằng khoá master (AES-GCM) thay vì lưu dạng rõ. |
| 2FA (TOTP) cho panel | |
| Bản quyền / license key | Nếu bán dạng phần mềm cài đặt. |
| Gói cài đặt | `.deb`, Docker image (`--network host --cap-add NET_ADMIN`), template LXC cho Proxmox, script trên GitHub Releases. |
| Giao diện dùng framework | Khi giao diện lớn hơn: chuyển sang Svelte/React + build bằng Vite, vẫn nhúng vào binary. |

## Giai đoạn 3: quy mô lớn

| Tính năng | Ghi chú |
|---|---|
| Hàng nghìn proxy mỗi node | Chuyển hẳn sang `anyip` + NDP proxy; một listener dùng chung với phân biệt theo username (`user-<id>`) để không tốn hàng nghìn port. |
| Kênh panel↔agent dạng push | WebSocket/gRPC thay cho polling 5 giây, phản hồi tức thì. |
| PostgreSQL | Thay SQLite khi có nhiều panel hoặc nhiều người dùng đồng thời. |
| Pool IPv4 nhiều IP | Engine đã hỗ trợ bind IPv4 (`OutIP` dạng v4). Thêm loại pool IPv4 cho VPS có nhiều IPv4. |
| Xoay IP / sticky session | Không cần cho nuôi tài khoản, nhưng hữu ích cho scraping nếu bán dịch vụ. |
| Monitoring | Endpoint `/metrics` cho Prometheus, dashboard Grafana. |
| Agent cho FreeBSD/OpenWrt | Chạy trực tiếp trên router. |

## Nợ kỹ thuật đã biết

- ✅ Đã có test tích hợp cho panel (`internal/panel/panel_test.go`).
- Chưa kiểm thử trên mạng IPv6 thật trong CI. Cần một máy runner có IPv6 (VPS) để chạy e2e: tạo proxy rồi `curl` qua proxy và so IP.
- `regen_ip` với pool lớn chọn subnet ngẫu nhiên nhưng chưa tránh trùng với proxy khác.
- Lệnh kiểm tra phụ thuộc dịch vụ bên ngoài (ipify, icanhazip). Nên cho cấu hình URL kiểm tra hoặc tự host một endpoint.
