# Cài openv6 trên Windows 10/11

## 1. Kiểm tra máy có IPv6

Mở **PowerShell** và chạy:

```powershell
Get-NetIPAddress -AddressFamily IPv6 | Where-Object PrefixOrigin -eq RouterAdvertisement | Format-Table InterfaceAlias,IPAddress,PrefixLength
```

Cần thấy ít nhất một dòng có địa chỉ bắt đầu bằng `2` (ví dụ `2405:4802:…`) và `PrefixLength 64`, ở card **Ethernet** hoặc **Wi-Fi**. Nếu không có dòng nào, xem mục *Nếu chưa có IPv6* trong [HUONG-DAN.md](HUONG-DAN.md).

## 2. Cài đặt

1. Tải **`openv6-setup-windows-amd64.exe`** ở trang [Releases](https://github.com/synthcoz/openv6/releases/latest).
2. Mở file. Bộ cài chưa được ký số nên Windows SmartScreen có thể hiện *"Windows protected your PC"*: bấm **More info → Run anyway**.
3. Chấp nhận hộp thoại quyền Administrator (UAC), rồi bấm **Next → Install**.

Bộ cài sẽ:
- cài dịch vụ **openv6** chạy nền, tự khởi động khi bật máy (kể cả khi chưa đăng nhập) và tự chạy lại nếu gặp lỗi;
- thêm **icon openv6 ở khay hệ thống** (góc phải thanh Taskbar), tự chạy khi đăng nhập;
- mở dashboard `http://127.0.0.1:8080` để bạn tạo tài khoản admin.

> Nếu icon bị ẩn trong mũi tên `^` ở khay, kéo icon ra thanh Taskbar để luôn thấy trạng thái.

## 3. Icon khay hệ thống

| Màu | Ý nghĩa |
|---|---|
| 🟢 Xanh lá | Tất cả proxy hoạt động |
| 🟡 Vàng | Có proxy lỗi |
| 🔴 Đỏ | Dịch vụ không chạy, mất IPv6, hoặc có sự cố cần sửa |
| ⚪ Xám | Đang tạm dừng proxy |

Bấm vào icon để mở menu:
- **Mở bảng điều khiển**: tự đăng nhập, không phải gõ mật khẩu.
- **Tạm dừng / Tiếp tục proxy**.
- **Kiểm tra tất cả proxy**.
- **Áp dụng lại mạng**: dùng khi đổi từ Wi-Fi sang dây LAN hoặc ngược lại.
- **Tự chạy khi đăng nhập**.
- **Ngôn ngữ**.
- **Cập nhật** (chỉ hiện khi có bản mới).
- **Thoát biểu tượng**: proxy vẫn chạy nền.

## 4. Máy dùng cả dây LAN và Wi-Fi

- openv6 tự chọn card có IPv6 /64 và có đường ra Internet. Khi cắm cả hai, Windows thường ưu tiên **dây LAN**.
- Muốn cố định một card: vào **Máy chủ (node) → Cài đặt → Card mạng**, chọn `Ethernet` hoặc `Wi-Fi`.
- Sau khi đổi card hoặc rút dây, bấm **Áp dụng lại mạng** (trong dashboard hoặc icon khay).
- Mỗi card nhận một dải /64 riêng từ modem. Đổi card thì IPv6 của proxy đổi theo, còn port và mật khẩu giữ nguyên.

## 5. Cho các máy khác trong nhà dùng proxy

- Windows phải coi mạng nhà là **Private**: *Settings → Network & internet → Ethernet/Wi-Fi → Network profile type → Private network*. Nếu mạng đang là *Public*, Windows chặn các máy khác và phần *Tự chẩn đoán* sẽ báo.
- openv6 tự thêm luật cho phép trong **Windows Defender Firewall** (chỉ áp dụng cho mạng Private và Domain, không mở trên Wi-Fi công cộng).
- Từ máy khác, kết nối tới `IP-LAN-của-máy-Windows:port`. Xem IP LAN bằng lệnh `ipconfig` (dòng *IPv4 Address*).

## 6. VPN, Cloudflare WARP, Tailscale

openv6 gắn kết nối của từng proxy vào đúng card mạng thật, nên proxy vẫn đi ra bằng IPv6 của nhà mạng khi máy bật WARP hoặc VPN. Riêng VPN có **kill switch** (chặn mọi lưu lượng ngoài VPN) thì không phần mềm nào vượt qua được: tắt kill switch, hoặc không bật VPN trên máy chạy proxy.

## 7. Gỡ cài đặt

Vào *Settings → Apps → Installed apps → openv6 → Uninstall*. Dịch vụ, icon khay, luật tường lửa và các IPv6 đã thêm đều được gỡ. Dữ liệu (proxy, cài đặt, bản sao lưu) vẫn được giữ lại ở `C:\ProgramData\openv6`.

## 8. Dòng lệnh (nâng cao)

Mở PowerShell bằng *Run as Administrator*:

```powershell
& "C:\Program Files\openv6\openv6.exe" version
```

```powershell
Get-Service openv6
```

```powershell
Get-Content C:\ProgramData\openv6\openv6.log -Tail 50
```
