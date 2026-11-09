# Cài openv6 trên Zorin OS: từng bước

Mọi lệnh dưới đây gõ trong **Terminal của máy Zorin** (mở bằng `Ctrl + Alt + T`). Gõ `sudo` thì hệ thống sẽ hỏi mật khẩu đăng nhập Zorin; khi gõ, màn hình không hiện ký tự, đó là bình thường.

## Bước 1: Kiểm tra IPv6 (bạn đã làm xong)

```bash
curl -6 https://api6.ipify.org
```

Lệnh in ra địa chỉ dạng `2405:4802:...` là đạt.

Kiểm tra thêm tên card mạng và prefix:

```bash
ip -6 addr show scope global
```

Ghi lại tên card mạng (ví dụ `enp3s0`, `eno1`, `wlp2s0`) và dòng `inet6 2405:....../64`.

> Nếu Zorin đang dùng **Wi-Fi**, mọi thứ vẫn chạy được, nhưng **cắm dây LAN sẽ ổn định hơn** khi chạy nhiều proxy.

## Bước 2: Lấy IP LAN của máy Zorin

```bash
hostname -I
```

Lấy địa chỉ dạng `192.168.1.xx`. Đây là địa chỉ để các máy khác trong nhà (Mac, Windows, điện thoại) kết nối tới.

> **Nên đặt IP LAN cố định** cho máy Zorin, nếu không, mỗi lần khởi động lại địa chỉ có thể đổi và anti-detect sẽ mất kết nối. Có 2 cách:
> - **Trên modem FPT** (khuyên dùng): vào `192.168.1.1`, mục *LAN → DHCP → Static/Reserved IP*, gán IP cố định cho địa chỉ MAC của máy Zorin.
> - **Trên Zorin:** *Settings → Network → (bánh răng) → IPv4 → Manual*. Chỉ dùng cách này khi bạn hiểu rõ dải mạng nhà mình.

## Bước 3: Cài openv6

### Cách A: một lệnh (khi repo GitHub đã có bản phát hành)

```bash
curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
```

Script tự tải đúng bản cho CPU của máy, cài vào `/usr/local/bin/openv6`, rồi tạo dịch vụ `openv6-server` tự chạy mỗi khi bật máy.

> Cách này chỉ chạy khi repo để chế độ **Public** và đã có Release (xem `docs/RELEASE.md`). Nếu repo Private, dùng cách B.

### Cách B: dùng file đã build sẵn

Chép file `openv6-linux-amd64` vào thư mục *Downloads* của Zorin (qua USB, AirDrop sang điện thoại rồi gửi, Google Drive…), sau đó chạy:

```bash
cd ~/Downloads
```

```bash
chmod +x openv6-linux-amd64
```

```bash
sudo ./openv6-linux-amd64 install server
```

### Cách C: tự build từ mã nguồn trên Zorin

```bash
sudo apt install -y git make
```

```bash
sudo snap install go --classic
```

```bash
git clone https://github.com/synthcoz/openv6.git && cd openv6
```

```bash
make build && sudo ./dist/openv6-linux-amd64 install server
```

> Không dùng `apt install golang` vì bản trong kho của Zorin quá cũ. Cần Go 1.24 trở lên.

### Kiểm tra dịch vụ đã chạy

```bash
systemctl status openv6-server --no-pager
```

Dòng `Active: active (running)` là đạt. Xem log trực tiếp (bấm `Ctrl + C` để thoát):

```bash
journalctl -u openv6-server -f
```

## Bước 4: Mở tường lửa (nếu có)

Kiểm tra trạng thái tường lửa:

```bash
sudo ufw status
```

Nếu kết quả là `Status: inactive` thì bỏ qua bước này. Nếu `active`, mở port cho mạng LAN:

```bash
sudo ufw allow from 192.168.0.0/16 to any port 8080 proto tcp
```

```bash
sudo ufw allow from 192.168.0.0/16 to any port 10000:20000 proto tcp
```

## Bước 5: Mở panel và tạo tài khoản

Trên Mac (hoặc ngay trên Zorin), mở trình duyệt và vào:

```
http://192.168.1.xx:8080
```

(`192.168.1.xx` là IP lấy ở bước 2. Ngay trên máy Zorin thì dùng `http://localhost:8080`.)

Lần đầu mở, panel yêu cầu tạo tài khoản admin (mật khẩu tối thiểu 8 ký tự).

## Bước 6: Kiểm tra node

1. Vào **Máy chủ (node)**. Node “local (tên-máy)” phải hiện **online**.
2. Mục **Card mạng IPv6** phải hiện tên card và prefix, ví dụ `enp3s0: 2405:4802:abcd:1234::/64`.
3. Pool `auto /64` phải hiện prefix đó (không còn “đang chờ phát hiện prefix”).
4. Bấm **Kiểm tra IPv6**. Giao diện phải báo “IPv6 hoạt động: 2405:…”.
5. Mục **Host kết nối**: nếu ô này không hiện đúng IP LAN `192.168.1.xx`, điền tay vào rồi bấm **Lưu**.

## Bước 7: Tạo proxy

1. Vào **Proxy → Tạo proxy**. Thử trước **3 proxy**, xác thực *User/pass ngẫu nhiên*, port bắt đầu `10001`.
2. Đợi khoảng 5–10 giây cho trạng thái chuyển sang **Hoạt động**.
3. Chọn cả 3 proxy, rồi chọn *Thao tác hàng loạt → Kiểm tra IP*. Mỗi proxy phải hiện ✓, và **3 IPv6 phải khác nhau**.

## Bước 8: Thử từ máy Mac

Mở Terminal trên Mac, thay `USER`, `PASS`, IP và port bằng thông tin của một proxy (bấm vào ô “Kết nối tới” để sao chép):

```bash
curl -x http://USER:PASS@192.168.1.xx:10001 https://api64.ipify.org
```

```bash
curl --socks5-hostname USER:PASS@192.168.1.xx:10001 https://api64.ipify.org
```

Cả hai lệnh phải in ra đúng IPv6 của proxy đó. Thử thêm port 10002: kết quả phải là một IPv6 khác.

## Bước 9: Dùng trong anti-detect

1. Trên panel, vào **Proxy → Xuất danh sách**, chọn định dạng `host:port:user:pass`, rồi bấm **Sao chép**.
2. Trong anti-detect, tạo profile và chọn loại proxy **HTTP** hoặc **SOCKS5** (cả hai đều được). Dán `192.168.1.xx:10001:user:pass` vào.
3. Bấm “Check proxy” trong anti-detect: kết quả phải là IP `2405:...`, quốc gia Việt Nam.
4. Ghi tên profile vào ô **Ghi chú** của proxy trên panel để dễ quản lý.

> Với SOCKS5, nếu anti-detect có tuỳ chọn **Remote DNS** / `socks5h`, hãy bật lên. Khi đó proxy tự phân giải tên miền và ưu tiên IPv6.

## Khi gặp lỗi

| Hiện tượng | Lệnh hoặc cách kiểm tra |
|---|---|
| Không vào được panel từ Mac | Trên Zorin, `curl localhost:8080/api/status` phải trả JSON. Nếu đúng mà Mac vẫn không vào được, xem lại tường lửa (bước 4) và IP LAN (bước 2). |
| Proxy “Lỗi” | Rê chuột vào dòng lỗi để xem nội dung, rồi xem `journalctl -u openv6-server -n 50`. |
| Kiểm tra IP báo timeout | Chạy `ip -6 addr show scope global` xem các địa chỉ proxy (`/128`) đã được thêm chưa, và `curl -6 https://api6.ipify.org` xem máy còn IPv6 không. |
| Sau khi khởi động lại modem, proxy đổi IP | Đây là hành vi bình thường với mạng FPT (prefix bị đổi). Port và mật khẩu vẫn giữ nguyên. |
| Quên mật khẩu admin | `sudo openv6 reset-password` |
| Cập nhật bản mới | Chạy lại lệnh cài ở bước 3. Dữ liệu ở `/var/lib/openv6` được giữ nguyên. |
| Gỡ cài đặt | `sudo openv6 uninstall` |

Khi gặp lỗi, gửi kèm kết quả 3 lệnh sau để được hỗ trợ nhanh:

```bash
systemctl status openv6-server --no-pager
```

```bash
journalctl -u openv6-server -n 80 --no-pager
```

```bash
ip -6 addr show scope global
```
