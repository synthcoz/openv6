# Phát hành phiên bản mới

GitHub Actions (`.github/workflows/release.yml`) tự build file chạy cho Linux amd64, arm64 và arm, rồi tạo Release mỗi khi bạn đẩy một tag dạng `v*`.

1. Sửa số phiên bản trong `internal/proto/proto.go` (`const Version = "0.1.1"`), rồi commit và đưa vào nhánh `main`. Workflow tự thấy phiên bản mới và tạo Release `v0.1.1`.
2. (Tuỳ chọn) Hoặc gắn tag và đẩy tag lên:

```bash
git tag v0.1.1
```

```bash
git push origin v0.1.1
```

3. Khoảng 2 phút sau, tab *Releases* trên GitHub sẽ có các file `openv6-linux-*`. Lệnh cài một dòng (`install.sh`) luôn lấy bản mới nhất.

Muốn người khác cài bằng một lệnh thì repo phải để chế độ **Public**. Nếu để Private, hãy tải file trong Releases về rồi chép sang máy cài.

## Ký số (bắt buộc cho bản bán)

Thêm các secret dưới đây ở GitHub → repo → *Settings → Secrets and variables → Actions → New repository secret*.

| Secret | Dùng để | Bắt buộc? |
|---|---|---|
| `OPENV6_SIGNING_KEY` | Ký file `SHA256SUMS` của mỗi bản phát hành (ed25519). Khoá công khai tương ứng nằm trong `internal/update/verify.go`. Bản phát hành chưa ký thì openv6 **từ chối tự cập nhật**. | Có |
| `WINDOWS_CERT_PFX` | Chứng chỉ ký mã Windows (file `.pfx`, mã hoá base64: `[Convert]::ToBase64String([IO.File]::ReadAllBytes("cert.pfx"))`). Khi có chứng chỉ này, Windows không còn hiện cảnh báo SmartScreen "Unknown publisher". | Nên có |
| `WINDOWS_CERT_PASSWORD` | Mật khẩu của file `.pfx`. | Đi kèm |

- Giữ một bản sao `OPENV6_SIGNING_KEY` ở nơi an toàn (trình quản lý mật khẩu). Nếu mất khoá này, phải tạo khoá mới, sửa `PublicKey` trong code, và người dùng cài tay một bản mới thì mới tự cập nhật lại được.
- Chứng chỉ ký mã: mua chứng chỉ OV/EV (Sectigo, DigiCert, SSL.com…) hoặc dùng Azure Trusted Signing. Chứng chỉ EV hết cảnh báo ngay; chứng chỉ OV hết cảnh báo sau khi tích luỹ đủ lượt tải.
