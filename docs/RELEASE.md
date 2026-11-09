# Phát hành phiên bản mới

GitHub Actions (`.github/workflows/release.yml`) tự build file chạy cho Linux amd64, arm64 và arm, rồi tạo Release mỗi khi bạn đẩy một tag dạng `v*`.

1. Sửa số phiên bản trong `internal/proto/proto.go` (`const Version = "0.1.1"`) rồi commit.
2. Gắn tag và đẩy tag lên:

```bash
git tag v0.1.1
```

```bash
git push origin v0.1.1
```

3. Khoảng 2 phút sau, tab *Releases* trên GitHub sẽ có các file `openv6-linux-*`. Lệnh cài một dòng (`install.sh`) luôn lấy bản mới nhất.

Muốn người khác cài bằng một lệnh thì repo phải để chế độ **Public**. Nếu để Private, hãy tải file trong Releases về rồi chép sang máy cài.
