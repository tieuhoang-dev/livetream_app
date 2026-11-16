# Livestream API

Go API dùng MySQL, MongoDB, Redis và LiveKit Cloud.

## Chạy API cùng Redis

1. Cài Go 1.25 trở lên và mở Docker Desktop (Linux containers).
2. Chuẩn bị `.env` từ `.env.example`; điền kết nối MySQL/MongoDB và các cấu hình ứng dụng đang dùng. Hai database này cần chạy sẵn.
3. Tại thư mục repo, chạy:

```sh
go run ./cmd/dev
```

Windows có thể double-click **`start-dev.bat`**. Lệnh tự bật Redis còn thiếu bằng Docker, chờ sẵn sàng rồi chạy API. Lần đầu cần mạng để tải image. Nhấn **Ctrl+C** để dừng API và Redis container vừa được lệnh bật; dữ liệu Redis được giữ lại.

Video dùng LiveKit Cloud với `LIVEKIT_URL`, `LIVEKIT_API_KEY` và `LIVEKIT_API_SECRET` trong `.env`. Để `LIVEKIT_API_URL` trống để backend tự suy ra địa chỉ HTTPS từ URL Cloud. Launcher không chạy LiveKit local.

[Cấu hình cổng, dịch vụ đang chạy sẵn và xử lý lỗi](docs/development.md) · [API](API.md) · [Postman](postman/README.md)
