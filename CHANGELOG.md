# Changelog

## v0.7.0 — bản thương mại

- **openv6 Pro, bản quyền mua một lần** qua Polar. Nhập key ở *Cài đặt → Bản quyền*; có gói 1 máy và 3 máy.
  - **Bản Free:** 3 proxy trên 1 máy, tính năng cơ bản (tạo, xuất, kiểm tra proxy, tự sửa lỗi, chọn nguồn mạng cho cả máy).
  - **Bản Pro:** không giới hạn proxy và máy, relay qua VPS, UPnP, giới hạn tốc độ, API token, nhiều tài khoản, chế độ khách hàng, cảnh báo Telegram, chọn card mạng cho từng proxy.
  - Key được kiểm tra online mỗi ngày. Mất mạng thì Pro vẫn chạy thêm 30 ngày.
  - Chuyển key sang máy khác: bấm *Huỷ kích hoạt* trên máy cũ trước.
- **Nhiều tài khoản và chế độ khách hàng (bán lại proxy):** admin tạo tài khoản khách rồi giao proxy cho khách (*Thêm → Giao cho khách hàng*). Khách chỉ thấy proxy của mình, được sao chép, xuất, kiểm tra, đổi IPv6 và đổi mật khẩu proxy.
- **Chọn card mạng cho từng proxy:** ví dụ nhóm tài khoản A ra Wi-Fi, nhóm B ra mạng dây. Chọn khi tạo proxy hoặc trong mục *Sửa*.
- **Mã hoá dữ liệu nhạy cảm:** mật khẩu proxy, token Telegram và license được mã hoá trong file dữ liệu. File sao lưu vẫn khôi phục được trên máy khác.
- **Bản cập nhật có chữ ký số:** openv6 chỉ tự cập nhật lên bản có chữ ký hợp lệ.
- **Chuẩn bị ký số bộ cài Windows:** khi có chứng chỉ, bộ cài hết cảnh báo "Unknown publisher" (xem docs/RELEASE.md).
- **Rà soát cuối (bảo mật và ổn định):**
  - Bản cập nhật cho node cũng phải khớp chữ ký số. Chữ ký gắn với đúng phiên bản, nên không thể dùng chữ ký của bản cũ để hạ cấp.
  - License gắn với máy (mã máy lấy từ hệ điều hành): chép thư mục dữ liệu sang máy khác không mang theo Pro.
  - Chỉnh lùi đồng hồ không kéo dài được 30 ngày ân hạn.
  - Mạng chặn (captive portal, tường lửa) trả lỗi 403 không làm mất Pro.
  - Mất hoặc hỏng `master.key`: dịch vụ vẫn chạy, mật khẩu proxy bị khoá (phải đặt lại) chứ không thành "không mật khẩu".
  - Tài khoản khách cần bản Pro, và chỉ xem được kết quả kiểm tra của proxy được giao.
  - Bỏ giới hạn đăng nhập sai chung cho mọi người, vì kẻ xấu có thể dùng nó khoá cả admin.
  - Bản Free chạy 3 proxy cũ nhất đang bật, cố định khi đổi port. Khi vượt giới hạn có ghi một sự kiện báo rõ.
  - Proxy chọn card riêng không bị đẩy sang card khác khi license hết hạn.
  - Cắm lại Wi-Fi/USB làm card nhận số hiệu mới: openv6 tự áp dụng lại ngay.
  - Mã hoá dữ liệu cũ trong một lần ghi, nên lần khởi động đầu sau khi nâng cấp không bị chậm.

## v0.6.1

- **Thoát từ icon khay giờ tắt hẳn openv6:** chuột phải icon → **Thoát openv6** sẽ dừng toàn bộ proxy, dịch vụ nền và icon. Trước đây chỉ đóng icon, còn proxy vẫn chạy.
- **Mở lại:** bấm biểu tượng openv6 (Desktop hoặc menu Start) là openv6 tự chạy lại và mở dashboard, không hiện hộp hỏi quyền Administrator. Khởi động lại máy cũng tự chạy như bình thường.
- Trên Linux và macOS, dịch vụ cũng không tự bật lại sau khi thoát từ icon khay.
- **Sửa lỗi không khởi động được sau khi cập nhật lên v0.6.0** trên một số máy: `migrate: SQL logic error: table settings has no column named value`.
  - Bước nâng cấp dữ liệu giờ chạy từng lệnh một và bỏ qua phần đã làm rồi.
  - Nếu bảng cài đặt bị hỏng, openv6 tự dựng lại và giữ nguyên proxy, node và các thiết lập khác.
- Khi dịch vụ không khởi động được hoặc bị crash, lý do được ghi vào `openv6.log` / `openv6-crash.log` trong thư mục dữ liệu.
- **Ổn định hơn, hết báo "Mất kết nối node" chập chờn:**
  - mỗi 5 giây máy chạy proxy chỉ gửi tín hiệu "còn sống"; cấu hình mạng (gán IPv6, mở cổng) chỉ áp dụng lại khi có thay đổi, cộng thêm một lần mỗi phút để tự sửa;
  - trước đây Windows chạy lại netsh/PowerShell và chờ 1,2 giây ở mỗi vòng, nên máy chậm hoặc có nhiều proxy có thể trễ quá ngưỡng;
  - kiểm tra loại mạng trên Windows chạy nền; ngưỡng báo mất kết nối tăng lên 45 giây.
- **Chọn nguồn mạng (Mạng dây LAN / Wi-Fi):**
  - hiện ngay trên thẻ máy chủ, trong hộp Tạo proxy và trong trình hướng dẫn;
  - đổi nguồn mạng thì proxy tự chuyển sang IPv6 của card mạng đó.
- **Kiểm tra & sửa lỗi:** liệt kê từng vấn đề kèm nguyên nhân và nút **Sửa**, hoặc **Tự sửa tất cả**. Các lỗi tự sửa được:
  - tường lửa Windows chặn proxy, mạng đang ở chế độ Public;
  - VPN chiếm đường đi của proxy, card mạng không còn;
  - port bị chương trình khác chiếm: tự chuyển sang port trống;
  - lỗi gán IPv6: tự áp dụng lại cấu hình mạng.
  Việc ảnh hưởng tài khoản như đổi IPv6 vẫn để bạn tự bấm. Mở từ trang Tổng quan, thẻ máy chủ hoặc menu tài khoản.
- Dropdown không còn bị cắt chữ: nhãn ngắn gọn, mũi tên thống nhất. Bỏ hiệu ứng làm mờ nền để giao diện mượt hơn khi điều khiển từ xa (Parsec, RDP).
- **Bảo mật (rà soát trước khi bán):**
  - Thư mục dữ liệu (token node, mật khẩu proxy, khoá relay) giờ chỉ SYSTEM/Admin đọc được. Icon khay và log chuyển sang `C:\ProgramData\openv6\shared` (macOS: `openv6-shared`).
  - Không đổi được nguồn tải bản cập nhật qua dashboard hay file sao lưu. Trước đây đó là đường để chạy mã lạ với quyền SYSTEM/root.
  - Proxy không đi vào được 127.0.0.1, mạng LAN, link-local hay metadata của VPS. Nhờ vậy không ai dùng proxy này để vào proxy khác hoặc vào máy chủ.
  - Panel mới cài, nếu mở từ Internet (ví dụ trên VPS), phải nhập **mã cài đặt**. Mã do lệnh cài in ra và có trong file `setup-code.txt`. Mở từ máy này hoặc mạng LAN thì không cần mã.
  - Giới hạn đăng nhập sai tính theo cả dải /64 và có giới hạn chung. Thêm Content-Security-Policy. Giới hạn số kết nối chờ trên relay.
- **Ổn định (rà soát lần cuối):**
  - Rút cáp, đổi Wi-Fi hay máy ngủ dậy thì proxy tự áp dụng lại trong vòng khoảng 10 giây, không phải chờ 1 phút.
  - Mất card mạng tạm thời: không còn xoá hết IPv6 rồi vẫn báo "OK"; openv6 giữ nguyên và thử lại.
  - Bấm Sửa tường lửa / mạng Private không còn làm ngắt toàn bộ proxy.
  - "Chia sẻ qua relay" không còn cấp trùng port trên VPS.
  - Nếu file dữ liệu mất số phiên bản, proxy không còn bị đổi sang phạm vi Internet.
  - Kết quả "khởi động lại / cập nhật" luôn được gửi về trước khi tiến trình khởi động lại.

## v0.6.0 — dùng proxy từ ngoài mạng nhà, giới hạn, API

- **Relay qua VPS:** máy ở nhà (kể cả sau CGNAT của FPT) tự kết nối ra một VPS; proxy được dùng qua `IP-VPS:port` từ bất kỳ đâu, IP đi ra vẫn là IPv6 nhà. Kết nối mã hoá TLS và kiểm tra vân tay chứng chỉ; tự kết nối lại khi rớt mạng.
  - Bật ở **Máy chủ → node VPS → Chia sẻ ra Internet → Làm relay cho node khác**.
  - Gán proxy vào relay khi tạo, khi sửa, hoặc hàng loạt (**Thêm → Chia sẻ qua relay**). Port trên VPS được tự chọn để không trùng.
  - Proxy qua relay bắt buộc có mật khẩu.
- **UPnP:** tự mở port trên router cho proxy phạm vi "Internet", gia hạn định kỳ; báo rõ khi không có router UPnP hoặc khi nhà mạng dùng CGNAT.
- **Tailscale:** tự nhận diện IP Tailscale của node và đưa vào danh sách cách kết nối.
- **Xuất danh sách theo đường kết nối:** LAN, qua relay, IP WAN (UPnP) hoặc Tailscale. Bảng chi tiết proxy có mục "Dùng từ ngoài mạng nhà" để sao chép từng đường.
- **Giới hạn cho từng proxy:** số kết nối đồng thời và tốc độ tối đa (KB/s, mỗi chiều).
- **API token:** tạo trong Cài đặt, gọi API bằng `Authorization: Bearer ov6_…` (xem `docs/API.md`). Chỉ lưu bản băm của token.
- Bản sao lưu có thêm API token và các thiết lập mới.
- **Giao diện tinh gọn hơn:** tông trung tính (đen/trắng/xám), nút chính màu đen, viền mảnh, nhãn màu nhạt. Chọn **màu nhấn** Trung tính / Tím / Xanh lá trong Cài đặt → Ngôn ngữ & giao diện. Tắt hiệu ứng khi hệ điều hành bật giảm chuyển động.
- **Gọn hơn:** bỏ khung "Đăng nhập / phiên bản" ở thanh bên. Ngôn ngữ, sáng/tối, cài đặt và đăng xuất gom vào nút tài khoản ở góc phải.
- **Mượt hơn:**
  - chuyển trang không dựng lại toàn bộ khung;
  - dữ liệu tự làm mới chỉ cập nhật khi thật sự có thay đổi, nên không còn nháy và không mất vị trí cuộn;
  - giao diện web được nén gzip (app.js từ 99 KB còn 26 KB) và lưu cache, mở lại gần như tức thì.
- **Điện thoại:**
  - thanh điều hướng dưới có nhãn ngắn;
  - hộp thoại trượt lên từ cạnh dưới, nút bấm to;
  - bảng chi tiết proxy vừa màn hình;
  - ô nhập không làm iPhone tự phóng to;
  - chừa chỗ cho tai thỏ và thanh home.
- Các mẹo trên trang Tổng quan có nút **×** để ẩn hẳn.

## v0.5.0 — giao diện mới

- **Bố cục mới:**
  - thanh bên có icon và số đếm;
  - thanh trên cùng có **ô trạng thái hệ thống**, nút chuyển **VI/EN** và nút **Sáng / Theo hệ thống / Tối**;
  - trên điện thoại, thanh bên chuyển thành thanh điều hướng ở cuối màn hình.
- **Tổng quan:** một thẻ lớn tóm tắt tình trạng bằng một câu, kèm việc cần làm; 4 thẻ số liệu; danh sách dải IPv6; dòng thời gian sự kiện.
- **Trình hướng dẫn lần đầu** 4 bước: kiểm tra mạng → chọn ai được dùng (LAN / chỉ máy này / Internet) → tạo proxy → xuất đúng định dạng anti-detect.
- **Trang Proxy:**
  - bộ lọc trạng thái dạng chip có số đếm;
  - bấm một lần để sao chép theo định dạng anti-detect dùng gần nhất;
  - hiện cờ quốc gia, độ trễ và nhà mạng;
  - **bảng chi tiết** trượt ra từ bên phải, có 3 tab: Tổng quan (sao chép theo nhiều định dạng), Kiểm tra (biểu đồ độ trễ và lịch sử), Sửa;
  - **thanh thao tác hàng loạt** nổi ở cuối màn hình;
  - xoá có **Hoàn tác**;
  - trên điện thoại, mỗi proxy hiện thành một thẻ.
- **Trang Máy chủ:** 3 nút chính, các thao tác khác gom trong menu "⋯"; thông tin và tự chẩn đoán chia hai cột; cài đặt và dải IPv6 thu gọn được.
- Phím tắt: `/` tìm, `N` tạo proxy, `Esc` đóng, `G` rồi `D/P/N/S/E` để chuyển trang.
- Bộ icon thống nhất; khung tải trước khi có dữ liệu; hiệu ứng mở/đóng nhẹ; màn hình trống có hướng dẫn; hộp xác nhận cho thao tác nguy hiểm.

## v0.4.1

**Windows: chạy chắc chắn, không hiện cửa sổ terminal**
- `openv6.exe` giờ là một file chạy dạng giao diện duy nhất, không bao giờ mở cửa sổ console. Lệnh gõ trong terminal vẫn hiện kết quả.
- Mở `openv6.exe` khi máy chưa cài: openv6 hỏi có muốn cài không, xin quyền Administrator, cài dịch vụ, rồi hiện icon khay và mở dashboard. Đã cài rồi thì mở dashboard.
- **Port 8080 bị chương trình khác chiếm:** dịch vụ tự chuyển sang port trống trong khoảng 8081–8099 thay vì dừng hẳn. Icon khay và lối tắt dashboard tự dùng đúng port.
- Icon khay **nói rõ vì sao không kết nối được dịch vụ**: chưa cài, dịch vụ đang dừng, đang khởi động, không đọc được thông tin kết nối, hoặc dịch vụ không phản hồi (kèm thông báo lỗi). Từ đó có nút *Cài đặt* hoặc *Khởi động dịch vụ*.
- Thêm **Xem nhật ký** và **Tạo báo cáo lỗi**: tạo file `openv6-report.txt` trên Desktop, chứa trạng thái dịch vụ, port, kết quả gọi API và 300 dòng log cuối, để gửi khi cần hỗ trợ.
- File thông tin kết nối của icon khay được cấp quyền đọc cho mọi người dùng trên máy.
- Lối tắt dashboard đăng nhập tự động; thêm lối tắt ngoài Desktop (tuỳ chọn khi cài).
- CI Windows: cài bằng bản giao diện giống bộ cài; kiểm tra API icon khay; giữ port 8080 bận rồi kiểm tra dashboard tự chuyển sang 8081.

## v0.4.0

**Chạy trên Windows, macOS và Linux như một ứng dụng hoàn chỉnh**
- **Windows 10/11:** bộ cài `openv6-setup-windows-amd64.exe` (Next → Install).
  - Dịch vụ Windows chạy nền, tự khởi động cùng máy và tự chạy lại khi lỗi.
  - IPv6 của proxy được gán với `skipassource` nên không ảnh hưởng địa chỉ của chính máy.
  - Mỗi kết nối proxy được gắn vào card mạng thật (`IPV6_UNICAST_IF`), nên vẫn đi đúng IPv6 nhà mạng khi bật VPN hoặc WARP.
  - Tự thêm luật Windows Firewall cho mạng Private; cảnh báo khi mạng đang đặt là Public.
- **macOS:** gói `openv6-macos.pkg` (Intel và Apple Silicon). Chạy nền bằng launchd. IPv6 được gán bằng `ifconfig alias pltime 0`, kết nối gắn vào card mạng thật (`IPV6_BOUND_IF`), tự cho phép qua tường lửa ứng dụng của macOS.
- **Linux:** thêm gói `.deb`; lệnh cài một dòng vẫn dùng được như cũ.
- **Icon khay hệ thống** (Windows, macOS, Linux có hỗ trợ AppIndicator):
  - đổi màu theo trạng thái (xanh / vàng / đỏ / xám);
  - mở dashboard và tự đăng nhập bằng liên kết dùng một lần;
  - tạm dừng / tiếp tục proxy, kiểm tra tất cả, áp dụng lại mạng;
  - bật/tắt tự chạy khi đăng nhập, đổi ngôn ngữ, báo khi có bản mới, khởi động dịch vụ khi dịch vụ đang dừng.
- Icon ứng dụng được vẽ bằng code, không cần file ảnh.
- Tạm dừng toàn bộ proxy của một node (từ icon khay hoặc dashboard).
- Lệnh cài node trong dashboard có 3 tab Linux / Windows (PowerShell) / macOS.

**Tối ưu**
- Agent nội bộ của panel trao đổi trực tiếp với panel, không còn tự gọi HTTP mỗi 5 giây.
- Card mạng đi ra được xác định bằng một socket UDP, không cần chạy lệnh ngoài; cách này dùng được trên mọi hệ điều hành.
- Cập nhật một nút bấm hoạt động trên cả 3 hệ điều hành. Trên Windows, file `.exe` đang chạy được đổi tên trước khi thay bằng bản mới.

**Kiểm thử**
- CI chạy trên **Windows và macOS thật**: gán IPv6 thật vào card mạng (kiểm tra cả `SkipAsSource` trên Windows), bind socket vào địa chỉ đó, chấp nhận tuỳ chọn gắn card mạng, rồi dọn sạch.
- CI cài thử, chạy và gỡ dịch vụ (Windows Service, launchd).
- CI build chéo cho 6 nền tảng.

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
