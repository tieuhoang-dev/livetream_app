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
tối đa 100), `seller_id` (UUID) và `q` (tìm một phần tên, tối đa 100 ký tự).
Sắp xếp mới nhất trước, theo `created_at DESC, id DESC`. Các ký tự `%`, `_`, `!`
trong `q` được tìm như ký tự thường, không phải wildcard SQL.

```text
GET /api/products?seller_id=<UUID>&page=1&page_size=20&q=ao
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
hơn 0. Live phải đang `start` và có sản phẩm được gắn vào; giá mua trong live là
`discount_price`. Sản phẩm gốc được tạo qua `POST /api/products`.

Chỉ chủ đơn được thanh toán, hủy hoặc hoàn đơn. Các chuyển trạng thái:

- `pending → paid`: thanh toán.
- `pending → cancelled`: hủy, trả lại tồn kho.
- `paid → refunded`: hoàn đơn, trả lại tồn kho và lưu lý do.

Order và Payment được cập nhật cùng transaction; thao tác lặp hoặc chuyển trạng
thái không hợp lệ bị từ chối. Các route pay/refund hiện chỉ ghi nhận trạng thái
nội bộ, **chưa thu tiền/hoàn tiền qua cổng thanh toán**.

## Sản phẩm trong live

Tất cả route bên dưới yêu cầu JWT. Chỉ chủ live được thao tác, và chỉ được thêm
sản phẩm thuộc tài khoản đó.

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
