# Test Livestream App bằng Postman

Bộ test theo `main` commit `7dc3dd0` của `tieuhoang-dev/livetream_app`.
Gồm **105 request HTTP có assertion**, bảng test case CSV, file Environment,
ảnh mẫu và hướng dẫn WebSocket riêng.

Luồng chính cho tài khoản mới: **Đăng ký → lấy OTP → Verify thành công →
Đăng nhập → gọi API có JWT**. Không đăng nhập trước bước Verify trong luồng này.

## 1. Import và cấu hình

1. Giải nén ZIP, mở Postman → **Import**, chọn:
   - `Livestream_App.postman_collection.json`
   - `Livestream_Local.postman_environment.json`
2. Chọn Environment **Livestream App - Local (dien tai khoan test)**.
3. Đặt `base_url`, mặc định `http://localhost:8080`. Khi chạy một HTTP request,
   collection tự cập nhật `ws_url` tương ứng (`http → ws`, `https → wss`).
4. Điền bốn biến cho **hai tài khoản test khác nhau**. Tài khoản mới/chưa xác
   thực phải hoàn tất bước 2 bên dưới trước khi chạy folder 02:

   | Biến | Giá trị |
   | --- | --- |
   | `seller_email` | Email tài khoản bán hàng test |
   | `seller_password` | Mật khẩu tài khoản đó |
   | `buyer_email` | Email tài khoản mua hàng test |
   | `buyer_password` | Mật khẩu tài khoản đó |

`seller` và `buyer` là tên gọi trong bộ test, không yêu cầu role riêng trong DB.
Collection không chứa email, password, JWT hoặc Brevo key thật. Không cần nhập
JWT thủ công: request login tự lưu token và lấy user ID từ payload JWT.

Backend cần MySQL, MongoDB, Redis như trong repo. Nếu chạy từ `.env`, lưu ý cấu
hình Redis hiện được đọc khi package khởi tạo: export `REDIS_ADDR` và
`REDIS_PASSWORD` trước khi chạy. Môi trường kiểm thử đã dùng `GOMAXPROCS=8` để
pool Redis mặc định đủ phục vụ 50 worker blocking; đây là cấu hình test, không
phải kết quả đo khả năng chịu tải production.

## 2. Nếu chưa có tài khoản

Chạy **từng request** trong folder `01 - Setup tai khoan (THU CONG)`:

1. Điền email bạn kiểm soát và password cho seller/buyer.
2. `AUTH-001`: đăng ký seller, mong đợi 200.
3. Lấy OTP từ email, mở `AUTH-002` → **Body → raw → JSON**. Nhập trực tiếp
   OTP vào trường `code` đang để trống, rồi bấm **Send**, mong đợi 200:

   ```json
   {
     "email": "{{seller_email}}",
     "code": ""
   }
   ```

   Ví dụ email có mã `012345` thì sửa thành `"code": "012345"`, giữ dấu nháy
   để không mất số 0 đầu. Script không tạo hay ghi đè mã bạn nhập.
4. Chỉ sau khi Verify thành công, chạy `AUTH-003` Login seller, mong đợi 200;
   token và user ID được lưu tự động.
5. Làm tương tự cho buyer: `AUTH-004` đăng ký → mở Body của `AUTH-005`, nhập
   OTP vào `code` và Send Verify → `AUTH-006` Login.

### Dùng Runner và dừng để nhập OTP

Collection đã thêm `pm.execution.setNextRequest(null)` sau mỗi Register thành
công. Lệnh này **kết thúc lượt chạy hiện tại**, không phải pause/resume và
không hiện popup OTP. Bạn có thể dùng nút Send như trên hoặc chạy theo 3 lượt:

1. Run folder 01, bật **Keep variable values**. Runner chạy `AUTH-001` rồi dừng
   thành công, chưa gửi Verify hay Login.
2. Nhập OTP seller vào Body `AUTH-002`, bấm **Save / Ctrl+S**. Mở lượt Run mới,
   bỏ chọn `AUTH-001`, chọn các request từ `AUTH-002` trở đi. Runner chạy Verify
   seller → Login seller → Register buyer (`AUTH-004`) rồi dừng để nhập OTP buyer.
3. Nhập OTP buyer vào Body `AUTH-005`, Save. Mở lượt Run mới chỉ chọn
   `AUTH-005` và `AUTH-006`. Verify và Login buyer thành công thì chuyển sang
   folder 02 để chạy các API nghiệp vụ.

Không bấm Run lại từ đầu vì Register đã chạy với email đó sẽ trả 409. OTP
phải được lấy từ email thật; script không tạo mã hay đọc hộp thư. Nếu `code`
còn trống, pre-request báo rõ cần nhập mã. Không cần biến Environment cho OTP.

Folder 01 chỉ có luồng thành công; cả 6 request đều mong đợi HTTP 200. Case
đăng nhập khi chưa verify bị chặn được tách riêng ở folder 07, dùng tài khoản
test khác, không xen vào các bước trên.

Username được tạo tự động nếu bạn chưa nhập. Nếu email đã đăng ký, register
trả 409: dùng tài khoản khác hoặc bỏ qua đăng ký khi tài khoản đã verified.
OTP hết hạn sau 15 phút; request Verify thành công phải được gửi trước khi hết
hạn. Không chạy lại Verify thành công: backend trả 400 cho user đã verified.

Nếu chưa có `BREVO_API_KEY`/`FROM_EMAIL`, bạn không nhận được OTP thực. Hãy dùng
hai tài khoản test đã được chuẩn bị/xác thực trong môi trường của mình. Bộ
Postman không bỏ qua hoặc giả lập bước xác thực email của backend.

## 3. Chạy HTTP tự động

Trong Collection Runner, **chỉ chọn folder `02 - HTTP tu dong`**, chạy 1
iteration và giữ thứ tự request. Bật tùy chọn giữ giá trị biến sau khi chạy
(Keep variable values/Persist variables, tùy phiên bản) nếu muốn dùng các token
và ID này cho bước WebSocket tiếp theo.

Folder có **66 request**, tự tạo `run_id` mới, đăng nhập hai tài khoản, lưu ID
sản phẩm/đơn hàng, kiểm tra:

- JWT thiếu/sai; quyền truy cập khi chưa đăng nhập.
- Product create/detail/list, lọc seller, tìm tên, phân trang, input sai.
- Chặn sửa/xóa/điều chỉnh kho sản phẩm của người khác.
- PATCH giá 0, xóa ảnh/mô tả; chặn ghi đè owner hoặc kho qua PATCH.
- Cộng/trừ kho; chặn âm kho, delta 0.
- Đặt đơn → kiểm tra kho → pay → refund → kiểm tra kho trả lại.
- Đặt đơn thứ hai → cancel; chặn pay/cancel/refund lặp hoặc sai trạng thái.
- Không cho tác động vào đơn của tài khoản khác, xóa sản phẩm có lịch sử đơn.
- Cập nhật profile và dọn sản phẩm chưa được sử dụng.

Không Run toàn bộ collection một lượt: các folder khác cần OTP, chọn file hoặc
tạo/kết thúc live thủ công. Thiếu biến bắt buộc sẽ báo lỗi pre-request để bạn
biết bước cần chuẩn bị; không được tính là test đã đạt.

Chạy trên dữ liệu/tài khoản test: bộ test tạo sản phẩm và đơn, thay đổi profile,
thanh toán/hủy/hoàn đơn nội bộ. Mỗi lượt giữ lại sản phẩm đã được OrderItem tham
chiếu và các đơn test vì backend chưa có API xóa đơn. Tên `PM_<run_id>` giúp
nhận diện dữ liệu. Các request điều chỉnh kho/đặt hàng áp dụng thay đổi thật vào
DB test; không tự resend request khi chưa rõ kết quả lần trước.

## 4. Avatar

Sau folder 02, chọn folder `03 - Avatar (chon file)`:

- Đặt Postman Working directory là thư mục giải nén; hoặc mở Body → form-data
  → field `file`, chọn `fixtures/avatar.png` và `fixtures/invalid-avatar.txt`
  trong từng request tương ứng.
- Chạy 4 request theo thứ tự: upload PNG, tải lại ảnh, từ chối TXT, thiếu file.
- Nếu dùng Postman trên web, chọn agent có thể đọc file và gọi được backend
  local; nếu file không resolve, chọn lại file trong UI.

## 5. Live và sản phẩm trong live

Đọc `WEBSOCKET_TEST_CASES.md`. Collection JSON v2.1 này là **HTTP collection**;
các message WebSocket cần mở bằng New → WebSocket Request trong Postman.

1. Sau folder 02, mở hai kết nối WS theo URL seller/buyer trong tài liệu.
2. Seller gửi `create_live`, copy `id` trả về vào Environment `live_id`.
3. Buyer gửi `join_live`, mong đợi `join_live_success`.
4. Chạy folder `04 - San pham live (can live_id)` (18 request): tạo sản phẩm
   riêng, kiểm tra quyền, gắn/cấp kho live, mua theo giá giảm, cancel, pay/refund,
   gỡ sản phẩm và kiểm tra kho gốc trở lại 20.
5. Seller gửi `end_live` với `content` là `live_id`. Copy ID đó vào
   `ended_live_id`.
6. Buyer gửi `join_live` lại: phải nhận `join_live_error`.
7. Chạy folder `05 - Live da ket thuc (can ended_live_id)` (2 request): chặn
   gắn thêm sản phẩm và đặt hàng live đã kết thúc với 409.

Không có API HTTP tạo live, nên không gọi `POST /api/livestreams`.

## 6. Reset mật khẩu

Folder `06 - Reset mat khau (THU CONG)` phải chạy từng bước, trên tài khoản test
buyer. Các case trong folder có điều kiện khác nhau, không Run cả folder.

1. `RESET-001`: khi Brevo đã cấu hình, yêu cầu OTP, mong đợi 200. Nếu dùng
   Runner, lượt chạy sẽ kết thúc ngay sau request này để bạn nhập OTP.
2. Đặt `buyer_new_password` khác mật khẩu cũ trong Environment. Mở
   `RESET-002` → Body → raw → JSON, nhập OTP từ email vào trường `code` đang
   trống, rồi Send. Không cần nhập biến Environment cho OTP reset.
   Nếu dùng Runner, Save Body và mở lượt mới từ `RESET-002`, bỏ chọn
   `RESET-001`; không chạy case `RESET-007` dành cho môi trường thiếu Brevo.
3. `RESET-002`: xác nhận reset, mong đợi 200. Script tự lưu mật khẩu cũ vào
   `buyer_previous_password`, rồi cập nhật `buyer_password` trong Environment.
4. `RESET-003`: login mật khẩu cũ trả 400; `RESET-004`: mật khẩu mới trả 200,
   cập nhật `buyer_token`.
5. `RESET-005`: tự lấy mã vừa xác nhận từ `buyer_used_reset_code` để thử dùng
   lại; phải trả `400 otp_expired` (mã đã bị xóa phía server).
6. `RESET-006`: email không tồn tại trả 404.
7. `RESET-007`: **chỉ chạy khi Brevo chưa cấu hình/gửi thất bại**, mong đợi
   `502 email_delivery_failed`. Không chạy giữa bước xin OTP và xác nhận OTP:
   request reset mới sẽ thay thế mã đang chờ.

OTP sai/hết hạn, tài khoản inactive và file quá 10 MB cần fixture/trạng thái
riêng; xem bảng bổ sung trong tài liệu WebSocket. Không coi 502 là thành công
của luồng gửi email. Pay/refund trong app hiện chỉ đổi trạng thái nội bộ, không
chứng minh đã thu hoặc hoàn tiền qua ngân hàng.

## 7. Case âm: đăng nhập khi chưa verify

Folder `07 - Negative auth (CHUA verify)` là bài kiểm tra lỗi độc lập, không
phải bước trong luồng chính:

1. Điền `unverified_email` mới và `unverified_password`, không dùng email
   seller/buyer đã xác thực.
2. `NEG-001`: đăng ký tài khoản test riêng, mong đợi 200. Cố ý không Verify
   tài khoản này.
3. `NEG-002`: thử login tài khoản chưa Verify, mong đợi `400 User not verified`.

Không cần chạy folder 07 để sử dụng hoặc kiểm thử các API nghiệp vụ. Khi chạy
lại folder này, dùng email test mới vì backend không có API xóa tài khoản.

## 8. Chạy bằng Newman (tùy chọn)

```sh
newman run Livestream_App.postman_collection.json \
  -e my-private-local.postman_environment.json \
  --folder "02 - HTTP tu dong" \
  --working-dir . --bail failure
```

Sao chép Environment mẫu thành file riêng và điền tài khoản trước khi chạy.
Muốn chạy tiếp folder live/avatar bằng CLI, dùng `--export-environment` rồi nạp
file xuất ở lần tiếp theo. File xuất chứa token/password của bạn, giữ riêng.

`HTTP_TEST_CASES.csv` mở được bằng Excel/Google Sheets, gồm ID, điều kiện, body,
HTTP status và mã lỗi mong đợi cho cả 105 request. `VALIDATION.md` ghi rõ những
folder đã chạy xác minh và các phần còn cần OTP thực.
