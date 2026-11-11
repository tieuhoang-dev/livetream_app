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
`discount_price`. Sản phẩm gốc phải tồn tại sẵn trong database; repo chưa có
service/API tạo sản phẩm gốc.

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

- 400: body/UUID/số lượng/giá/OTP không hợp lệ hoặc OTP hết hạn.
- 401: chưa đăng nhập hoặc JWT không hợp lệ.
- 403: không sở hữu live hoặc sản phẩm cần gắn vào live.
- 404: không có tài nguyên; đơn không thuộc user hiện tại cũng trả 404.
- 409: thiếu kho, trùng sản phẩm, trạng thái live/đơn không phù hợp hoặc sản phẩm còn đơn đang xử lý.
- 500: lỗi nội bộ, không trả chi tiết SQL ra client.
- 502: gửi email reset thất bại.

## Kiểm tra tích hợp

```sh
TEST_MYSQL_DSN='user:password@tcp(127.0.0.1:3306)/livestream_test?charset=utf8mb4&parseTime=True&loc=Local' go test -v ./...
```

Dùng database thử nghiệm riêng. Test chạy HTTP/WebSocket với MySQL thực, tự
migrate schema và dọn các fixture UUID vừa tạo. Bao phủ JWT, profile/avatar,
quyền sở hữu, chuyển trạng thái đơn, tồn kho khi mua đồng thời, tạo live và
quản lý sản phẩm trong live, OTP reset hết hạn/sai/dùng lại. Test email cố ý bỏ
API key để kiểm tra lỗi cấu hình và dùng OTP fixture cho bước xác nhận; không
gửi email thực. Không cần Redis/MongoDB cho suite API này. Khi không đặt
`TEST_MYSQL_DSN`, integration suite được skip.
