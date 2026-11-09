# openv6 API

Mọi thao tác trên dashboard đều gọi API JSON này. Script và phần mềm khác dùng **API token**.

## Xác thực

1. Dashboard → **Cài đặt → API token → Tạo token**. Token có dạng `ov6_…` và chỉ hiện một lần.
2. Gửi kèm mỗi request:

```
Authorization: Bearer ov6_xxxxxxxx
Content-Type: application/json
```

Token có toàn quyền như tài khoản admin, trừ việc tạo/xoá token. Nhập sai 10 lần trong 5 phút sẽ bị chặn tạm thời. Nếu lộ token, xoá nó trong Cài đặt.

## Các lệnh hay dùng

| Việc | Request |
|---|---|
| Danh sách node | `GET /api/nodes` |
| Danh sách pool của node | `GET /api/pools?node_id=1` |
| Danh sách proxy | `GET /api/proxies` (lọc: `?node_id=1`) |
| Tạo nhiều proxy | `POST /api/proxies/bulk` |
| Sửa một proxy | `PUT /api/proxies/{id}` |
| Thao tác hàng loạt | `POST /api/proxies/action` |
| Xuất danh sách | `POST /api/proxies/export` |
| Kết quả lệnh (kiểm tra proxy) | `GET /api/commands/{id}` |
| Tổng quan | `GET /api/dashboard` |

### Tạo proxy

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"node_id":1,"pool_id":1,"count":10,"auth":"random","scope":"lan","label_prefix":"FB","tag":"facebook"}' \
  http://192.168.1.10:8080/api/proxies/bulk
```

Các trường của `bulk`:

| Trường | Ý nghĩa |
|---|---|
| `node_id`, `pool_id` | Node và dải IPv6 (bắt buộc) |
| `count` | Số proxy (1–5000) |
| `start_port` | Port bắt đầu (mặc định 10001), tự bỏ qua port đã dùng |
| `protocol` | `both` (mặc định), `http`, `socks5` |
| `auth` | `random`, `none`, `fixed` (kèm `user`, `pass`) |
| `scope` | `lan` (mặc định), `local`, `any` (bắt buộc có mật khẩu) |
| `label_prefix`, `tag`, `note` | Tên hiển thị ("FB 01", "FB 02"…), nhãn, ghi chú |
| `expires_days` | Số ngày hết hạn, 0 = không hết hạn |
| `spread` | Mỗi proxy một /64 riêng (pool lớn hơn /64) |
| `max_conns`, `rate_kbps` | Giới hạn kết nối đồng thời và tốc độ (KB/s mỗi chiều), 0 = không giới hạn |
| `relay_node_id` | Chia sẻ qua relay của node này (0 = không) |

### Thao tác hàng loạt

```json
{"ids":[1,2,3], "action":"regen_ip"}
```

`action`: `enable`, `disable`, `delete`, `test`, `regen_ip`, `regen_pass`, `remove_auth`, `reset_stats`, `fallback_on`, `fallback_off`,
`set_scope` (`value`: `lan|local|any`), `set_tag`, `set_protocol` (`both|http|socks5`),
`set_limits` (`value`: `"max_conns,rate_kbps"`, ví dụ `"20,2048"`), `set_relay` (`value`: id node relay, `"0"` để tắt).

`test` trả về danh sách lệnh; hỏi kết quả bằng `GET /api/commands/{id}` cho tới khi `done` là `true`.

### Xuất danh sách

```json
{"ids":[1,2,3], "format":"host:port:user:pass", "scheme":"socks5", "via":"relay"}
```

- `format`: `host:port:user:pass`, `user:pass@host:port`, `url`, `scheme://host:port:user:pass`, `host:port@user:pass`, `csv`, `json`.
- `via`: bỏ trống (LAN / địa chỉ chính), `relay`, `wan`, `tailscale`. Proxy không có đường đó bị bỏ qua (`skipped` trong kết quả).
- `host`: ghi đè địa chỉ.

Kết quả: `{"text": "...", "skipped": 0}`.

### Sửa một proxy

`PUT /api/proxies/{id}` với các trường cần đổi: `label`, `port`, `protocol`, `auth_user`, `auth_pass`, `scope`, `tag`, `note`,
`enabled`, `ipv4_fallback`, `expires_at` (unix), `ip` (địa chỉ cụ thể trong pool), `max_conns`, `rate_kbps`, `relay_node_id`, `relay_port`.

Lỗi trả về mã HTTP 4xx kèm `{"error": "mô tả"}`.
