# Changelog

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
