# ShippingService — chỉ lấy báo giá

Dịch vụ này đọc danh mục khu vực và lấy báo phí GHN. Không có chức năng tạo
vận đơn, đặt lịch lấy hàng, cập nhật/hủy vận đơn hay gọi webhook giao hàng.
Đơn mua của ứng dụng vẫn là đơn SQL nội bộ; lưu đơn không gửi hàng qua GHN.

## Cấu hình

Lấy tài khoản/token/shop thử nghiệm qua https://developer.ghn.dev. Điền trong
`.env` backend trên máy của bạn, rồi khởi động lại backend:

```dotenv
GHN_ENV=staging
GHN_TOKEN=<token thử nghiệm của bạn>
GHN_SHOP_ID=<shop thử nghiệm thuộc token đó>
```

Không ghi Token trong FE, URL, báo cáo test hoặc commit Git. Khi thiếu/sai
cấu hình, checkout báo `shipping_provider_not_configured`, không tự gán phí 0.
Mặc định là staging. `GHN_ENV=production` chỉ đổi môi trường **báo giá**, vẫn
không mở chức năng gửi hàng; giữ staging trong giai đoạn thử nghiệm.

Client chỉ cho phép chính xác:

- GET `/shiip/public-api/master-data/province`
- GET `/shiip/public-api/master-data/district`
- GET `/shiip/public-api/master-data/ward`
- GET `/shiip/public-api/v2/station/get`
- POST `/shiip/public-api/v2/shipping-order/fee`

Lớp HTTP từ chối đường dẫn/phương thức khác, host khác và mọi redirect trước
khi có thể gửi tiếp request. Không hỗ trợ URL tùy ý qua biến môi trường.
Các bài unit test dùng server `httptest` cục bộ, không gọi tài khoản GHN thật.

## Dữ liệu và luồng

1. Người bán sửa/tạo sản phẩm, điền `parcel`: `weight_grams`, `length_cm`,
   `width_cm`, `height_cm`. Đây là kiện đóng gói cho một đơn vị sản phẩm.
   Sản phẩm cũ chưa có dữ liệu báo `shipping_product_incomplete` cho đến khi
   chủ shop bổ sung. Không dùng khối lượng hoặc giá/phí từ người mua tự khai.
2. Người bán xác nhận nơi lấy hàng ở shop hoặc phiên live. Không nhập phí.
3. Người mua chọn tỉnh/thành, khu vực GHN, quận/huyện và phường/xã hãng hỗ trợ.
   Danh mục dùng mã GHN thật; không gửi mã tỉnh từ API khác sang GHN.
4. Backend kiểm tra mã tỉnh/quận/phường thuộc đúng nhau, tính kiện từ SQL rồi
   gọi API fee. Chỉ gửi mã khu vực, quy cách kiện, giá trị khai báo và COD;
   không gửi tên, điện thoại, số nhà hay mã đơn của ứng dụng.
5. FE hiện tiền hàng/phí/tổng và nhãn staging. Khi tạo đơn nội bộ, backend
   lấy báo giá mới trước transaction SQL. Trong transaction, kiểm tra lại
   giá/kiện/nơi lấy/tồn kho. Thay đổi báo giá hoặc dữ liệu trả 409
   `checkout_changed`, không trừ kho. Không giữ khóa SQL trong lúc chờ GHN.
6. Lưu snapshot địa chỉ và `delivery.shipping` (provider, environment,
   quote_only=true, kiện, phí, service type, thời điểm báo giá). Retry cùng
   request_id trả đơn đã lưu mà không cần gọi hãng lại.

API phí GHN hiện yêu cầu mã quận/phường theo danh mục cũ. App vẫn dùng danh
mục 34 tỉnh, ánh xạ các vùng cũ về tỉnh hiện tại và yêu cầu chọn khu vực hãng
để phân biệt sau sáp nhập. GPS hỗ trợ tìm địa chỉ, không đoán mã quận/phường
khi không ánh xạ chắc chắn. Khi chưa tải được danh mục, địa chỉ vẫn nhập được
nhưng checkout chưa xác nhận đơn với một báo giá giả.

Với nhiều đơn vị cùng sản phẩm, cộng khối lượng và xếp chồng chiều cao; dài/rộng
giữ theo kiện một đơn vị. Trả lỗi nếu kiện vượt giới hạn hỗ trợ (1.000.000 g,
300 cm mỗi chiều). Đây là quy cách kiện dự tính, cần sửa metadata nếu cách đóng
gói thực tế khác. Service type 2 dưới 20 kg, type 5 từ 20 kg; phí theo GHN.
COD dùng giá trị tiền hàng khi chọn COD; chuyển khoản dùng COD=0. Phí hãng và
phụ phí được lấy nguyên từ response, không lấy cấu hình phí cũ của shop.

## API ứng dụng

Các API sau cần JWT người dùng, không nhận Token GHN từ client:

- GET `/api/shipping/status`: provider/environment/configured/quote_only.
- GET `/api/shipping/provinces`: các vùng GHN kèm province_code hiện tại.
- GET `/api/shipping/districts?province_id=<GHN id>`.
- GET `/api/shipping/wards?district_id=<GHN id>`.
- POST `/api/orders/quote`: product_id, quantity, payment_method,
  livestream_id nếu có, delivery gồm province_code và carrier_province_id,
  carrier_district_id, carrier_ward_code.

Tên/điện thoại/đường chỉ bắt buộc khi tạo đơn. Backend lưu tên phường/xã chuẩn
từ danh mục hãng vào snapshot. Phí có thể đổi theo phương thức COD, vì vậy FE
báo giá lại khi đổi phương thức thanh toán. GET/PUT shipping-settings và
pickup vẫn lưu địa chỉ, các trường phí shop cũ không còn được sử dụng.

Tài liệu chính thức đã đối chiếu: https://developer.ghn.vn/en/docs/order/calculate-fee
và https://developer.ghn.vn/en/docs/getting-started/overview.

## Bưu cục gần vị trí phát

Người bán có thể mở **Cài đặt giao hàng** trong live (hoặc bước **Bắt đầu
phát**), lấy GPS, rồi bấm **Tìm bưu cục GHN gần vị trí này**. Danh sách có
khoảng cách đường chim bay, ghim trên bản đồ và liên kết chỉ đường. Không tự
chọn bưu cục đầu tiên; người bán xác nhận bưu cục mình sẽ tự mang hàng đến.

GHN Get Station bắt buộc quận/huyện. Reverse geocode tự điền mã khu vực nếu
tên tỉnh/quận/phường khớp duy nhất với danh mục GHN. Thiếu tên quận hoặc dữ
liệu sau sáp nhập chưa khớp thì chọn khu vực thủ công. Tra cứu giới hạn theo
quận/huyện đã chọn, không bảo đảm là bưu cục gần nhất ngoài khu vực đó. Có thể
đổi sang quận lân cận để tìm tiếp. Khoảng cách không phải quãng đường chạy xe
và không được dùng làm công thức tính phí ship.

API ứng dụng (cần JWT):

```http
POST /api/shipping/stations/nearby
Content-Type: application/json

{"latitude":21.028511,"longitude":105.804817,"carrier_province_id":<GHN province ID>,"carrier_district_id":<GHN district ID>}
```

Response: `data[]` gồm id/name/address, tọa độ/khoảng cách nếu có,
`pickup_address` nếu ánh xạ được tuyến; kèm district, environment, complete,
quote_only=true. `complete=false` khi chưa đọc hết danh mục (tối đa 5 trang
100 dòng, dừng nếu hãng lặp trang). Không nhận token GHN từ FE. Tọa độ GPS
chỉ để backend xếp hạng; GHN nhận mã quận, không nhận GPS cá nhân từ API này.

Chọn bưu cục điền địa chỉ, giữ tên/điện thoại người bán. Khi lưu pickup,
backend đọc lại bưu cục theo `station_id` và khu vực, lấy địa chỉ chuẩn từ
GHN; không tin tên/đường/phường do client tự gửi. Bưu cục không ánh xạ chắc
chắn được phường theo danh mục GHN vẫn xem được nhưng không được chọn để
báo phí. Snapshot đơn có station_id/station_name và
`delivery.shipping.origin_type=station_dropoff`. Thay đổi nơi lấy hàng không
đổi snapshot đơn cũ; lưu nơi gửi và đặt mua không rời phòng live.

Fee API được gọi với from_district_id/from_ward_code của bưu cục. Đây là báo
phí theo tuyến từ khu vực bưu cục, chưa phải chốt phí gửi thực tế hay xác nhận
bất kỳ ưu đãi/phụ phí nào của hình thức gửi tại quầy. Không gửi pick_station_id
sang API tạo đơn, không tạo vận đơn, không đặt lịch lấy hàng.

Một số tài khoản GHN yêu cầu quyền tra bưu cục/captcha. HTTP 422 trả lỗi
`shipping_stations_access_required`; liên hệ GHN mở quyền. Giao diện cho nhập
địa chỉ lấy hàng để tiếp tục, không giả dữ liệu bưu cục hay tự vượt captcha.
Lỗi tra bưu cục không làm mất địa chỉ đang nhập. Bài kiểm thử dùng bưu cục và
HTTP fixture giả lập, chưa xác nhận dữ liệu bưu cục của tài khoản GHN thật.

Tài liệu GHN: https://developer.ghn.vn/vi/docs/master-data/get-station.
