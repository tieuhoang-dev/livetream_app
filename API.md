# API

Base URL khi chạy local: `http://localhost:8080`.

Các route được bảo vệ nhận header `Authorization: Bearer <JWT>` từ API login.
User phải được xác thực email và có trạng thái `active`. User ID luôn lấy từ JWT,
không lấy từ request body. WebSocket vẫn giữ route `/api/auth/ws` và hỗ trợ
`?token=<JWT>` như trước.

## Tài khoản

| Method | Route | Body / định dạng | JWT |
| --- | --- | --- | --- |
| POST | `/api/users/register` | `username`, `email`, `password` | Không |
| POST | `/api/users/verify` | `email`, `code` | Không |
| POST | `/api/users/login` | `email`, `password` | Không |
| POST | `/api/users/password/reset-request` | `email` | Không |
| POST | `/api/users/password/reset` | `email`, `code`, `new_password` | Không |
| PUT | `/api/users/me` | `fullname`, `avatar` | Có |
| POST | `/api/users/me/avatar` | multipart field `file` | Có |

Avatar nhận JPG/JPEG/PNG/WebP, request tối đa 10 MB. Response trả `url` bắt đầu
bằng `/uploads/`, có thể GET trực tiếp. Profile và avatar chỉ cập nhật tài khoản
đang đăng nhập.

OTP reset được hash và lưu riêng với OTP xác thực email, hết hạn sau 15 phút và
chỉ dùng một lần. Reset yêu cầu tài khoản đã xác thực. Cần cấu hình
`BREVO_API_KEY` và `FROM_EMAIL`; reset-request trả `502 email_delivery_failed`
nếu dịch vụ email chưa cấu hình hoặc gửi thất bại.

## Sản phẩm

| Method | Route | Công dụng | JWT |
| --- | --- | --- | --- |
| GET | `/api/products` | Danh sách, phân trang và tìm kiếm | Không |
| GET | `/api/products/:product_id` | Chi tiết sản phẩm | Không |
| POST | `/api/products` | Tạo sản phẩm | Có |
| PATCH | `/api/products/:product_id` | Sửa thông tin sản phẩm | Có |
| POST | `/api/products/:product_id/stock` | Điều chỉnh kho gốc | Có |
| DELETE | `/api/products/:product_id` | Xóa sản phẩm chưa được sử dụng | Có |

Tạo sản phẩm:

```json
{
  "name": "Áo thun",
  "price": 150000,
  "stock_quantity": 20,
  "image_url": "https://example.com/ao-thun.jpg",
  "description": "Áo cotton"
}
```

`name`, `price`, `stock_quantity` bắt buộc. `seller_id` tự lấy từ JWT.
Tên sau khi bỏ khoảng trắng hai đầu phải khác rỗng; tên, mô tả và URL ảnh tối
đa 255 ký tự. Giá từ 0 đến 99,999,999.99, tối đa 2 chữ số thập phân. Kho ban đầu
không âm. Ảnh/mô tả là tùy chọn; ảnh nhận URL HTTP/HTTPS hoặc đường dẫn
`/uploads/...`. ProductService chỉ lưu URL ảnh, không tải file từ URL.

Create trả 201; detail/update/stock trả 200. JSON trả các trường `id`,
`seller_id`, `name`, `price`, `stock_quantity`, `image_url`, `description`,
`created_at`, không trả object User hoặc thông tin xác thực của người bán.

Danh sách nhận `page` (mặc định 1, tối đa 1,000,000), `page_size` (mặc định 20,
tối đa 100), `seller_id` (UUID), `q` (tìm một phần tên, tối đa 100 ký tự)
và `in_stock=true` để chỉ lấy sản phẩm còn hàng trong kho gốc.
Sắp xếp mới nhất trước, theo `created_at DESC, id DESC`. Các ký tự `%`, `_`, `!`
trong `q` được tìm như ký tự thường, không phải wildcard SQL.

```text
GET /api/products?seller_id=<UUID>&page=1&page_size=20&q=ao&in_stock=true
```

Response danh sách: `{ "data": [...], "total": 42, "page": 1, "page_size": 20 }`.
Không có kết quả trả `data: []`, `total: 0`.

PATCH chỉ nhận các trường cần sửa trong `name`, `price`, `image_url`,
`description`. Giá 0 và chuỗi rỗng cho ảnh/mô tả được lưu đúng; không cho sửa
owner hoặc ghi đè kho qua PATCH. Body rỗng, nhiều JSON object liên tiếp, trường
không hỗ trợ và request JSON quá 64 KB bị từ chối với 400.

Điều chỉnh kho dùng `{ "quantity_delta": 5 }` để nhập thêm 5 hoặc
`{ "quantity_delta": -2 }` để rút 2. Delta phải khác 0; không được làm âm kho
hoặc tràn số. Đây là thao tác cộng/trừ, mỗi request thành công đều được áp dụng
một lần; không tự retry request chưa rõ kết quả. Transaction khóa cùng dòng
sản phẩm với thao tác đặt hàng và cấp/trả kho live, để các cập nhật đồng thời
không ghi đè nhau. `stock_quantity` chỉ là kho gốc còn tự do, không bao gồm số
đã chuyển sang live hay đã giữ cho đơn hàng.

Chỉ chủ sản phẩm được sửa, điều chỉnh kho hoặc xóa (người khác nhận 403).
Xóa thành công trả 204; sản phẩm đang gắn trong live hoặc đã được tham chiếu
bởi bất kỳ OrderItem nào trả `409 product_in_use`, kể cả đơn đã hủy/hoàn, để
giữ lịch sử. Không tự xóa dây chuyền các đơn/live. Sửa giá/thông tin sản phẩm
gốc không thay đổi giá đã chốt trong đơn hoặc snapshot sản phẩm đã gắn vào live.

## Đơn hàng

Tất cả route bên dưới yêu cầu JWT.

| Method | Route | Body | Thành công |
| --- | --- | --- | --- |
| GET | `/api/orders` | Query `channel`, `status`, `page`, `page_size` | 200 |
| GET | `/api/orders/:order_id` | Không cần | 200 |
| GET | `/api/sales/offline` | Query `status`, `page`, `page_size` | 200 |
| GET | `/api/sales/offline/:order_id` | Không cần | 200 |
| POST | `/api/orders` | `product_id`, `quantity`, `payment_method`, tùy chọn `livestream_id` | 201 |
| POST | `/api/orders/:order_id/pay` | `payment_method` | 200 |
| POST | `/api/orders/:order_id/cancel` | Không cần | 200 |
| POST | `/api/orders/:order_id/refund` | `refund_reason` | 200 |

Ví dụ tạo đơn:

```json
{
  "product_id": "<UUID sản phẩm>",
  "quantity": 2,
  "payment_method": "manual",
  "livestream_id": "<UUID live, bỏ trường này nếu mua thông thường>"
}
```

Response có `order_id`, `total_amount`, `status: "pending"`. Số lượng phải lớn
hơn 0. Đơn offline là đơn mua từ danh mục ngoài livestream. Bỏ `livestream_id`
để áp dụng giá và trừ kho gốc. Khi có `livestream_id`, phiên phải đang `start`
và có sản phẩm được gắn vào. Đơn áp dụng `discount_price` và trừ kho của live.
Sản phẩm gốc được tạo qua `POST /api/products`.

`GET /api/orders` chỉ trả đơn của người mua đang đăng nhập. `channel` nhận
`offline` hoặc `live`, mặc định trả cả hai. `GET /api/sales/offline` trả đơn
ngoài live có các sản phẩm thuộc người bán đang đăng nhập. Cả hai danh sách
nhận `status=pending|paid|cancelled|refunded` và dùng phân trang như danh mục.
Thứ tự là `created_at DESC, id DESC`. Bản ghi gồm `id`, `livestream_id`,
`total_amount`, `status`, `refund_reason`, `created_at` và `updated_at`.

Các endpoint chi tiết thêm `items` với `product_id`, `name`, `image_url`,
`quantity` và `price_at_purchase`. Tên và ảnh lấy từ danh mục hiện tại.
`payment` gồm `payment_method`, `status` và `amount`, hoặc `null` nếu bản ghi
thanh toán chưa có. Người bán xem đơn offline qua `/api/sales/offline`.
Người mua thực hiện các thao tác trên đơn qua `/api/orders`.

Chỉ chủ đơn được thanh toán, hủy hoặc hoàn đơn. Các chuyển trạng thái:

- `pending → paid`: thanh toán.
- `pending → cancelled`: hủy, trả lại tồn kho.
- `paid → refunded`: hoàn đơn, trả lại tồn kho và lưu lý do.

Order và Payment được cập nhật cùng transaction; thao tác lặp hoặc chuyển trạng
thái không hợp lệ bị từ chối. Các route pay/refund hiện chỉ ghi nhận trạng thái
nội bộ, **chưa thu tiền/hoàn tiền qua cổng thanh toán**.

## Tìm livestream

| Method | Route | Công dụng | JWT |
| --- | --- | --- | --- |
| GET | `/api/livestreams` | Tìm và lọc phiên live | Không |
| GET | `/api/livestreams/:livestream_id` | Chi tiết phiên live | Không |
| GET | `/api/livestreams/:livestream_id/products` | Danh sách sản phẩm trong live | Không |

Danh sách phiên nhận `q` (tìm theo tiêu đề, tối đa 100 ký tự), `seller_id` (UUID),
`product_id` (UUID sản phẩm được gắn), `status=start|end|all` và
`sort=newest|oldest`. Mặc định là `start` và `newest`. Thứ tự dùng `started_at`
và `id` để phân trang ổn định khi hai phiên có cùng thời gian. Ví dụ
`GET /api/livestreams?q=ao&status=start&sort=newest&page=1&page_size=20`.

Danh sách live và danh sách sản phẩm trong live dùng `{data, total, page, page_size}`
với mặc định trang 1, 20 bản ghi mỗi trang và tối đa 100 bản ghi mỗi trang.
Chi tiết live trả `id`, `seller_id`, `seller_username`, `seller_avatar`, `title`,
`status`, `started_at` và `ended_at`. Sản phẩm trong live trả `id`, `product_id`,
`name`, `image_url`, `description`, `original_price`, `discount_price`,
`stock_quantity` và `is_pinned`. Sản phẩm ghim đứng đầu. Dùng `product_id` khi
đặt đơn. Live đã kết thúc vẫn đọc được theo ID, còn đặt đơn yêu cầu live đang mở.

## Quản lý sản phẩm trong live

Các route ghi yêu cầu JWT. Chỉ chủ live được thao tác và chỉ được thêm sản
phẩm thuộc tài khoản đó.

| Method | Route | Body | Thành công |
| --- | --- | --- | --- |
| POST | `/api/livestreams/:livestream_id/products` | `product_id`, `discount_price`, `stock_quantity`, tùy chọn `is_pinned` | 201 |
| DELETE | `/api/livestreams/:livestream_id/products/:product_id` | Không cần | 204 |

`stock_quantity > 0`, `discount_price >= 0`. Thêm sản phẩm chuyển lượng hàng này
từ kho sản phẩm gốc sang phần kho dành riêng cho live, đồng thời sao chép tên,
ảnh, mô tả và giá gốc. Không được thêm trùng hoặc thêm vào live đã kết thúc.
Xóa trả lại phần hàng chưa bán; bị chặn nếu còn đơn `pending` hoặc `paid` liên
quan, để các đơn đó vẫn có thể hủy/hoàn đúng kho.

Tạo/kết thúc live vẫn qua WebSocket với `type: "create_live"` (content là tiêu
đề) và `type: "end_live"` (content là UUID live). Live mới lưu `ended_at = NULL`.
`type: "join_live"` chỉ cho tham gia live có trạng thái `start`; live đã kết
thúc hoặc không tồn tại trả `join_live_error`.

Khi kết thúc live, kho chưa bán được trả về kho gốc trong cùng transaction.
Hủy hoặc hoàn đơn sau đó trả hàng về kho gốc. Gửi lại `end_live` giữ nguyên
`ended_at` và không trả kho lần nữa. Với phiên đã kết thúc trước bản cập nhật,
gửi lại `end_live` từ tài khoản chủ live để trả phần kho còn giữ.

## Bình luận trong live

| Method | Route | Dữ liệu | JWT |
| --- | --- | --- | --- |
| GET | `/api/livestreams/:livestream_id/comments` | Query `after` tùy chọn | Có |
| POST | `/api/livestreams/:livestream_id/comments` | `{"content":"Còn hàng không?"}` | Có |

Bình luận được lưu theo từng phòng. Backend lấy tên người gửi từ tài khoản
đã xác thực. Nội dung là văn bản từ 1 đến 300 ký tự sau khi bỏ khoảng trắng
ở hai đầu. Khi phiên kết thúc, vẫn đọc được lịch sử và yêu cầu gửi mới trả `409`.

Lần đọc đầu lấy tối đa 50 bình luận gần nhất theo thứ tự cũ đến mới. Response có
`data`, `next_cursor` và `has_more`. Gửi `next_cursor` làm `after` để lấy các
bình luận mới theo từng lượt. ID và cursor là chuỗi số. Frontend đọc phần mới
mỗi 2,5 giây trong phiên đang mở.

## Video qua LiveKit

| Method | Route | Body | JWT |
| --- | --- | --- | --- |
| POST | `/api/livestreams/:livestream_id/media-token` | `{"role":"publisher"}` hoặc `{"role":"viewer"}` | Có |
| POST | `/api/livestreams/:livestream_id/end` | Không cần | Có |

Token media trả `{ "url": "wss://…", "token": "…" }`, có hiệu lực 90 giây
để bắt đầu kết nối và chỉ dùng cho phòng được chỉ định. Chỉ chủ phiên được
lấy token phát camera/micro. Người xem chỉ được nhận media. Mỗi lượt xem có
identity riêng để nhiều tab có thể xem song song. Live đã kết thúc trả `409`,
người phát sai quyền nhận `403`, cấu hình LiveKit bị thiếu trả `503`.

Kết thúc phiên vẫn hỗ trợ lệnh WebSocket `end_live`. Cả hai đường đều cập nhật
trạng thái, trả kho và yêu cầu LiveKit ngắt phòng. `media_closed: false` cho
biết phiên đã kết thúc trong Go nhưng LiveKit chưa xác nhận đóng phòng. Chủ
phiên có thể gọi lại endpoint kết thúc. Trên bản LiveKit tự host, token cũ
có thể dùng lại đến khi hết hạn để kết nối, tùy cấu hình LiveKit. Backend chặn
cấp token mới cho phiên đã kết thúc.

### Chạy local trên Windows

1. Tải `livekit-server.exe` từ [LiveKit Releases](https://github.com/livekit/livekit/releases)
   và chạy `.\livekit-server.exe --dev` trong PowerShell.
2. Thêm vào `.env` của backend rồi khởi động lại Go.

   ```dotenv
   LIVEKIT_URL=ws://127.0.0.1:7880
   LIVEKIT_API_KEY=devkey
   LIVEKIT_API_SECRET=secret
   ```

3. Mở frontend tại `http://localhost:5173`, đăng nhập và tạo phiên. Mở cửa sổ
   ẩn danh bằng tài khoản thứ hai để xem trực tiếp trên cùng máy.

Dùng HTTPS cho frontend và WSS cho LiveKit khi phát từ điện thoại hoặc qua
Internet. `LIVEKIT_URL` phải truy cập được từ trình duyệt. Có thể đặt
`LIVEKIT_API_URL` thành origin HTTP(S) nội bộ cho các lệnh quản lý phòng nếu
khác origin client. Chỉ lưu API key và secret ở backend. Cặp `devkey`/`secret`
dành cho localhost. Xem [cổng và firewall của LiveKit](https://docs.livekit.io/transport/self-hosting/ports-firewall/)
khi tự host qua Internet.

## Lỗi

- 400: body/UUID/phân trang/sản phẩm/số lượng/giá/OTP không hợp lệ hoặc OTP hết hạn.
- 401: chưa đăng nhập hoặc JWT không hợp lệ.
- 403: không sở hữu live hoặc sản phẩm cần thao tác.
- 404: không có tài nguyên; đơn không thuộc user hiện tại cũng trả 404.
- 409: thiếu kho, trùng sản phẩm, trạng thái live/đơn không phù hợp, sản phẩm còn đơn đang xử lý hoặc sản phẩm gốc còn tham chiếu live/đơn.
- 500: lỗi nội bộ, không trả chi tiết SQL ra client.
- 502: gửi email reset thất bại.

## Kiểm tra tích hợp

```sh
TEST_MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/livestream_test?charset=utf8mb4&parseTime=True&loc=Local' go test -v ./...
```

Dùng database thử nghiệm riêng. Test chạy HTTP/WebSocket với MySQL thực, tự
migrate schema và dọn các fixture UUID vừa tạo. Các fixture sản phẩm được tạo
qua API, không chèn trực tiếp vào DB. Bao phủ CRUD sản phẩm, lọc/tìm kiếm/phân
trang, quyền sở hữu, chặn xóa khi còn tham chiếu, điều chỉnh kho đồng thời,
giới hạn giá/kho, JWT, profile/avatar,
quyền sở hữu, chuyển trạng thái đơn, tồn kho khi mua đồng thời, tạo live và
quản lý sản phẩm trong live, OTP reset hết hạn/sai/dùng lại. Test email cố ý bỏ
API key để kiểm tra lỗi cấu hình và dùng OTP fixture cho bước xác nhận; không
gửi email thực. Không cần Redis/MongoDB cho suite API này. Khi không đặt
`TEST_MYSQL_DSN`, integration suite được skip.
