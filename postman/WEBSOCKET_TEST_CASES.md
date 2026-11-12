# Test WebSocket trong Postman

WebSocket được test bằng **New → WebSocket Request**, không phải gửi GET HTTP
thông thường trong collection. File collection v2.1 chỉ chạy HTTP; file
`websocket-messages.json` là thư viện payload để copy, không phải collection
WebSocket import tự động.

Trước khi chạy, hoàn tất folder `02 - HTTP tu dong`, giữ Environment có các
biến `seller_token`, `buyer_token`, `seller_id`, `buyer_id`, `ws_url`.

Mở hai tab:

```text
Seller: {{ws_url}}/api/auth/ws?token={{seller_token}}
Buyer:  {{ws_url}}/api/auth/ws?token={{buyer_token}}
```

JWT hợp lệ: handshake 101 Switching Protocols. HTTP GET thông thường có JWT
nhưng không có header Upgrade không phải một kết nối WebSocket thành công.

| ID | Tab / điều kiện | Message / thao tác | Kết quả mong đợi |
| --- | --- | --- | --- |
| WS-001 | Tab mới, không JWT | Connect `{{ws_url}}/api/auth/ws` | Handshake bị từ chối, HTTP 401 |
| WS-002 | Tab mới, JWT sai | Connect `...?token=invalid-token` | HTTP 401, không upgrade |
| WS-003 | Seller đã connect | `{"type":"create_live","content":"Postman live"}` | `create_live_success`, có UUID `id`, đúng `seller_id`, `status: "start"`; copy `id` vào `live_id` |
| WS-004 | Buyer, live đang start | `{"type":"join_live","content":"{{live_id}}"}` | `join_live_success`, đúng ID |
| WS-005 | Buyer không phải chủ live | `{"type":"end_live","content":"{{live_id}}"}` | `end_live_error`; live vẫn tham gia được |
| WS-006 | Buyer, UUID không tồn tại | `{"type":"join_live","content":"{{missing_id}}"}` | `join_live_error` |
| WS-007 | Seller và buyer cùng online | Text message bên dưới | Seller nhận ACK, buyer nhận cùng `id`, nội dung đúng, `status: "queued"` |
| WS-008 | Seller | Gửi message có `sender_id` giả là buyer | `from` vẫn là `seller_id` lấy từ JWT |
| WS-009 | Seller | Text không có `receiver_id` | `message_error`; có thể tiếp tục gửi tin hợp lệ trên cùng socket |
| WS-010 | Seller → buyer | Lần lượt đổi type thành `message`, `text`, `image`, `video`, `voice`, `file` | Type được giữ đúng; hai tab nhận cùng ID, tempId và content |
| WS-011 | Seller, sau folder HTTP 04 | `{"type":"end_live","content":"{{live_id}}"}` | `end_live_success`; copy ID vào `ended_live_id` |
| WS-012 | Buyer, sau WS-011 | `{"type":"join_live","content":"{{ended_live_id}}"}` | **`join_live_error`**, không phải success; đây là regression của lỗi join live đã kết thúc |
| WS-013 | Giữ tab seller cũ, mở tab seller mới cùng JWT | Connect tab mới, sau đó buyer gửi tin đến seller | Tab seller cũ bị đóng; tab seller mới nhận tin, không bị cleanup của tab cũ xóa mất |
| WS-014 | Buyer disconnect, seller còn online | Seller gửi tin đến `buyer_id` | Seller nhận ACK queued; chưa thể kết luận buyer đã đọc/nhận hoặc Mongo đã lưu chỉ từ ACK |
| WS-015 | Tab test riêng | Gửi payload JSON với content > 1 MiB | Server đóng kết nối do giới hạn read, thường close code 1009; kết nối mới vẫn mở được |

## Payload chat mẫu

Gửi trong tab **seller**:

```json
{
  "type": "text",
  "receiver_id": "{{buyer_id}}",
  "sender_id": "{{buyer_id}}",
  "temp_id": "pm-msg-001",
  "content": "Xin chào từ Postman"
}
```

`sender_id` trong ví dụ cố ý giả mạo để kiểm tra WS-008. Response đúng:

```json
{
  "type": "text",
  "id": "<UUID server tạo>",
  "tempId": "pm-msg-001",
  "from": "<seller_id>",
  "to": "<buyer_id>",
  "content": "Xin chào từ Postman",
  "status": "queued"
}
```

Response còn có `username`, `avatar`, `timestamp`. So sánh hai tab có cùng `id`
và `tempId`. Trường gửi lên là **`temp_id`**, trường response là **`tempId`**.
`receiver_id` là UUID **người dùng**, không phải UUID livestream. Chat hiện là
chat trực tiếp giữa người dùng, chưa phải broadcast theo phòng live.

Với `image`, `video`, `voice`, `file`, `content` vẫn là string (ví dụ URL test).
Đổi type không thực hiện upload file hoặc phát media. `queued` chỉ xác nhận
Redis nhận tin; Postman chưa có API lịch sử chat để truy vấn xác nhận MongoDB
đã lưu. Muốn xác nhận persistence cần đọc DB/log worker bằng công cụ khác.

## Case bổ sung cần fixture hoặc chờ thời gian

| ID | Chuẩn bị và thao tác | Kết quả mong đợi |
| --- | --- | --- |
| EDGE-001 | User mới chưa verified; gửi `/api/users/verify` với OTP chắc chắn khác mã hiện tại | 400, `Invalid verification code` |
| EDGE-002 | User mới chưa verified; chờ OTP > 15 phút rồi verify đúng mã cũ | 400, `Verification code has expired` |
| EDGE-003 | Xin OTP reset; gửi `/api/users/password/reset` với mã sai khi mã thật vẫn còn hạn | 400, `invalid_otp`; password không đổi |
| EDGE-004 | Xin OTP reset; chờ > 15 phút rồi xác nhận mã cũ | 400, `otp_expired`; password không đổi |
| EDGE-005 | Dùng JWT đã hết hạn 12 giờ để gọi route bảo vệ/WS | HTTP 401 trước khi thao tác dữ liệu/upgrade |
| EDGE-006 | Chuẩn bị tài khoản `status != active` bằng fixture DB, dùng JWT hợp lệ của user đó | HTTP 401 trên kết nối/request mới; repo chưa có API quản trị đổi status |
| EDGE-007 | Upload avatar file > 10 MiB bằng field `file` | 400; controller hiện trả Bad Request khi MaxBytesReader từ chối |
| EDGE-008 | Hai buyer cùng mua hết một sản phẩm có kho 1 | Chỉ một đơn 201, đơn còn lại 409; Collection Runner chạy tuần tự không chứng minh được concurrency |

Với EDGE-008, dùng hai tab/client gửi đồng thời hoặc bài integration Go có sẵn;
không dùng nhiều iteration tuần tự của Runner để kết luận chống oversell.
