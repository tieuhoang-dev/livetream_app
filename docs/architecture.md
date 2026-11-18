# Cấu trúc khởi động backend

`cmd/main.go` → `internal/app.Run(ctx)` là điểm ghép ứng dụng. Entrypoint nhận
signal và báo lỗi sau khi cleanup hoàn tất; không truy vấn dữ liệu hay đăng
ký từng endpoint ở đây.

| Vị trí | Trách nhiệm |
| --- | --- |
| `internal/app/config.go` | Nạp `.env` tùy chọn, giữ ưu tiên env, kiểm tra setting bắt buộc/port |
| `internal/app/storage.go` | Mở kho dữ liệu theo thứ tự, migrate/index, sở hữu và đóng kết nối kể cả khi startup lỗi giữa chừng |
| `internal/app/router.go` | Middleware CORS, uploads, Studio và gọi root route registry |
| `internal/app/workers.go` | Khởi động, hủy và join worker chat/lifecycle |
| `internal/app/server.go` | Phục vụ HTTP và drain request khi dừng |
| `config/mysql.go`, `mongo.go`, `redis.go` | Kết nối riêng từng kho; nhận context và trả lỗi, không gọi log.Fatal |
| `models/migrate.go` | Migration SQL, trả lỗi về caller |
| `routes/routes.go` | Đăng ký các nhóm route và WebSocket một lần |
| `routes/users.go`, `products.go`, `orders.go`, `sales.go`, `livestreams.go`, `delivery.go` | Endpoint theo từng dịch vụ |
| `controller/http/`, `controller/websocket/` | Adapter transport: đọc input, xác thực quyền, gọi service, trả output |
| `controller/services/` | Business transaction, lưu trữ và thông báo sau commit |
| `internal/realtime/` | Root WebSocket: kết nối, auth, correlation, subscription và fan-out |

`config.DB`, `ChatCollection` và `RedisClient` vẫn là các handle dùng chung
cho business service hiện có. `app` giữ quyền sở hữu vòng đời kết nối; route
và controller không tự mở/đóng database hoặc khởi động worker. Transport WS
vẫn độc lập với LiveKit video. Không thêm HTTP endpoint cho realtime.

Thứ tự dừng: drain HTTP → đóng/join WebSocket → hủy/join worker → đóng các
kho dữ liệu. Các goroutine không sống lâu hơn kết nối mà chúng sử dụng.
Nếu startup lỗi trước khi nhận traffic, tài nguyên đã mở vẫn được thu hồi.

Kiểm thử runtime dùng database SQL test, Mongo database có tên ngẫu nhiên và
Redis riêng qua `TEST_MYSQL_DSN`, `TEST_MONGO_URI`, `TEST_REDIS_ADDR`. Nó chạy
router thật, CORS/Studio/WS, chuyển một tin Redis sang Mongo, dừng ứng dụng và
kiểm tra socket/kết nối đã đóng. Kiểm thử API đầy đủ dùng `routes.Setup` để
bảo đảm việc chia nhóm giữ nguyên URL và quyền truy cập. Xem
[development.md](development.md), [REALTIME.md](../REALTIME.md) và
[SHIPPING.md](../SHIPPING.md).
