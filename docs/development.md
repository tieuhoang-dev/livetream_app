# Chạy môi trường local

## Một lệnh chạy API và Redis

Cần Go 1.25 trở lên trên máy; MySQL và MongoDB dùng các kết nối trong `.env` và cần chạy sẵn. Mở Docker Desktop ở chế độ Linux containers nếu cần launcher tự bật Redis. Nếu Redis đã chạy và xác thực được, không cần Docker.

```sh
go run ./cmd/dev
```

Trên Windows, có thể double-click `start-dev.bat` ở thư mục gốc repo. Cửa sổ giữ lại thông báo nếu khởi động lỗi.

Lệnh nạp `.env`, ưu tiên biến môi trường đã đặt trong terminal. Nó kiểm tra Redis bằng `PING` có xác thực và dùng lại Redis đang chạy. Nếu Redis ở localhost chưa chạy, lệnh bật Redis qua `compose.dev.yml`, chờ sẵn sàng rồi chạy API. Redis từ xa không truy cập được hoặc xác thực thất bại sẽ báo lỗi; launcher không tạo Redis khác thay thế.

API được build vào `.dev/` rồi chạy trực tiếp. `Ctrl+C` dừng API và chỉ dừng Redis container mà lần chạy đó đã bật. MySQL/MongoDB và Redis được dùng lại vẫn tiếp tục chạy. Redis dùng volume để giữ dữ liệu khi dừng hoặc khởi động lại.

## `.env` cho Redis và LiveKit Cloud

Giữ các cấu hình MySQL/MongoDB, JWT, email hiện tại. Đặt thêm:

```dotenv
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
LIVEKIT_URL=wss://your-project.livekit.cloud
LIVEKIT_API_KEY=your-cloud-api-key
LIVEKIT_API_SECRET=your-cloud-api-secret
LIVEKIT_API_URL=
```

Lấy URL/key/secret từ project LiveKit Cloud của bạn. Để `LIVEKIT_API_URL` trống: backend tự suy ra HTTPS từ URL WSS của Cloud. Nếu chuyển từ server local, xóa giá trị `http://localhost:7880` hoặc `http://127.0.0.1:7880` cũ ở biến này. API secret chỉ đặt ở backend.

Launcher giữ nguyên các biến LiveKit trong `.env`; không tạo LiveKit container, không mở các cổng 7880/7881/7882, không gán `devkey/secret` và không gọi Cloud khi khởi động. Backend dùng cấu hình Cloud khi thực hiện các thao tác video. Bỏ trống cả bốn biến LiveKit sẽ tắt chức năng video như khi chạy API trực tiếp.

Redis container chỉ mở cổng trên `127.0.0.1`, mặc định 6379. Đổi cổng bằng `REDIS_ADDR=127.0.0.1:<port>`. Nếu đã chạy bản launcher cũ, có thể dừng LiveKit container cũ trong Docker Desktop; launcher mới chỉ quản lý Redis.

## Chỉ bật hoặc dừng dependencies

```sh
go run ./cmd/dev -services-only
go run ./cmd/dev -stop
```

`-services-only` để Redis container tiếp tục chạy và không chạy API. `-stop` chủ động dừng Redis thuộc Compose project hiện tại; không xóa volume, không dừng server native hoặc project Docker khác. Khi lần chạy API tiếp theo dùng lại Redis này, `Ctrl+C` sẽ để nó chạy tiếp.

Project mặc định là `livemart-dev`; đổi bằng `DEV_COMPOSE_PROJECT` nếu cần nhiều checkout. Redis volume thuộc project này. Tránh chạy nhiều API cùng lúc với cùng project/cổng.

## Khi không khởi động được

- **Docker chưa sẵn sàng:** mở Docker Desktop và đợi engine chạy; kiểm tra `docker info` và `docker compose version` trong terminal.
- **Redis đang có nhưng xác thực thất bại:** kiểm tra `REDIS_PASSWORD` có khớp Redis đang chạy. Launcher sẽ dừng tại đây.
- **Redis container không bật được:** xem thông báo Docker và kiểm tra cổng trong `REDIS_ADDR` có bị chương trình khác dùng không.
- **Video Cloud lỗi:** kiểm tra URL/key/secret Cloud trong `.env` và giá trị `LIVEKIT_API_URL` cũ; launcher không kiểm tra kết nối Cloud thay cho các API video.
- **MySQL/MongoDB không kết nối được:** kiểm tra database hiện tại và URL trong `.env`; launcher không tạo database thay thế.

Xem trạng thái/log khi cần (thay tên project nếu đã cấu hình khác):

```sh
docker compose -p livemart-dev -f compose.dev.yml ps
docker compose -p livemart-dev -f compose.dev.yml logs --tail=100
```

## Kiểm thử

```sh
go test ./...
```

Test Docker thật là opt-in, dùng project/cổng/volume riêng và tự xóa dữ liệu test. Ví dụ PowerShell:

```powershell
$env:DEVSTACK_DOCKER_TEST = "1"
go test ./internal/devstack -run TestDockerLifecycle -v -count=1
Remove-Item Env:DEVSTACK_DOCKER_TEST
```

Các test MySQL hiện có cần `TEST_MYSQL_DSN` trỏ đến database dùng cho test.
