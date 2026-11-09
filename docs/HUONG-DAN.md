# Hướng dẫn cài đặt và sử dụng openv6

## 0. Khái niệm

| Thuật ngữ | Ý nghĩa |
|---|---|
| **Panel** | Web quản lý (port 8080). Chỉ cần **một** panel cho tất cả các máy. |
| **Node** | Máy Linux chạy proxy: mini PC trong nhà, VPS, máy homelab… Máy cài panel tự động là một node (“máy này”). |
| **Pool** | Dải IPv6 mà node dùng để cấp IP cho proxy. Pool `auto` tự theo dải /64 của card mạng. |
| **Host kết nối** | Địa chỉ IPv4 thiết bị dùng để kết nối tới proxy. Trong nhà là IP LAN của máy chạy proxy, ví dụ `192.168.1.10`. |

```
Máy Windows / điện thoại / anti-detect
        │  kết nối IPv4: 192.168.1.10:10001 (user/pass)
        ▼
Máy Zorin chạy openv6 ──── đi ra bằng IPv6 2405:4802:xxxx:xxxx::abcd ───► Facebook, Google…
```

**Yêu cầu:** máy chạy proxy phải là **Linux** (Zorin, Ubuntu, Debian, Proxmox LXC…). Windows chỉ đóng vai máy **dùng** proxy. WSL trên Windows không phù hợp vì nằm sau NAT và không có IPv6 thật.

---

## 1. Kiểm tra mạng nhà có IPv6 chưa (FPT)

Trên **máy Zorin**, mở Terminal:

```bash
ip -6 addr show scope global
```

Nếu báo `command not found: ip` thì cài thêm gói `iproute2` (Zorin thường có sẵn):

```bash
sudo apt install iproute2
```

Kết quả tốt sẽ có dòng dạng `inet6 2405:4802:xxxx:xxxx:.../64 scope global dynamic mngtmpaddr`. Địa chỉ bắt đầu bằng `2` hoặc `3` là IPv6 thật; nếu chỉ thấy `fe80::` thì chưa có IPv6.

Kiểm tra thêm:

```bash
curl -6 https://api6.ipify.org
```

Trên **Windows**, chạy `ipconfig` trong PowerShell và tìm dòng `IPv6 Address . . . : 2405:...`, hoặc mở trang https://test-ipv6.com.

### Nếu chưa có IPv6 (modem FPT)

1. Đăng nhập modem (thường là `192.168.1.1`; tài khoản ghi ở mặt sau modem hoặc lấy qua app Hi FPT).
2. Tìm mục **IPv6** (thường nằm trong *Network → WAN* hoặc *LAN → IPv6*): bật IPv6 và chọn chế độ LAN **SLAAC / Stateless** (hoặc “Auto”).
3. Nếu modem không có tuỳ chọn IPv6, gọi tổng đài FPT (19006600) nhờ bật IPv6 cho hợp đồng.
4. Khởi động lại máy Zorin, sau đó kiểm tra lại.

> **Prefix của FPT có thể đổi** khi khởi động lại modem. openv6 tự phát hiện prefix mới, giữ nguyên port, user/pass và phần đuôi IP. Anti-detect không cần sửa gì, nhưng IPv6 mà website nhìn thấy sẽ đổi phần đầu. Đây là giới hạn của mạng gia đình; muốn IP cố định tuyệt đối thì dùng VPS.

---

## 2. Cài panel trên máy Zorin

```bash
# chép file openv6-linux-amd64 vào máy, rồi:
chmod +x openv6-linux-amd64
sudo ./openv6-linux-amd64 install server
```

Lệnh này cài vào `/usr/local/bin/openv6`, tạo dịch vụ `openv6-server` tự chạy khi khởi động máy, và lưu dữ liệu ở `/var/lib/openv6`.

Mở trình duyệt (trên bất kỳ máy nào trong nhà): `http://<IP-LAN-máy-Zorin>:8080`. Bạn có thể xem IP LAN bằng lệnh `hostname -I`. Lần đầu mở, giao diện yêu cầu tạo tài khoản admin.

Xem log khi cần:

```bash
journalctl -u openv6-server -f
```

### Tường lửa (nếu Zorin bật ufw)

```bash
sudo ufw allow from 192.168.0.0/16 to any port 8080 proto tcp
```

```bash
sudo ufw allow from 192.168.0.0/16 to any port 10000:20000 proto tcp
```

---

## 3. Tạo proxy và dùng trong anti-detect

1. Vào **Máy chủ (node)**, bấm **Kiểm tra IPv6**. Kết quả phải hiện một IP `2405:...`. Pool `auto /64` sẽ hiện prefix, ví dụ `2405:4802:abcd:1234::/64`.
2. Vào **Proxy**, bấm **Tạo proxy** và chọn:
   - Số lượng, ví dụ 30. Port bắt đầu mặc định là 10001.
   - **Xác thực:** *User/pass ngẫu nhiên* là lựa chọn khuyên dùng. Chọn *Không mật khẩu* thì chỉ thiết bị trong LAN dùng được.
   - **Tag**, ví dụ `facebook`, để lọc và xuất theo nhóm.
   - **Dự phòng IPv4:** để **tắt** khi nuôi tài khoản (xem mục 6).
3. Bấm **Kiểm tra** ở từng dòng (hoặc chọn tất cả rồi *Thao tác hàng loạt → Kiểm tra IP*). Kết quả ✓ nghĩa là website đã thấy đúng IPv6 của proxy đó.
4. Bấm **Xuất danh sách**, chọn định dạng mà anti-detect của bạn nhận. Phổ biến nhất là `host:port:user:pass`. Sau đó sao chép hoặc tải file.
5. Ghi chú tài khoản vào từng proxy (nút **Sửa → Ghi chú**), ví dụ “FB Lan – acc 12”, để biết proxy nào gắn với profile nào.

### Thử từ máy Windows

```powershell
curl.exe -x http://USER:PASS@192.168.1.10:10001 https://api64.ipify.org
```

```powershell
curl.exe --socks5-hostname USER:PASS@192.168.1.10:10001 https://api64.ipify.org
```

Cả hai lệnh phải in ra IPv6 của proxy. Mỗi port nhận **cả HTTP lẫn SOCKS5**, tự nhận diện giao thức.

---

## 4. Thêm VPS hoặc mini PC khác (node)

1. Vào **Máy chủ (node) → Thêm node**, đặt tên, giao diện sẽ hiện **lệnh cài**.
2. Chạy lệnh đó trên máy mới với quyền root:
   ```bash
   curl -fsSL http://PANEL:8080/install.sh | sudo bash -s -- agent --panel http://PANEL:8080 --token ....
   ```
3. Khoảng 10 giây sau node hiện “online”.

**Điều kiện:** máy mới phải **truy cập được panel**, vì node chủ động gọi về panel nên node nằm sau NAT vẫn chạy được. Vậy panel đặt ở đâu?

| Tình huống | Gợi ý |
|---|---|
| Chỉ có máy trong nhà | Panel ở máy Zorin, mọi thứ chạy trong LAN. |
| Có VPS + máy nhà | **Đặt panel trên VPS** (`install server` trên VPS), còn máy nhà cài `agent` trỏ về VPS. Máy nhà gọi ra được, nên không cần mở port trên modem. |
| Muốn giữ panel ở nhà | Dùng Tailscale hoặc ZeroTier (mạng riêng ảo) rồi điền địa chỉ Tailscale vào *Cài đặt → Địa chỉ panel*. Cách khác là Cloudflare Tunnel. |

Khi panel mở ra Internet, **bắt buộc dùng HTTPS**, vì token node và mật khẩu đi trên đường truyền:

```bash
openv6 server -tls-cert /etc/ssl/panel.crt -tls-key /etc/ssl/panel.key
```

Bạn cũng có thể đặt panel sau Caddy hoặc Nginx (reverse proxy có HTTPS tự động).

Nếu máy node dùng chip ARM (Raspberry Pi, một số mini PC), chép `dist/openv6-linux-arm64` (hoặc `-arm`) vào thư mục `/var/lib/openv6/bin/` trên máy panel, để lệnh cài tải đúng bản.

### VPS có dải lớn hơn /64

Nhiều nhà cung cấp cấp dải **/48 hoặc /56 được route** về VPS (có khi phải bật trong trang quản trị). Khi đó vào node đó, **thêm pool**:

- CIDR: ví dụ `2a01:4f8:c0c:1234::/56`
- Chế độ: `anyip`

Lúc tạo proxy, chọn pool này và tick **“Mỗi proxy nằm trong một dải /64 riêng”**: mỗi tài khoản sẽ có một /64 khác nhau, tách biệt tốt hơn nhiều.

> Chế độ `anyip` chỉ dùng cho dải **được route** về máy. Nếu nhà cung cấp chỉ cấp /64 kiểu on-link (thường gặp), cứ dùng pool `auto`, không cần thêm gì.

Mở port trên tường lửa của VPS (cả tường lửa trên trang quản trị của nhà cung cấp) cho dải port proxy, ví dụ `10001-11000/tcp`.

---

## 5. Câu hỏi thường gặp và xử lý lỗi

| Hiện tượng | Nguyên nhân và cách xử lý |
|---|---|
| Node báo “không có route IPv6 mặc định” | Máy chưa có IPv6. Xem lại mục 1. |
| Pool `auto` hiện “đang chờ phát hiện prefix” | Card mạng chưa nhận địa chỉ /64. Nếu máy có nhiều card, chọn đúng card trong *Cài đặt node → Card mạng*. |
| Proxy “Lỗi: listen: address already in use” | Port đã bị chương trình khác dùng. Sửa proxy sang port khác. |
| Kiểm tra báo lỗi timeout | Modem chặn IPv6 đi ra hoặc chưa nhận địa chỉ. Thử `curl -6 https://api6.ipify.org` trên máy node. |
| Website mở được ở trình duyệt thường nhưng qua proxy báo 502 | Website đó **không có IPv6**. Có thể bật *Dự phòng IPv4* cho riêng proxy đó (đọc kỹ mục 6). |
| Không vào được panel | Kiểm tra `systemctl status openv6-server`, tường lửa ufw, và đúng IP LAN. |
| Quên mật khẩu admin | `sudo openv6 reset-password` |
| Gỡ cài đặt | `sudo openv6 uninstall`: dừng dịch vụ và xoá mọi IPv6 đã thêm vào card mạng. |

---

## 6. Lưu ý khi nuôi nhiều tài khoản

1. **Cùng /64 = dễ bị coi là một người.** Mạng nhà FPT thường chỉ cho một /64 trên LAN, nên mọi proxy chung một /64. Facebook, Google… có thể liên kết các tài khoản với nhau. Dashboard sẽ cảnh báo khi điều này xảy ra. Cách khắc phục là dùng VPS có /48 hoặc /56, hoặc router cấp prefix riêng (xem `docs/ROADMAP.md`, mục DHCPv6-PD).
2. **Dự phòng IPv4 = mọi tài khoản chung một IPv4.** Chỉ bật cho proxy dùng việc khác, không bật cho proxy nuôi tài khoản.
3. **Không phải website nào cũng hỗ trợ IPv6.** Facebook, Google, YouTube, TikTok, Instagram, Cloudflare… đều có IPv6. Một số trang nhỏ thì không.
4. Mỗi profile anti-detect nên gắn với **một proxy cố định**, đừng đổi qua lại.
5. Tuân thủ điều khoản sử dụng của nền tảng và của nhà mạng hoặc nhà cung cấp VPS.
