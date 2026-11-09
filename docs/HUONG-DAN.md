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

## 5. Cảnh báo Telegram và tự kiểm tra

1. Trên Telegram, nhắn cho **@BotFather** lệnh `/newbot`, đặt tên rồi nhận **bot token**.
2. Nhắn cho bot vừa tạo một tin bất kỳ. Sau đó nhắn **@userinfobot** để lấy **Chat ID** của bạn. Muốn nhận cảnh báo trong nhóm thì thêm bot vào nhóm và dùng ID của nhóm (bắt đầu bằng `-`).
3. Trong panel, vào **Cài đặt → Cảnh báo Telegram**, dán token và Chat ID, bấm **Lưu**, rồi bấm **Gửi tin thử**.
4. Mục **Vận hành → Tự kiểm tra proxy** đặt chu kỳ kiểm tra (mặc định 10 phút). Proxy lỗi 2 lần liên tiếp sẽ được báo, và báo lại khi nó hoạt động trở lại.

## 6. Cập nhật, sao lưu, xem log

- **Cập nhật:** vào **Cài đặt → Cập nhật**. Khi có bản mới, bấm **Cập nhật lên vX**: panel tự tải bản mới, khởi động lại (proxy trên máy đó ngắt vài giây), sau đó bấm **Cập nhật các node** để nâng cấp các máy khác.
- **Sao lưu:** vào **Cài đặt → Sao lưu & khôi phục → Tải file sao lưu**. Panel còn tự sao lưu mỗi ngày vào `/var/lib/openv6/backups`. Chuyển sang máy mới: cài openv6 trên máy mới, rồi **Khôi phục từ file**.
- **Log:** vào **Máy chủ (node) → Log** để xem nhật ký của panel hoặc của từng node, không cần gõ lệnh.

## 7. Máy có bật VPN, Cloudflare WARP hoặc Tailscale

### Trên máy chạy proxy (node)

| Trường hợp | Ảnh hưởng | openv6 xử lý |
|---|---|---|
| Ứng dụng 1.1.1.1 **chỉ đổi DNS** | Không | — |
| **Cloudflare WARP** (chế độ VPN) | WARP giành đường ra mặc định, nên proxy không ra được Internet | Tự định tuyến proxy vòng qua VPN. Tự chẩn đoán hiện “VPN đang bật, proxy đi vòng” |
| **Tailscale** bình thường | Không | Thiết bị trong mạng Tailscale (100.x) được coi là LAN |
| **Tailscale exit node** | Giống WARP | Tự định tuyến vòng qua VPN |
| **WireGuard, OpenVPN, VPN thương mại** | Giống WARP; một số VPN **tắt hẳn IPv6** hoặc có **kill switch** | Tự định tuyến vòng qua VPN. Tắt hẳn IPv6 hoặc kill switch thì **không phần mềm nào vượt qua được**: chẩn đoán báo rõ cách sửa (tắt “IPv6 leak protection” hoặc kill switch, hoặc không chạy VPN trên máy này) |

- Tuỳ chọn nằm ở **Máy chủ (node) → Cài đặt → Định tuyến proxy vòng qua VPN** (mặc định bật, hiện chỉ có trên Linux).
- Bật VPN mà proxy báo **“Website không hỗ trợ IPv6”**: đổi **DNS cho proxy** sang Cloudflare hoặc Google, vì một số VPN lọc bản ghi IPv6.
- Kiểm tra nhanh trên máy node:

```bash
sudo openv6 selftest
```

Lệnh in card mạng, prefix, router, các VPN đang bật, rồi thử định tuyến vòng qua VPN trong một vùng mạng giả lập. Mạng thật không bị ảnh hưởng. Kết quả cuối phải là **ĐẠT / PASSED**.

### Trên máy dùng proxy (máy chạy anti-detect)

Máy dùng proxy kết nối tới IP LAN của node (ví dụ `192.168.1.10`). Nếu máy đó bật VPN:
- **Cloudflare WARP:** mặc định không đưa các dải LAN vào đường hầm, nên vẫn dùng được.
- **Tailscale exit node:** bật **“Allow local network access”**.
- **VPN thương mại:** bật **“Allow LAN” / “LAN discovery” / “Local network sharing”**.

## 8. Phạm vi chia sẻ của proxy

| Lựa chọn | Ai dùng được | Ghi chú |
|---|---|---|
| **Chỉ máy này** | Phần mềm trên chính máy node, qua `127.0.0.1:port` | Máy khác không thấy proxy |
| **Mạng LAN** (mặc định) | Thiết bị trong whitelist của node (mặc định các dải LAN và Tailscale) | Có hoặc không mật khẩu đều được |
| **Internet** | Bất kỳ ai có user/pass | Bắt buộc mật khẩu. Dùng cho VPS hoặc máy nhà đã mở port |

Đổi phạm vi cho nhiều proxy cùng lúc: chọn proxy rồi vào **Thao tác hàng loạt → Đổi phạm vi chia sẻ**.

### Dùng proxy từ ngoài mạng nhà (4G, quán cà phê, máy ở nơi khác)

Mạng FPT thường dùng CGNAT nên không mở port từ Internet về nhà được. openv6 có 3 cách, xem trong **Máy chủ → node → Chia sẻ ra Internet**:

| Cách | Khi nào dùng | Cần gì |
|---|---|---|
| **Relay qua VPS** (khuyên dùng) | Dùng được mọi lúc, kể cả sau CGNAT | Một VPS Linux rẻ có IPv4 public (VPS không cần IPv6) |
| **UPnP** | Modem có IPv4 public và bật UPnP | Không cần thêm gì; openv6 tự mở port. Nếu thấy "nhà mạng dùng CGNAT" thì cách này không dùng được |
| **Tailscale** | Chỉ máy của bạn dùng | Cài Tailscale trên máy chạy proxy và máy dùng proxy, cùng tài khoản |

**Relay qua VPS, từng bước:**
1. Trên dashboard: **Máy chủ → Thêm node**, đặt tên ví dụ "VPS", chạy lệnh cài hiện ra trên VPS.
2. Ở node VPS: **Chia sẻ ra Internet → bật "Làm relay cho node khác"** (port mặc định 7443) → Lưu.
   Nếu nhà cung cấp VPS có tường lửa riêng (Security Group…), mở port 7443 và các port proxy (ví dụ 10001–10100).
3. Trang **Proxy**: chọn proxy → **Thêm → Chia sẻ qua relay** → chọn VPS. Proxy phải có user/mật khẩu.
4. Huy hiệu 🌐 `IP-VPS:port` xuất hiện bên cạnh proxy. Khi **Xuất danh sách**, chọn **Kết nối qua: Qua relay (VPS)**.

Máy ở nhà tự kết nối ra VPS (mã hoá TLS, kiểm tra vân tay chứng chỉ), nên không cần mở port trên modem. Lưu lượng đi qua VPS, còn IP mà website nhìn thấy vẫn là IPv6 nhà bạn.

### Giới hạn cho từng proxy

Trong **Sửa** proxy hoặc **Thêm → Đặt giới hạn** cho nhiều proxy:
- **Kết nối tối đa:** số kết nối đồng thời; vượt quá thì kết nối mới bị từ chối.
- **Tốc độ tối đa (KB/s):** áp dụng riêng cho chiều tải lên và tải xuống. 1024 KB/s ≈ 8 Mbps.

Để 0 là không giới hạn.

### API cho phần mềm khác

**Cài đặt → API token → Tạo token.** Xem cách gọi trong [API.md](API.md).

## 9. Câu hỏi thường gặp và xử lý lỗi

| Hiện tượng | Nguyên nhân và cách xử lý |
|---|---|
| Node báo “không có route IPv6 mặc định” | Máy chưa có IPv6. Xem lại mục 1. |
| Pool `auto` hiện “đang chờ phát hiện prefix” | Card mạng chưa nhận địa chỉ /64. Nếu máy có nhiều card, chọn đúng card trong *Cài đặt node → Card mạng*. |
| Proxy “Lỗi: listen: address already in use” | Port đã bị chương trình khác dùng. Sửa proxy sang port khác. |
| Kiểm tra báo lỗi timeout | Modem chặn IPv6 đi ra hoặc chưa nhận địa chỉ. Thử `curl -6 https://api6.ipify.org` trên máy node. |
| Website mở được ở trình duyệt thường nhưng qua proxy báo 502 | Website đó **không có IPv6**. Có thể bật *Dự phòng IPv4* cho riêng proxy đó (đọc kỹ mục 6). |
| Không vào được panel | Kiểm tra `systemctl status openv6-server`, tường lửa ufw, và đúng IP LAN. |
| Bật VPN thì proxy lỗi | Xem mục 7 và chạy `sudo openv6 selftest` |
| Đổi mạng hoặc card mạng xong proxy lỗi | **Máy chủ (node) → Áp dụng lại mạng** |
| Quên mật khẩu admin | `sudo openv6 reset-password` |
| Gỡ cài đặt | `sudo openv6 uninstall`: dừng dịch vụ và xoá mọi IPv6 đã thêm vào card mạng. |

---

## 10. Lưu ý khi nuôi nhiều tài khoản

1. **Cùng /64 = dễ bị coi là một người.** Mạng nhà FPT thường chỉ cho một /64 trên LAN, nên mọi proxy chung một /64. Facebook, Google… có thể liên kết các tài khoản với nhau. Dashboard sẽ cảnh báo khi điều này xảy ra. Cách khắc phục là dùng VPS có /48 hoặc /56, hoặc router cấp prefix riêng (xem `docs/ROADMAP.md`, mục DHCPv6-PD).
2. **Dự phòng IPv4 = mọi tài khoản chung một IPv4.** Chỉ bật cho proxy dùng việc khác, không bật cho proxy nuôi tài khoản.
3. **Không phải website nào cũng hỗ trợ IPv6.** Facebook, Google, YouTube, TikTok, Instagram, Cloudflare… đều có IPv6. Một số trang nhỏ thì không.
4. Mỗi profile anti-detect nên gắn với **một proxy cố định**, đừng đổi qua lại.
5. Tuân thủ điều khoản sử dụng của nền tảng và của nhà mạng hoặc nhà cung cấp VPS.

## 11. Bản quyền, tài khoản khách và card mạng riêng cho từng proxy

**Bản quyền (openv6 Pro)**
- Bản Free chạy 3 proxy trên 1 máy. Muốn dùng nhiều hơn: mua license key, rồi vào *Cài đặt → Bản quyền*, dán key và bấm *Kích hoạt*.
- Mỗi key dùng được trên 1 hoặc 3 máy, tuỳ gói đã mua. Muốn chuyển sang máy khác thì bấm *Huỷ kích hoạt* trên máy cũ trước.
- Máy cần Internet khi kích hoạt. Sau đó mỗi ngày openv6 kiểm tra key một lần; nếu mất mạng, Pro vẫn chạy thêm 30 ngày.

**Bán lại proxy cho khách**
1. Vào *Cài đặt → Tài khoản*, thêm tài khoản với vai trò *Khách hàng*.
2. Ở trang *Proxy*, chọn các proxy → *Thêm → Giao cho khách hàng*.
3. Khách đăng nhập cùng địa chỉ dashboard. Họ chỉ thấy proxy của mình, được sao chép, xuất danh sách, kiểm tra, đổi IPv6 và đổi mật khẩu proxy.

**Card mạng riêng cho từng proxy**
- Máy có cả mạng dây và Wi-Fi thì khi tạo proxy, phần *Nguồn mạng* cho chọn proxy đi ra qua card nào. Có thể đổi sau trong *Sửa*.
- Card được chọn phải có cùng dải IPv6 với pool. Nếu card đó ngắt kết nối, proxy báo lỗi và *Kiểm tra & sửa lỗi* có nút đưa proxy về card mặc định.
