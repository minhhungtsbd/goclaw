# Chính sách đổi, hủy và hoàn tiền

## Nguyên tắc bắt buộc

Trước khi xử lý phải xác định:

1. Dịch vụ là Proxy hay VPS.
2. Tên gói chính xác từ trang quản lý hoặc ảnh đơn hàng.
3. Khách đổi/hủy theo nhu cầu hay đang báo lỗi dịch vụ.

Khi khách đã gửi IP, dùng `cloudmini_proxy_check` với `service_info` trước để xác định loại dịch vụ, tên gói và hạn. Chỉ xin ảnh/tên gói khi không có IP hoặc API không có kết quả; không hỏi lại dữ liệu API và hội thoại đã xác định.

## Khách báo hủy trên web không thành công

Không chuyển Admin ngay khi chỉ thấy ảnh hoặc thông báo “dịch vụ này không thể hủy”. Xử lý theo thứ tự:

1. Xin IP nếu hội thoại chưa có IP.
2. Nếu chưa có email Cloudmini, xin email và không nói yêu cầu đang chờ xử lý.
3. Gọi lại `cloudmini_proxy_check(operation="service_info", account_email="...")`; chỉ dùng dữ liệu chính sách khi `account_email_matches == true`.
4. Đối chiếu `plan` và `cancellation_policy`:
   - `not_supported`: trả lời theo chính sách gói, không tạo Admin handoff. Với Residential Static, giải thích rõ không hỗ trợ hủy/hoàn phần thời gian còn lại theo nhu cầu; thông báo web là hành vi phù hợp với chính sách.
   - `self_service`: gói được phép tự hủy. Nếu khách đã xác minh đúng email nhưng nút hủy vẫn báo lỗi, chuyển Admin kiểm tra thao tác thủ công; không cam kết ETA hoặc mức hoàn.
   - `review_required`: chỉ chuyển sau khi đối chiếu chính sách hiện hành hoặc khi cần thao tác nội bộ.

Không áp dụng chính sách VPS cho Proxy hoặc ngược lại. “Acc bị die” thường là tài khoản bên thứ ba bị khóa, không đồng nghĩa Proxy lỗi.

- Không yêu cầu Check Live nếu khách chỉ đổi/hủy vì acc bên thứ ba die.
- Chỉ Check Live khi Proxy không kết nối, không hoạt động hoặc nghi lỗi dịch vụ.
- Nếu lỗi Cloudmini/NCC được xác nhận, chuyển xử lý theo kết luận Admin/Kỹ thuật.
- Không tự xác nhận lỗi, đổi miễn phí, mức hoàn hoặc ETA khi chưa có kết quả.

### NGOẠI LỆ ĐẶC BIỆT: Tài khoản Reseller
Nếu email tài khoản Cloudmini của khách nằm trong danh sách **Reseller đặc biệt** (xem chi tiết danh sách email tại `reseller.md`), các quy tắc giới hạn đổi/hủy thông thường (như phí đổi, hủy hoàn tiền 80%, không đổi của gói Static/NN...) sẽ **hoàn toàn được bỏ qua** đối với **tất cả các gói Proxy và VPS**.
- **Quy tắc bắt buộc:** Mọi yêu cầu liên quan đến **Hủy, Đổi, Khôi phục, Gia hạn** từ các email này phải **luôn chuyển thẳng cho Admin** (gọi `escalate_to_admin`) mà không áp dụng quy định hiện tại của Cloudmini.

## Proxy IP tĩnh

### PrivateV4 — 50.000đ/tháng/IP

- Khách tự đổi hoặc hủy tại **Quản lý Proxy**.
- Đổi theo nhu cầu: chọn IP → **Thao tác → Thay thế IP**, phí **20.000đ/IP**.
- Hủy: khách tự thao tác tại trang quản lý, hoàn **80% giá trị thời gian còn lại** vào số dư Cloudmini.
- Tài khoản bên thứ ba bị khóa, khách muốn đổi mục đích sử dụng hoặc đơn giản muốn IP mới đều không phải lỗi Proxy, nhưng vẫn là nhu cầu thay IP có phí hợp lệ của PrivateV4. Không được từ chối quyền thay IP chỉ vì lý do này.
- Khi khách chỉ hỏi chính sách và đã nêu rõ PrivateV4, trả lời trực tiếp theo các quy định trên. Chỉ xin IP/email nếu cần kiểm tra dịch vụ cụ thể hoặc khách báo thao tác trên web bị lỗi.

Mẫu phản hồi:

> Dạ, gói PrivateV4 hỗ trợ anh/chị tự thay IP tại Quản lý Proxy → chọn IP → Thao tác → Thay thế IP, phí 20.000đ/IP ạ. Nếu không muốn tiếp tục sử dụng, anh/chị cũng có thể tự hủy và được hoàn 80% giá trị thời gian còn lại vào số dư Cloudmini.

### BudgetV4 — 40.000đ/tháng/IP

- Không hủy/hoàn theo nhu cầu.
- Admin có thể đổi IP thủ công miễn phí.
- Không áp dụng nếu đổi lặp lại hoặc có dấu hiệu lạm dụng.
- Agent ghi nhận và chuyển Admin, không cam kết chắc chắn được duyệt.

### PrivateV6 — 5.000đ/tháng/IP

- Không hủy/hoàn theo nhu cầu.
- Nếu xác định lỗi Cloudmini/NCC, Admin có thể đổi hoặc hoàn thủ công.
- Không tự xác nhận điều kiện khi chưa kiểm tra.

### Residential Static — 120.000đ/tháng/IP

- Không đổi IP theo nhu cầu.
- Không hủy/hoàn thời gian còn lại theo nhu cầu.
- Không nêu phí đổi 20.000đ vì có thể khiến khách hiểu nhầm gói vẫn đổi được.

Mẫu phản hồi:

> Dạ, các IP này thuộc gói Residential Static. Theo lưu ý của gói khi đặt hàng, gói này không hỗ trợ đổi IP hoặc hủy/hoàn phần thời gian còn lại theo nhu cầu ạ. Các IP vẫn có thể tiếp tục sử dụng đến hết hạn.

### Budget Residential Static — 80.000đ/tháng/IP

- Không hủy/hoàn theo nhu cầu.
- Admin có thể đổi IP thủ công miễn phí.
- Không hỗ trợ đổi nhiều lần hoặc lạm dụng.
- Agent ghi nhận và chuyển Admin, không cam kết chắc chắn được duyệt.

## Proxy xoay

### Residential VN — 120.000đ/tháng/proxy

- Khách tự đổi IP miễn phí tại Quản lý Proxy.
- Không hủy/hoàn trong thời gian sử dụng.
- Nếu lỗi Cloudmini/NCC được xác nhận, Admin có thể thay Proxy hoặc hoàn thủ công sau kiểm tra.

### Rotating Residential — 55.000đ/GB

- Không hủy/hoàn trong quá trình sử dụng.
- Hết dung lượng có thể mua thêm GB.
- Khách tự cấu hình quốc gia, thành phố, xoay 5–30 phút hoặc random mỗi request tại Quản lý Rotating Proxy.
- Có IP dân cư xoay tại hơn 180 quốc gia.
- Dung lượng có hạn tối đa 120 ngày từ khi đăng ký thành công; khuyến nghị dùng hết trước hạn.

## VPS

### Custom, Mini, Promo và YT

- Có thể đổi VPS/IP hoặc hủy trong thời gian sử dụng.
- Khách tự thao tác tại Quản lý VPS.
- Phí đổi: 20.000đ/IP.
- Hủy: hoàn 80% giá trị thời gian còn lại vào số dư Cloudmini.

YT áp dụng chính sách tương tự Custom, Mini và Promo.

### NN

- Bao gồm NN1, NN2, NN3, NN4, NN5 và NN6.
- Không đổi IP/VPS trong thời gian sử dụng.
- Không hủy/hoàn theo nhu cầu.

## Luồng trả lời yêu cầu đổi/hủy Proxy

Nếu chưa rõ gói:

> Dạ, anh/chị gửi em ảnh chi tiết gói tại Quản lý Proxy hoặc tên gói giúp em để em kiểm tra đúng chính sách đổi/hủy ạ.

Sau khi xác định:

- Residential Static: thông báo không đổi/hủy theo nhu cầu; không áp dụng cho Budget Residential Static.
- PrivateV4: hướng dẫn tự đổi hoặc hủy.
- BudgetV4/Budget Residential Static: ghi nhận để Admin xem xét đổi thủ công, không cam kết nếu lặp lại/lạm dụng.
- PrivateV6: không hủy/hoàn theo nhu cầu; có lỗi thực tế thì chuyển kiểm tra.
- Residential VN: hướng dẫn tự đổi miễn phí.
- Rotating Residential: không hủy/hoàn; hướng dẫn mua thêm/cấu hình dung lượng khi phù hợp.
- Lỗi đã xác nhận từ Cloudmini/NCC: chuyển Admin/Kỹ thuật theo kết quả.
