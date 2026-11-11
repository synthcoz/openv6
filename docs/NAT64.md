# Vào trang chỉ có IPv4 mà không lộ IPv4 nhà (NAT64)

Proxy openv6 đi ra bằng IPv6. Với trang **chỉ có IPv4**, bắt buộc phải có một địa chỉ IPv4 đi ra. Có ba cách:

| Cách | Trang thấy IPv4 nào | Ghi chú |
|---|---|---|
| “Dự phòng IPv4” trên proxy, hoặc danh sách *Trang được đi bằng IPv4* (không có NAT64) | IPv4 của máy chạy proxy (IP nhà) | Mọi proxy dùng chung một IPv4 |
| **NAT64 trên VPS của bạn** (khuyên dùng) | IPv4 của VPS | Không lộ IP nhà; một VPS cho một IPv4 sạch, không chung với ai |
| NAT64 công cộng | IPv4 của dịch vụ, dùng chung với nhiều người lạ | Không cần VPS, nhưng IP có thể đã bị nền tảng đánh dấu; tốc độ không bảo đảm |

Với NAT64, proxy **vẫn đi bằng IPv6** (từ chính IPv6 của proxy) tới một địa chỉ đặc biệt dạng `<prefix NAT64>::<IPv4 của trang>`. Cổng NAT64 nhận gói đó và mở kết nối IPv4 tới trang bằng IPv4 của chính nó.

## Cài đặt trong openv6

*Cài đặt → Truy cập trang web → Khi trang chỉ có IPv4* có ba lựa chọn:

| Lựa chọn | Trang chỉ có IPv4 | Lộ IP thật? |
|---|---|---|
| **Không cho đi IPv4** (mặc định cho bản cài mới) | Không mở được | Không bao giờ |
| **Đi qua cổng NAT64** | Thấy IPv4 của cổng NAT64 | Không. Nếu prefix sai hoặc cổng tắt, trang không mở được nhưng vẫn không lộ IP |
| **Dùng IPv4 của máy này** | Thấy IPv4 thật của máy chạy proxy, mọi proxy dùng chung | **Có**, không nên dùng với anti-detect |

Lựa chọn này áp dụng cho proxy bật “Dự phòng IPv4” và cho danh sách **Trang được đi bằng IPv4** (ví dụ trang kiểm tra IP của anti-detect). Khi chọn NAT64, điền **Prefix NAT64 (/96)** của cổng, ví dụ `2001:db8:64:64::/96`.

Bản đang có proxy bật “Dự phòng IPv4” từ trước sẽ giữ “Dùng IPv4 của máy này” cho tới khi bạn đổi, để không âm thầm thay đổi hành vi. Danh sách proxy hiện nhãn **⚠ IPv4 thật** trên những proxy đang lộ IP.

Kiểm tra: chi tiết proxy → *Kiểm tra* → **Kết nối gần đây**. Kết nối thành công tới trang IPv4 sẽ ghi `NAT64`.

## Dựng cổng NAT64 trên VPS (Jool)

Cần: một VPS Linux (Ubuntu/Debian) có **IPv4 public** và **IPv6**, quyền root. Đây là phần nâng cao; nếu nhà cung cấp làm khác các bước dưới, đọc tài liệu IPv6 của họ.

1. Chọn một /96 **nằm trong dải IPv6 của VPS** mà máy không dùng, ví dụ VPS có `2001:db8:1234:5678::/64` thì lấy `2001:db8:1234:5678:64::/96`.

2. Cài Jool và bật chuyển tiếp gói:

   ```bash
   sudo apt install -y jool-dkms jool-tools
   sudo modprobe jool
   sudo sysctl -w net.ipv4.ip_forward=1 net.ipv6.conf.all.forwarding=1
   sudo jool instance add "openv6" --netfilter --pool6 2001:db8:1234:5678:64::/96
   ```

   Jool dùng IPv4 của VPS (dải port 61001–65535) để đi ra.

3. Đảm bảo gói gửi tới /96 đó về được VPS:
   - Nếu nhà cung cấp **route** cả /64 về VPS (Hetzner Cloud và một số nơi khác), không cần làm gì thêm.
   - Nếu /64 là **on-link** (VPS phải tự trả lời từng địa chỉ), cài `ndppd` để VPS trả lời Neighbor Solicitation cho /96, hoặc hỏi nhà cung cấp cách route thêm một dải.

4. Mở tường lửa của VPS cho lưu lượng chuyển tiếp (IPv6 vào /96, IPv4 ra Internet).

5. Thử từ máy chạy proxy. Ghép prefix của bạn với IPv4 của một trang chỉ có IPv4, viết dạng hex (ví dụ `93.184.216.34` → `5db8:d822`):

   ```bash
   curl -6 -m 10 "http://[2001:db8:1234:5678:64::5db8:d822]/"
   ```

   Có trả lời là cổng NAT64 hoạt động.

6. Điền `2001:db8:1234:5678:64::/96` vào **Prefix NAT64** trong openv6.

Để Jool tự chạy sau khi khởi động lại, thêm các lệnh ở bước 2 vào một dịch vụ systemd hoặc `/etc/rc.local` của VPS.

## Dịch vụ NAT64 công cộng

Một số dịch vụ miễn phí cung cấp NAT64 công khai (tìm “public NAT64”, ví dụ nat64.net). Điền prefix /96 họ công bố. Lưu ý: IPv4 dùng chung với mọi người dùng của dịch vụ, nên không hợp để nuôi tài khoản; chỉ nên dùng cho trang kiểm tra IP hoặc trang ít quan trọng.
