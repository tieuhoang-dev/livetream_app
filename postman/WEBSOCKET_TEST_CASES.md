# Test WebSocket trong Postman

Kết nối `{{ws_url}}/api/ws` cho từng tab seller/buyer. Không gắn JWT trong URL.
Tab công khai đọc danh sách/phòng. Chức năng riêng tư cần gửi authenticate:

```json
{"type":"authenticate","request_id":"auth-seller","payload":{"token":"{{seller_token}}"}}
```

Đổi token ở tab buyer. Response là result với request_id tương ứng;
subscribe nhận thêm event/snapshot theo subscription_id. Payload mẫu nằm trong
websocket-messages.json. Đây là thư viện copy payload, không phải HTTP collection.

| ID | Kiểm tra | Mong đợi |
| --- | --- | --- |
| WS-001 | Tab chưa auth gửi comments.send/live.create | result error=unauthorized, status=401 |
| WS-002 | authenticate với token sai/hết hạn/inactive | 401, không thực thi lệnh ghi |
| WS-003 | Seller authenticate đúng | data.user_id đúng; không có token trong URL |
| WS-004 | Guest lives.subscribe; đổi filter/page | Snapshot công khai, bộ lọc hợp lệ; invalid_query cho filter sai |
| WS-005 | Seller live.create | data.id, status=start; lưu live_id; ended_at=NULL trong SQL |
| WS-006 | Buyer live.subscribe | Snapshot live/products; tự cập nhật khi đổi kho/kết thúc; không có GPS/điện thoại cá nhân |
| WS-007 | Buyer comments.subscribe | Lịch sử Mongo/cursor dạng chuỗi; chưa có tin trả data=[] |
| WS-008 | Buyer comments.send; thêm sender_id/username giả | Server dùng tài khoản đã auth; Mongo đã lưu trước ACK; phòng nhận push ngay |
| WS-009 | Buyer live.end live của seller | forbidden/403; trạng thái/kho không đổi |
| WS-010 | Seller live.end | status=end, push tới phòng/list; không trả kho hai lần |
| WS-011 | live.get phiên đã end | Đọc được status=end; comments.send mới nhận 409 |
| WS-012 | Hai tab cùng tài khoản subscribe | Cả hai hoạt động; đóng một tab không đá tab kia |
| WS-013 | shipping.stations.nearby quanh GPS | Xếp đường chim bay trong khu vực; selected là bưu cục hợp lệ gần nhất; có thể cần quyền GHN |
| WS-014 | shipping.pickup.save với use_nearest_station=true | BE tự chọn/xác minh bưu cục, giữ contact; không tạo vận đơn |
| WS-015 | Buyer/seller orders.subscribe, rồi tạo đơn HTTP nội bộ | Chỉ tài khoản liên quan nhận order.changed; tài khoản khác không nhận dù giả user_id |
| WS-016 | Mất mạng rồi reconnect/subscribe lại với cursor | Đọc bù Mongo, FE gộp ID không trùng; không tự gửi lại lệnh ghi mất ACK |
| WS-017 | Unsubscribe phòng rồi đổi live | Không áp dụng event muộn của phòng cũ |
| WS-018 | chat.subscribe + chat.send tin riêng khi Redis/Mongo chạy | ACK queued khi Redis nhận, worker ghi Mongo; không đi vào comment live |
| WS-019 | Checkout địa chỉ người mua + đặt hàng | Hiện Giao đến, phí/tổng; thành công giữ nguyên phòng/video |

GET/POST comments HTTP và các WS URL cũ đã bỏ. HTTP collection còn 104 case,
không dùng HTTP GET để kiểm tra authenticate WebSocket. Studio chuẩn bị phiên
qua protocol mới và không gửi lại live.create khi chưa rõ kết quả.

Báo phí chỉ gọi danh mục/fee GHN. Không dùng create/preview/booking của hãng.
Xem [REALTIME.md](../REALTIME.md) và [SHIPPING.md](../SHIPPING.md) cho payload
đầy đủ, thiết lập staging và giới hạn bưu cục/quyền tài khoản.
