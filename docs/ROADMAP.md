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
| ✅ Giới hạn tốc độ / số kết nối theo proxy (v0.6) | Còn thiếu: quota dung lượng theo tháng. |

## Đã xong ở v0.3
- Chạy ổn định khi có VPN, WARP hoặc Tailscale (định tuyến proxy vòng qua VPN, DNS riêng, tự chẩn đoán, `openv6 selftest`).
- Phạm vi chia sẻ Chỉ máy này / LAN / Internet; Khởi động lại; Áp dụng lại mạng; mã lỗi kèm hướng dẫn sửa.

## Đã xong ở v0.4–v0.5
- Windows và macOS (dịch vụ nền, icon khay, bộ cài), một file `.exe` dạng giao diện, tự đổi port khi 8080 bị chiếm, icon khay tự chẩn đoán và tạo báo cáo lỗi.
- Giao diện mới (v0.5): trình hướng dẫn lần đầu, bảng chi tiết proxy, thao tác hàng loạt, hoàn tác, giao diện sáng/tối/theo hệ thống, VI/EN, dùng tốt trên điện thoại.

## Đã xong ở v0.6
- Chia sẻ ra Internet: relay qua VPS (TLS + ghim vân tay chứng chỉ, multiplex yamux), UPnP (tự phát hiện CGNAT), nhận diện Tailscale.
- Giới hạn số kết nối và tốc độ theo proxy; API token (xem `docs/API.md`).

## Kế hoạch tiếp theo (cũ, giữ để tham khảo)
- **v0.4: node chạy trên Windows.** Gán IPv6 bằng `New-NetIPAddress -SkipAsSource $true`, chạy như Windows Service, bộ cài `.exe`. Trên Windows, mô hình "strong host" giữ gói tin đi đúng card mạng của IP nguồn, cần kiểm chứng khi bật WARP hoặc VPN.
- **v0.5: chia sẻ ra Internet từ mạng nhà:** qua Tailscale (hướng dẫn và tự nhận diện), UPnP (khi modem có IPv4 public), hoặc chuyển tiếp qua VPS (máy nhà tự kết nối lên VPS, dùng được cả khi nằm sau CGNAT).
- **v0.6: làm lại giao diện** dựa trên các tính năng đã ổn định.

## Đợt giao diện (đã chốt nội dung, làm ở v0.6)

- Trình hướng dẫn lần đầu (kiểm tra IPv6 → IP LAN → tạo proxy → xuất).
- Lấy tên profile làm trung tâm, sao chép nhanh theo định dạng anti-detect trên từng dòng.
- Bảng chi tiết proxy (IP, quốc gia, nhà mạng, lịch sử kiểm tra, biểu đồ lưu lượng).
- Làm lại giao diện: icon, thanh trạng thái hệ thống, màn hình trống có hướng dẫn, nút hoàn tác, chế độ đơn giản cho người chỉ có một máy.

## Giai đoạn 2: đóng gói thành sản phẩm (mục tiêu: bán phần mềm)

| Tính năng | Ghi chú |
|---|---|
| **Nhiều người dùng và phân quyền** | Bảng `users` đã có cột `role`. Thêm vai trò `admin / operator / viewer`, gán proxy theo user (`proxies.owner_id`). |
| **Chế độ khách hàng (reseller)** | Khách hàng đăng nhập chỉ thấy proxy của mình, tự đổi IP hoặc mật khẩu trong giới hạn cho phép. |
| Tài liệu OpenAPI | API token đã có (v0.6, `docs/API.md`); còn thiếu `openapi.yaml` và token giới hạn quyền (chỉ đọc). |
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
