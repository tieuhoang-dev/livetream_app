# Realtime qua WebSocket dùng chung

Mọi kết nối ứng dụng dùng `ws://localhost:8080/api/ws` (`wss://` khi HTTPS).
Không dùng `/api/auth/ws` hoặc `/api/livestreams/ws` nữa. Không truyền JWT vào
URL. Khách xem danh sách/phòng công khai; gửi frame authenticate để dùng chức
năng riêng tư. WebSocket không mang video: camera/âm thanh vẫn qua LiveKit.

```json
{"type":"authenticate","request_id":"auth-1","payload":{"token":"<JWT từ login>"}}
```

Mỗi lệnh cần request_id riêng (tối đa 64 ký tự). Response:

```json
{"type":"result","request_id":"auth-1","data":{"user_id":"<UUID>"}}
{"type":"result","request_id":"bad-1","error":"unauthorized","status":401}
```

Subscribe cần thêm subscription_id riêng. Snapshot và push dùng subscription_id
để FE bỏ qua sự kiện của phòng/bộ lọc đã rời. Unsubscribe:

```json
{"type":"unsubscribe","request_id":"leave-1","subscription_id":"room-1","payload":{}}
```

## Các lệnh

| Type | Payload | Quyền / kết quả |
| --- | --- | --- |
| lives.list | q, status, sort, seller_id, product_id, page, page_size | Công khai; một trang danh sách |
| lives.subscribe | Như lives.list | Công khai; snapshot tự cập nhật |
| live.get | livestream_id | Công khai; trạng thái kể cả phiên đã kết thúc |
| live.subscribe | livestream_id, page, page_size | Công khai; live, products, shipping_origin |
| live.create | title | JWT; chủ phiên từ người đã xác thực |
| live.logout | {} | JWT; kết thúc tất cả phiên đang mở của chính tài khoản trước khi logout |
| live.products.create | livestream_id, product, discount_price, stock_quantity, is_pinned | Chủ phiên; tạo catalog + cấp kho nguyên tử |
| live.end | livestream_id | Chủ phiên; end và media_closed |
| live.products.add | livestream_id, product_id, discount_price, stock_quantity, is_pinned | Chủ phiên/sản phẩm |
| live.products.remove | livestream_id, product_id | Chủ phiên |
| live.media | livestream_id, role: publisher/viewer | JWT; chỉ chủ phiên được phát |
| comments.subscribe | livestream_id, after (chuỗi số, mặc định "0") | JWT; lịch sử và push bình luận |
| comments.send | livestream_id, content | JWT; 1–300 ký tự, chỉ live đang mở |
| orders.subscribe | {} | JWT; chỉ thông báo đơn của buyer/seller hiện tại |
| shipping.status | {} | JWT; configured/environment/quote_only |
| shipping.provinces | {} | JWT; danh mục tỉnh GHN |
| shipping.districts | province_id | JWT; danh mục quận/huyện |
| shipping.wards | district_id | JWT; danh mục phường/xã |
| shipping.stations.nearby | latitude, longitude, carrier_province_id, carrier_district_id | JWT; danh sách theo khoảng cách và selected do BE chọn |
| shipping.pickup.get | livestream_id | Chủ phiên; địa chỉ lấy hàng riêng tư |
| shipping.pickup.save | livestream_id, pickup_address | Chủ phiên; địa chỉ chuẩn đã xác minh |
| shipping.origin.subscribe | livestream_id | JWT; báo thay đổi nơi gửi/kiện để lấy lại quote |
| shipping.quote | product_id, quantity, payment_method, delivery, livestream_id nếu có | JWT; báo phí GHN, không tạo vận đơn |
| chat.subscribe | {} | JWT; chỉ tin nhắn riêng của tài khoản hiện tại |
| chat.send | receiver_id, message_type, content, temp_id | JWT; đưa tin riêng vào Redis Stream |

Ví dụ phòng và bình luận:

```json
{"type":"live.subscribe","request_id":"room-req","subscription_id":"room-1","payload":{"livestream_id":"<UUID>","page":1,"page_size":12}}
{"type":"comments.subscribe","request_id":"comments-req","subscription_id":"comments-1","payload":{"livestream_id":"<UUID>","after":"0"}}
{"type":"comments.send","request_id":"send-1","payload":{"livestream_id":"<UUID>","content":"Còn hàng không?"}}
```

Sự kiện:

```json
{"type":"event","subscription_id":"comments-1","event":"snapshot","data":{"data":[],"next_cursor":"0","has_more":false}}
{"type":"event","subscription_id":"comments-1","event":"comment.created","data":{"id":"123","username":"Buyer","content":"Còn hàng không?","created_at":"2026-11-18T00:00:00Z"}}
```

## Lưu trữ, quyền và kết nối lại

Bình luận live chỉ lưu MongoDB, SQL giữ quan hệ/trạng thái phiên. Mongo xác
nhận lưu trước khi backend gửi ACK/push. Không có HTTP GET/POST comments và
không polling. ID/cursor là chuỗi để không mất độ chính xác trong JavaScript.
Subscribe đăng ký phòng trước khi đọc lịch sử; FE gộp theo ID. Khi reconnect,
FE đọc tiếp từ checkpoint của lịch sử đã nhận, có phân trang, và không làm
mất/gửi lặp tin chỉ vì snapshot và push cùng chứa tin đó.

Tin nhắn riêng có service riêng: Redis nhận thì ACK trạng thái queued; worker
chuyển sang Mongo. Count trong XREADGROUP là số tối đa mỗi lần đọc, không yêu
cầu tích đủ 100 tin. Luồng này không xử lý comment live. Import SQL comment cũ
chỉ chạy lúc khởi động; không phải đường đọc/ghi comment hiện tại.

Thông báo đơn chỉ tới buyer và seller liên quan. Snapshot phòng không có điện
thoại, địa chỉ nhà hay GPS người bán/người mua. Địa chỉ bưu cục là dữ liệu công
khai. FE chỉ hiện địa chỉ người mua trong checkout/đơn mà họ có quyền xem.
Nhiều tab cùng tài khoản có kết nối độc lập; một tab không đá tab khác.

FE chỉ tự nối lại và đăng ký lại subscription. Không tự gửi lại lệnh ghi khi
mất ACK: server có thể đã ghi thành công. Đặt hàng HTTP vẫn dùng request_id
để chống trùng. Bảo mật/quyền và lỗi domain được dùng chung giữa HTTP và WS.

## Tổ chức mã

- `internal/realtime/service.go`: root service, quản lý socket, heartbeat,
  request_id, backpressure, giới hạn công việc/subscription và shutdown.
- `controller/websocket/`: adapter lives/comments/shipping/chat; chỉ đăng ký
  lệnh và gọi business service, không có vòng read/write riêng.
- `controller/services/realtime_events.go`: các business service phát sự kiện
  sau khi transaction thành công; dùng cùng root service ở config.Realtime.
- `controller/services/livestream_comments.go`: Mongo comment live.
- `controller/services/direct_chat*.go`: tin nhắn riêng và queue/worker.
- `controller/services/livestream_comment_migration.go`: import lịch sử cũ.
- `controller/http/`: HTTP auth, sản phẩm, đơn hàng và route tương thích.
- FE `src/realtime/client.js`: một socket theo phiên, correlation, subscribe,
  reconnect; component không tự mở socket hay tạo vòng polling.

Root bus hiện nằm trong một process Go. Chạy nhiều instance cần thêm adapter
pub/sub chung; không coi Redis queue tin riêng là bus fan-out liên instance.
Thư viện root không chứa truy vấn SQL/Mongo hay logic GHN.

Kiểm thử Go dùng MySQL/Mongo cục bộ và httptest; test FE giả lập server WS.
GHN chỉ đọc danh mục/báo phí, chưa kiểm thử bằng tài khoản GHN thật.


## Vòng đời live và mất tín hiệu

`live.create` thực sự chạy qua WebSocket; SQL lưu trạng thái bền vững, WS
trả kết quả và đẩy snapshot mới cho các client đăng ký `lives.subscribe` và
`live.subscribe`. Không có HTTP API tạo live. Tạo/kết thúc/gỡ sản phẩm chỉ
publish sau khi SQL commit; filter/phân trang được áp lại cho từng subscriber.

`controller/services/live_lifecycle.go` chạy ở backend, kiểm tra LiveKit
ListParticipants mỗi 15 giây, tối đa 4 request đồng thời. Chỉ participant
`seller-<seller_id>` ở trạng thái ACTIVE và có track CAMERA/MICROPHONE mới gia
hạn. Mute có chủ đích vẫn được chấp nhận; viewer/chat WS không giữ live sống.
Phiên mới có 90 giây để bắt đầu phát. Mất publisher khoảng 90 giây (cộng tối
đa một chu kỳ kiểm tra trong điều kiện bình thường) thì kết thúc, trả hàng
chưa bán về kho, đóng room và đẩy lại list/phòng. LiveKit lỗi kéo dài cũng
không duy trì phiên vô hạn. Thời gian dựa trên lần LiveKit xác nhận publisher,
không đo trực tiếp từng gói RTP ở backend Go.

Deadline/last_signal lưu trong SQL, không reset khi backend khởi động lại.
Dòng legacy chưa có deadline nhận một lần grace rồi lưu lại. Nhiều backend
cần bus fan-out chung như lưu ý phía trên. Worker recheck deadline dưới khóa
SQL trước khi end; end đồng thời/thử lại không trả kho hai lần hoặc đổi ended_at.

FE logout rời video ngay, gửi `live.logout` trước khi xóa token/đóng socket.
Nếu request không tới backend, việc ngắt media vẫn dẫn tới timeout. Room API
chỉ dùng ListParticipants/DeleteRoom; GHN vẫn chỉ danh mục và báo phí.

Tạo sản phẩm trong live:

```json
{"type":"live.products.create","request_id":"new-product-1","payload":{"livestream_id":"<UUID>","product":{"name":"Áo","price":100000,"stock_quantity":10,"image_url":"/uploads/products/<file>.png","parcel":{"weight_grams":500,"length_cm":20,"width_cm":15,"height_cm":10}},"discount_price":80000,"stock_quantity":5,"is_pinned":true}}
```

Upload ảnh nhị phân dùng HTTP multipart `/api/products/images`, sau đó chỉ
truyền URL lưu trữ trong frame. Không gửi file base64 vào WebSocket.
