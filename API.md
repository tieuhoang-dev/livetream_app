# API

Base URL khi chạy local: `http://localhost:8080`.

Danh sách live trên FE dùng WebSocket công khai `/api/livestreams/ws` theo
hợp đồng bên dưới. REST danh sách được giữ để tương thích client cũ.

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
| POST | `/api/orders` | `product_id`, `quantity`, `payment_method`, `delivery`, tùy chọn `livestream_id` | 201 |
| POST | `/api/orders/:order_id/pay` | `payment_method` | 200 |
| POST | `/api/orders/:order_id/cancel` | Không cần | 200 |
| POST | `/api/orders/:order_id/refund` | `refund_reason` | 200 |

Ví dụ tạo đơn:

```json
{
  "product_id": "<UUID sản phẩm>",
  "quantity": 2,
  "payment_method": "manual",
  "livestream_id": "<UUID live, bỏ trường này nếu mua thông thường>",
  "delivery": {
    "name": "Người nhận", "phone": "0901234567", "province_code": "1",
    "ward": "Phường nơi nhận", "address_line": "Số nhà, tên đường"
  }
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

Bình luận được gửi và đọc **trực tiếp từ MongoDB** `livestream_app.chat_logs`,
với `message_type: "livestream_comment"` và `livestream_id`. POST chỉ trả `201`
sau khi MongoDB xác nhận lưu, kể cả khi chỉ gửi một tin. Mongo lỗi thì API báo
lỗi; nội dung không được ghi sang SQL. Giới hạn 50 chỉ là kích thước trang đọc.

SQL giữ tài khoản, quan hệ và trạng thái phiên live để kiểm tra quyền. Schema
SQL mới không tạo bảng `livestream_comments`. Bộ đếm trong MongoDB
`chat_counters` giữ ID/cursor dạng chuỗi số để FE tiếp tục dùng API hiện tại.

Khi nâng cấp, dừng backend cũ trước rồi chạy bản mới: nếu bảng bình luận SQL cũ
còn tồn tại, backend chuyển lịch sử sang MongoDB trước khi mở API. ID, tên,
nội dung và thời gian được giữ lại; tiến độ chuyển nằm trong `chat_migrations`
để có thể tiếp tục khi chạy lại. Bảng SQL cũ được giữ làm bản sao dữ liệu cũ,
không được API dùng để đọc hoặc ghi bình luận sau khi chuyển xong.

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


## Checkout: địa chỉ nhận và phí giao hàng

Cập nhật cả FE và backend. Shop cần lưu địa chỉ lấy hàng/phí trước khi nhận
đơn mới. Phí do shop cấu hình theo nội tỉnh/liên tỉnh; ghim bản đồ chỉ hỗ trợ
xác định vị trí, chưa phải báo giá tự động từ hãng vận chuyển.

- GET/PUT `/api/sales/shipping-settings`: JWT của shop. GET trả
  `{configured, settings}`. PUT nhận `pickup_address`, `same_province_fee`,
  `other_province_fee`, `free_shipping_from` (null nếu không áp dụng).
- POST `/api/orders/quote`: JWT người mua, nhận cùng thông tin sản phẩm,
  số lượng, phương thức thanh toán và `delivery` như tạo đơn. Trả `unit_price`,
  `items_subtotal`, `shipping_fee`, `total_amount`, `shipping_zone`,
  `pickup_province`, `currency: VND`. Báo giá không giữ kho.
- POST `/api/orders`: bắt buộc thông tin nhận hàng dưới đây; backend kiểm tra
  lại giá, kho và phí. `expected_total` giúp trả 409 `checkout_changed` khi
  tổng tiền đổi. `request_id` là UUID cho cùng một lần đặt, gửi lại sẽ trả
  cùng đơn, không trừ kho hai lần. Dùng lại key với nội dung khác trả 409.
- Chi tiết đơn của người mua/người bán có `delivery.recipient`,
  `delivery.pickup`, `delivery.note`, `items_subtotal`, `shipping_fee`.
  Địa chỉ và phí là bản chụp lúc đặt, không đổi theo cấu hình shop sau đó.
  Đơn cũ thiếu dữ liệu có `delivery: null`, phí null; không tự điền phí 0.

Ví dụ tạo đơn:

```json
{
  "product_id": "<UUID sản phẩm>",
  "quantity": 2,
  "payment_method": "cod",
  "delivery": {
    "name": "Người nhận",
    "phone": "0901234567",
    "province_code": "1",
    "ward": "Phường nơi nhận",
    "address_line": "Số nhà, tên đường",
    "latitude": 21.028511,
    "longitude": 105.804817
  },
  "note": "Gọi trước khi giao",
  "request_id": "<UUID của lần đặt>",
  "expected_total": 250000
}
```

`latitude`/`longitude` không bắt buộc nhưng phải có đủ cặp nếu gửi. Người dùng
vẫn đặt được bằng địa chỉ nhập tay khi GPS/bản đồ lỗi. Tên tỉnh lấy từ mã
tỉnh hợp lệ; mã/tên được chụp từ API v2 của provinces.open-api.vn ngày 17/11/2026
trong `controller/services/provinces.json`. Tiền được tính bằng đơn vị nhỏ
trong backend, không lấy giá/phí do client tự khai. `cod` và `bank_transfer`
là lựa chọn FE; `manual` còn được nhận cho client cũ.

Collection Postman và Test Studio đã có SHIP-001 để cấu hình phí 0 **cho tài
khoản test** trước các case đơn cũ; payload đặt hàng đã có địa chỉ.

## Danh sách live qua WebSocket

Mở `ws://localhost:8080/api/livestreams/ws` (production HTTPS dùng `wss://`).
Đây là kênh chỉ đọc, không cần JWT và không thay kết nối chat `/api/auth/ws`.
Gửi message sau để nhận danh sách và đăng ký cập nhật cùng bộ lọc:

```json
{
  "type": "list_lives",
  "request_id": "catalog-1",
  "query": {
    "status": "start",
    "sort": "newest",
    "q": "",
    "page": 1,
    "page_size": 12
  }
}
```

Response có `type: "list_lives_success"`, cùng `request_id`, và các trường
`data`, `total`, `page`, `page_size` giống REST. Mặc định `start`, `newest`,
trang 1, 20 dòng; `page_size` từ 1–100. `query` còn nhận `seller_id`,
`product_id` là UUID. `status` nhận `start/end/all`, `sort` nhận `newest/oldest`.
Bộ lọc sai trả `list_lives_error` với `error: "invalid_query"` và không thay
subscription hợp lệ trước đó. Request mới hợp lệ thay subscription cũ.

Khi tạo/kết thúc live hoặc thêm/xóa sản phẩm khỏi live thành công, server đẩy
lại snapshot theo bộ lọc hiện tại, kể cả kết thúc qua HTTP. Client không cần
poll danh sách. Thông báo này được fan-out trong một tiến trình backend;
nếu triển khai nhiều instance cần nối kênh thông báo liên tiến trình.

Kết nối có ping/pong và giới hạn message 16 KiB. Khi mất kết nối, FE tự mở lại
với backoff và gửi lại bộ lọc; không chuyển sang REST. FE dùng kênh này tại
trang khám phá, danh sách chuyển phòng và danh sách live của người bán.
API chi tiết phiên/sản phẩm, đơn hàng và media token vẫn giữ hợp đồng riêng.

## Vị trí hiện tại và nơi lấy hàng của phiên live

`DeliveryAddress` có thêm `location_mode: "manual" | "current"` (tùy chọn cho
client cũ). Chế độ `current` bắt buộc cả latitude và longitude, cùng địa chỉ
đủ tên/điện thoại/tỉnh/phường/đường. Tọa độ GPS không thay tên/số điện thoại
hoặc số căn hộ; người dùng kiểm tra và bổ sung nếu dữ liệu bản đồ chưa đủ.

`POST /api/delivery/reverse-geocode` cần JWT, body:

```json
{"latitude":21.028511,"longitude":105.804817}
```

Response 200 có `address` gồm location_mode=current, province_code/name, ward,
address_line, latitude/longitude và `attribution`. Tên/điện thoại được giữ trên
FE, không gửi đến dịch vụ bản đồ. Tỉnh cũ từ provider được ánh xạ về danh mục
34 tỉnh hiện tại. Ngoài Việt Nam/không xác định tỉnh trả 400
`location_not_supported`; tọa độ thiếu/sai trả 400 `invalid_coordinates`;
lỗi dịch vụ trả 503 `geocoding_unavailable`. Không tự dùng địa chỉ cũ khi GPS
thất bại: FE yêu cầu thử lại hoặc chủ động chọn Nhập địa chỉ.

Backend mặc định gọi Nominatim/OpenStreetMap, tối đa một request/giây trong
một tiến trình, cache tối đa 256 vị trí trong 10 phút. Có thể cấu hình
`GEOCODING_REVERSE_URL` trỏ đến dịch vụ reverse tương thích Nominatim; cần
khởi động lại backend sau khi đổi. Nếu chạy nhiều instance hoặc lưu lượng
cao, dùng dịch vụ riêng/thích hợp thay vì public endpoint. FE giữ attribution
OpenStreetMap. Reverse geocoding và tile maps đều cần Internet.

Hai API sau cần JWT **chủ phiên**, không xuất địa chỉ qua danh sách/chi tiết
live công khai:

- `GET /api/livestreams/:id/pickup`: `{pickup_address: ...}`; nếu chưa có nơi
  riêng thì dùng địa chỉ shop để điền sẵn, hoặc null khi shop chưa cấu hình.
- `PUT /api/livestreams/:id/pickup`: body `{pickup_address: DeliveryAddress}`.
  Lưu địa chỉ cho phiên đang mở; người khác nhận 403, phiên kết thúc nhận 409.

Khi bắt đầu phát, FE mặc định lấy GPS mới, tự điền địa chỉ và xin người bán
kiểm tra/xác nhận. Sau khi lưu nơi lấy hàng mới, FE mới xin media token.
Có thể chuyển sang Nhập địa chỉ nếu không dùng GPS. Đây là vị trí tại thời
điểm bắt đầu phát, không phải theo dõi vị trí liên tục.

Checkout trong live dùng nơi lấy hàng của **phiên đó** và mức phí của shop;
đơn ngoài live vẫn dùng địa chỉ shop. Phiên cũ chưa có địa chỉ riêng dùng
địa chỉ shop như trước. Cập nhật nơi phát không đổi địa chỉ shop hoặc snapshot
đã lưu trong đơn cũ. Migration thêm `livestream_pickups` và location_mode vào
các địa chỉ đã có; không cần tự xóa/tạo lại bảng.

### Khắc phục shop có địa chỉ live nhưng thiếu phí ship

`GET /api/livestreams/:id/pickup` trả thêm `shipping_configured` và
`shipping_rates` (null nếu chưa có). `PUT` nhận thêm:

```json
{
  "pickup_address": { "name": "Shop", "phone": "0901234567", "province_code": "1", "ward": "Phường Ba Đình", "address_line": "12 Đường shop", "location_mode": "manual" },
  "shipping_rates": {
    "same_province_fee": 25000,
    "other_province_fee": 40000,
    "free_shipping_from": null
  }
}
```

Backend lưu địa chỉ phiên và mức phí trong cùng transaction. Shop chưa có
cấu hình thì bắt buộc cả hai mức phí; thiếu trả 409 `shipping_not_configured`,
phí sai/thiếu một mức trả 400 `invalid_shipping_settings`. Không tự gán phí 0.
Khi tạo cấu hình lần đầu, địa chỉ đã xác nhận cũng là địa chỉ shop ban đầu.
Shop đã có cấu hình thì việc cập nhật phí không đổi địa chỉ shop; các đơn cũ
vẫn giữ snapshot. Client cũ có thể bỏ shipping_rates nếu shop đã cấu hình.

FE đưa việc nhập phí vào bước bắt đầu phát. Chủ phiên cũng có thể sửa bằng
Cài đặt giao hàng trong phòng mà không dừng video; người mua thử tính lại sau
khi cấu hình được lưu.

`POST /api/orders/quote` chỉ cần khu vực nhận hàng, ví dụ
`{"product_id":"<UUID>","quantity":1,"delivery":{"province_code":"1"}}`
(kèm livestream_id khi mua live). payment_method có thể bỏ qua ở bước quote.
Báo giá không yêu cầu tên/điện thoại/phường/đường, nên FE hiện phí sớm khi chọn
tỉnh. `POST /api/orders` vẫn yêu cầu đủ thông tin nhận hàng và payment_method;
việc nới dữ liệu quote không nới điều kiện tạo đơn. Nếu gửi location_mode=current
vào quote thì tọa độ vẫn phải hợp lệ. Cập nhật backend cùng phiên bản FE này.
