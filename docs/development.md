# Chạy môi trường local

## Một lệnh chạy API, Redis và LiveKit

Mở Docker Desktop ở chế độ Linux containers. Cần Go 1.25 trở lên trên máy; MySQL và MongoDB vẫn dùng các kết nối trong `.env` và cần chạy sẵn.

```sh
go run ./cmd/dev
```

Trên Windows, có thể double-click `start-dev.bat` ở thư mục gốc repo. Cửa sổ giữ lại thông báo nếu khởi động lỗi.

Lệnh nạp `.env`, ưu tiên biến môi trường đã đặt trong terminal. Nó kiểm tra Redis bằng `PING` có xác thực, kiểm tra LiveKit bằng API `ListRooms` có ký token. Dịch vụ đang chạy và xác thực được sẽ được dùng lại. Dịch vụ ở localhost chưa chạy được bật qua `compose.dev.yml`, sau đó API mới khởi động. Không tự thay thế dịch vụ từ xa hoặc dịch vụ đang chiếm cổng nhưng xác thực thất bại.

API được build vào `.dev/` rồi chạy trực tiếp. `Ctrl+C` dừng API và chỉ dừng các container mà lần chạy đó đã bật. MySQL/MongoDB và dịch vụ được dùng lại vẫn tiếp tục chạy. Redis dùng volume để giữ dữ liệu khi dừng hoặc khởi động lại.

## `.env` tối thiểu cho Redis/LiveKit local

Giữ các cấu hình MySQL/MongoDB, JWT, email hiện tại. Đặt thêm:

```dotenv
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
LIVEKIT_URL=ws://127.0.0.1:7880
LIVEKIT_API_URL=http://127.0.0.1:7880
LIVEKIT_API_KEY=devkey
LIVEKIT_API_SECRET=secret
```

`devkey`/`secret` chỉ là thông tin mẫu cho local. Có thể đặt key/secret khác; launcher truyền đúng cặp đó cho container. Nếu cả hai bỏ trống và room API ở localhost, launcher dùng cặp mẫu trên. Khi chạy API trực tiếp bằng `go run ./cmd`, các biến LiveKit trống vẫn tắt chức năng video như trước.

Container chỉ mở cổng trên `127.0.0.1`: Redis 6379, LiveKit HTTP/WebSocket 7880, WebRTC TCP 7881 và UDP 7882. Đổi cổng Redis/HTTP bằng `REDIS_ADDR` và `LIVEKIT_API_URL`; khi client URL cũng ở localhost, launcher đồng bộ cổng `LIVEKIT_URL`. Đổi cổng media bằng `DEV_LIVEKIT_TCP_PORT` / `DEV_LIVEKIT_UDP_PORT`.

Nếu đang dùng LiveKit Cloud hoặc server khác, giữ URL/key/secret đang dùng. Khi có domain WSS công khai và API nội bộ riêng, cấu hình cả `LIVEKIT_URL=wss://...` và `LIVEKIT_API_URL=http://127.0.0.1:7880`. Launcher giữ URL công khai, nhưng container mặc định chỉ phục vụ thử nghiệm trên cùng máy. Để điện thoại xem video từ mạng khác, cần triển khai LiveKit có địa chỉ media truy cập được và cấu hình HTTPS/WSS, ICE/TURN phù hợp; forward cổng 7880 riêng chưa đủ cho WebRTC.

## Chỉ bật hoặc dừng dependencies

```sh
go run ./cmd/dev -services-only
go run ./cmd/dev -stop
```

`-services-only` để container tiếp tục chạy và không chạy API. `-stop` chủ động dừng Redis/LiveKit thuộc Compose project hiện tại; không xóa volume, không dừng server native hoặc project Docker khác. Khi lần chạy API tiếp theo dùng lại các container này, `Ctrl+C` sẽ để chúng chạy tiếp.

Project mặc định là `livemart-dev`; đổi bằng `DEV_COMPOSE_PROJECT` nếu cần nhiều checkout. Redis volume thuộc project này. Tránh chạy nhiều API cùng lúc với cùng project/cổng.

## Khi không khởi động được

- **Docker chưa sẵn sàng:** mở Docker Desktop và đợi engine chạy; kiểm tra `docker info` và `docker compose version` trong terminal.
- **Dịch vụ đang có nhưng xác thực thất bại:** kiểm tra `REDIS_PASSWORD`, `LIVEKIT_API_KEY` và `LIVEKIT_API_SECRET` có khớp server đang chạy. Launcher sẽ dừng tại đây.
- **Container không bật được:** kiểm tra cổng Redis, LiveKit và cả TCP/UDP media có đang bị chương trình khác dùng không.
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
