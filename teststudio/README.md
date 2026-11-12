# Livestream Test Studio

Giao diện chạy bộ test Livestream API. Nạp sẵn 105 request từ collection của repo.

1. Nhập URL backend và hai tài khoản test khác nhau.
2. Chọn **Đăng ký mới** hoặc **Đã xác thực**; chọn các nhóm cần kiểm tra.
3. Bấm **Bắt đầu chạy**.
4. Đến Verify, Studio giữ nguyên cursor và hiện ô OTP. Nhập mã email rồi
   **Tiếp tục**; Verify và Login chạy tiếp trong cùng phiên, không gửi Register lại.

File upload và tài khoản phụ cho case chưa verify cũng được hỏi khi cần. Live
được tạo/kết thúc tự động bằng WebSocket. Các ID được lấy từ response.

Nếu test chưa đạt, có thể thử lại **riêng bước hiện tại**, tiếp tục bước sau,
hoặc bỏ qua phần còn lại của nhóm. Test bị bỏ qua không được tính là đạt.
Case RESET-007 dành cho provider chưa cấu hình sẽ tự bỏ qua nếu bước gửi OTP
vừa thành công trên cùng môi trường.

## Bản local

Backend cũng phục vụ Studio tại `http://localhost:8080/teststudio/`. Mở địa chỉ
này sau khi cập nhật code và khởi động lại backend; Studio tự dùng cùng origin,
không cần CORS giữa hai trang. Không phải chạy thêm server giao diện.


Mở Studio qua HTTP server, không mở trực tiếp file HTML:

```sh
node serve.mjs
```

Truy cập `http://127.0.0.1:4173`. Backend Go chạy riêng, mặc định cổng 8080.
Không cần cài package cho giao diện. Trong repo Livestream có thể dùng bản Go:

```sh
go run ./cmd/teststudio
```

Bản hosted HTTPS cần backend truy cập được từ trình duyệt. Nếu backend là
localhost và trình duyệt chặn private-network/mixed-content/CORS, dùng bản
local HTTP hoặc địa chỉ HTTPS của backend. Studio không chuyển credential qua
server hosting; request được gửi trực tiếp từ trình duyệt tới backend đã nhập.

## Dữ liệu và giới hạn

- Chỉ dùng tài khoản/database test: bộ case tạo sản phẩm, đặt/hủy/hoàn đơn,
  sửa profile và thử đổi mật khẩu.
- Credential, token và OTP ở bộ nhớ phiên, không lưu vào localStorage hoặc
  gửi tới dịch vụ hosting. Báo cáo JSON che password/token/OTP.
- Cần Brevo hoạt động để nhận OTP thật. Giao diện không lấy OTP từ database,
  không tạo mã thay server và không bỏ qua xác thực. Nếu provider chưa cấu
  hình, nhóm reset có thể thất bại; hãy bỏ qua nhóm hoặc sửa cấu hình backend.
- Phiên tiếp tục trên cùng trang. Không refresh hoặc đóng tab khi đang chạy;
  trình duyệt sẽ cảnh báo nếu phiên còn hoạt động. Start/Continue không chạy
  lại Register; bắt đầu một phiên mới là thao tác riêng.
- Hỗ trợ script/matcher dùng trong collection đi kèm, không cam kết toàn bộ
  Postman API cho collection tùy ý. Script chạy trong Worker riêng; request
  chỉ được gửi tới origin backend đã cấu hình.
- Media thật và payment gateway chưa được backend tích hợp. Các loại tin chat
  không phải upload media; collection HTTP 105 case chưa bao gồm toàn bộ
  checklist WebSocket/edge case trong tài liệu trước đó.

## Chẩn đoán kết nối

Nút Kiểm tra kết nối kiểm tra cả GET sản phẩm và POST JSON tới login với body
`{}` và header Authorization giả dành riêng cho probe. Controller trả HTTP 400
vì thiếu email/password; probe không tạo tài khoản hoặc gửi OTP. Chỉ GET thành
công chưa đủ để hiện trạng thái sẵn sàng. Probe và runner dùng cùng chế độ
credentials/redirect.

Nếu fetch bị chặn, chi tiết vẫn hiện JSON đã chuẩn bị gửi (mật khẩu được che),
URL, method và lỗi trình duyệt. Không tự retry Register: nếu đã nhận OTP dù
response bị mất, chọn Tiếp tục bước sau để Verify trên cùng phiên.

## Kiểm tra

```sh
node --test tests/runner.test.mjs tests/connection.test.mjs
```

Kiểm tra chờ OTP, resume cùng cursor, nhập sai mã chỉ retry Verify, dừng khi
đang chờ, manual pause và đổi mật khẩu. Suite backend thực là opt-in qua
`STUDIO_REAL_API`, `STUDIO_FIXTURE_HELPER` và tùy chọn `STUDIO_FIXTURE_PYTHON`;
fixture helper chỉ được dùng trong môi trường test riêng, không được giao
trong app. 98/98 case không phụ thuộc gửi email reset đã chạy đạt với backend
thật; OTP xác nhận dùng fixture trong harness, không mô phỏng email đã gửi.
