# Kết quả xác minh bộ Postman

Ngày: 12/11/2026. Source backend: `main`, commit `7dc3dd0`.
Runner: Newman 6.2.3. Backend Go chạy thực với MySQL, MongoDB và Redis.

| Folder | Request đã chạy | Assertion đạt | Ghi chú |
| --- | --- | --- | --- |
| 01 - Setup tài khoản | 6/6 | 13 | Từng tài khoản chạy đăng ký → verify → login; OTP fixture, không kiểm tra giao email |
| 02 - HTTP tự động | 66/66 | 126 | Hai tài khoản khác nhau, verified/active; toàn bộ script tự lưu token/ID |
| 03 - Avatar | 4/4 | 7 | Upload PNG mẫu, tải lại, từ chối TXT và thiếu file |
| 04 - Sản phẩm live | 18/18 | 31 | Live được tạo qua WebSocket thật trước khi chạy folder |
| 05 - Live đã kết thúc | 2/2 | 4 | End live qua WebSocket thật; join sau end bị từ chối |
| 06 - Reset mật khẩu | 6/7 | 11 | Xác nhận bằng OTP fixture, login cũ/mới, chống dùng lại, email không tồn tại, provider thiếu cấu hình |
| 07 - Negative auth riêng | 2/2 | 4 | Tài khoản test khác, cố ý chưa verify; login phải bị chặn |
| **Tổng** | **104/105** | **196** | Không có failure trong các request đã chạy |

Case chưa chạy: **RESET-001**, gửi email reset thành công và trả 200, vì môi
trường chưa có Brevo API key. Case này vẫn có trong collection để chạy thủ công
khi đã cấu hình provider. Không coi 502 ở case thiếu cấu hình là bằng chứng
gửi email thành công.

Tài khoản/password/token dùng xác minh không được đưa vào bộ tải về. Environment
mẫu để trống credential; không chứa OTP fixture hoặc khóa JWT của server. Các
hook chuẩn bị OTP fixture chỉ nằm trong harness kiểm thử riêng, không nằm trong
script của collection được giao.

Các request Verify seller/buyer và xác nhận reset có trường `code` trống,
hiển thị trực tiếp trong Body để người dùng nhập OTP rồi Send. Harness xác minh
điền OTP fixture vào một bản sao trong bộ nhớ để mô phỏng thao tác nhập Body;
file collection giao vẫn để trống và không tạo OTP hay đọc OTP tự động từ DB.
Script chặn mã trống/không đủ 6 số, không ghi đè mã người dùng đã nhập.

Register seller/buyer kết thúc lượt Runner khi trả 200 bằng
`pm.execution.setNextRequest(null)`. Đã xác minh setup theo 3 lượt: 1 request
(Register seller), 3 request (Verify seller, Login seller, Register buyer),
2 request (Verify buyer, Login buyer). Runner không gửi Verify/Login khi dừng
đợi OTP. Request gửi OTP reset cũng có lệnh dừng; hành vi control-flow được
kiểm tra riêng bằng script, còn gửi email reset thành công vẫn chưa test do
thiếu Brevo key.

Toàn bộ 105 request đã được kiểm tra cú pháp JavaScript. JSON được Newman đọc
và thực thi; những tên biến response tránh các global legacy của Postman.

`WEBSOCKET_TEST_CASES.md` và `WEBSOCKET_AND_EDGE_CASES.csv` là hướng dẫn kiểm
tra thủ công, không được tính là 15 test WebSocket hoặc 8 edge case tự động đã
chạy bằng Newman. Khi xác minh bộ collection, WebSocket được dùng để tạo live,
kết thúc live và xác nhận join live đã kết thúc trả lỗi. Các giới hạn media,
payment gateway và kiểm tra persistence chat được ghi rõ trong hướng dẫn.
