# Changelog

## v0.3.0

**Chạy ổn định khi máy bật VPN (Cloudflare WARP, Tailscale, WireGuard, VPN thương mại)**
- Agent chọn card mạng theo prefix /64 thật, không theo đường ra mặc định. Trước đây, bật WARP hoặc exit node làm pool `auto` mất prefix và mọi proxy báo lỗi.
- Bỏ qua địa chỉ nội bộ `fc00::/7` (ví dụ Tailscale `fd7a:…`) khi tìm prefix.
- **Định tuyến proxy vòng qua VPN** (Linux, bật mặc định): mỗi IP proxy có một luật định tuyến riêng (`ip -6 rule … lookup 6464 pref 100`) để luôn đi qua card mạng thật và router của nhà mạng. Lưu lượng của chính máy vẫn đi qua VPN. Agent ghi nhớ router để vẫn hoạt động khi VPN xoá đường ra mặc định.
- Nhận diện VPN: Cloudflare WARP, Tailscale, WireGuard, NordVPN, Proton VPN, OpenVPN/tun, ZeroTier, PPP. Phát hiện IPv6 bị tắt (chống rò IP của VPN).
- **DNS riêng cho proxy** theo node: theo hệ thống, Cloudflare, Google, Quad9 hoặc tự nhập. DNS IPv6 được hỏi từ chính IP của proxy.
- Lệnh `sudo openv6 selftest`: in tình trạng IPv6/VPN của máy và kiểm tra định tuyến vòng qua VPN trong một vùng mạng giả lập, không đụng tới mạng thật.

**Phạm vi chia sẻ cho từng proxy**
- *Chỉ máy này* (bind 127.0.0.1), *Mạng LAN* (theo whitelist; thiết bị Tailscale 100.x cũng tính là LAN), *Internet* (bắt buộc có mật khẩu).
- Khi nâng cấp từ bản cũ: proxy có mật khẩu chuyển thành *Internet*, proxy không mật khẩu chuyển thành *LAN* (giữ nguyên cách hoạt động như trước).

**Dịch vụ và thông báo lỗi**
- Node hiện thời gian đã chạy; có nút *Khởi động lại* và *Áp dụng lại mạng* (gỡ rồi gán lại toàn bộ IPv6 và luật định tuyến).
- Mọi lỗi của proxy và kết quả kiểm tra đều có mã lỗi, kèm giải thích và cách sửa bằng tiếng Việt và tiếng Anh: port bị chiếm, IPv6 không còn trên máy, quá thời gian chờ (VPN kill switch), website không có IPv6, website thấy sai IP… Chi tiết kỹ thuật vẫn xem được.
- Tự chẩn đoán có thêm: IPv6 đang bật, card mạng IPv6, router IPv6, tình trạng VPN. Tổng quan hiện từng vấn đề kèm cách sửa.

## v0.2.0

**Ổn định và vận hành**
- Tự kiểm tra định kỳ toàn bộ proxy (mặc định 10 phút một lần): IP đi ra có đúng không, độ trễ, quốc gia, nhà mạng (ASN). Lưu lịch sử 200 lần kiểm tra gần nhất cho mỗi proxy.
- Cảnh báo Telegram khi: node mất kết nối hoặc kết nối lại, proxy lỗi 2 lần liên tiếp hoặc hoạt động lại, nhà mạng đổi prefix IPv6, có phiên bản mới.
- Node tự chẩn đoán: iproute2, route IPv6, prefix, quyền root, tinh chỉnh kernel, tường lửa. Mỗi mục lỗi kèm gợi ý cách sửa.
- Tự tăng giới hạn kernel (bảng neighbor IPv6, somaxconn, file-max). Lệnh cài ghi thêm `/etc/sysctl.d/99-openv6.conf` để giữ cấu hình sau khi khởi động lại.
- Tự mở tường lửa ufw cho các port proxy. openv6 chỉ thêm hoặc xoá đúng các luật do mình tạo. Khi cài panel, port 8080 được mở cho mạng LAN.
- Cập nhật một nút bấm: panel tự tải bản mới từ GitHub Releases rồi khởi động lại; các node cập nhật từ panel.
- Sao lưu và khôi phục toàn bộ cấu hình bằng một file JSON; panel tự sao lưu mỗi ngày (giữ 14 bản).
- Xem log của panel và của từng node ngay trên web, có lọc và tự làm mới.

**Dễ dùng**
- Đặt tên cho proxy (tên profile hoặc tài khoản); khi tạo hàng loạt có thể tự đặt tên theo mẫu "FB 01, FB 02…".
- Xuất danh sách theo mẫu có sẵn cho Multilogin, GoLogin, Dolphin {Anty}, ixBrowser, Linken Sphere 2, AdsPower.
- Nút "Kiểm tra tất cả"; tổng quan hiện số proxy đang lỗi và thông báo khi có bản mới.

**Kỹ thuật**
- Cơ chế nâng cấp cơ sở dữ liệu (migration) theo phiên bản.
- Test tích hợp cho panel: luồng tạo proxy, đổi prefix, xuất danh sách, kiểm tra sức khoẻ, log, sao lưu/khôi phục, cài đặt.

## v0.1.0
- Bản đầu tiên: proxy IPv6 tĩnh HTTP + SOCKS5, panel quản lý nhiều node, tạo hàng loạt, xuất danh sách, giao diện tiếng Việt và tiếng Anh.
