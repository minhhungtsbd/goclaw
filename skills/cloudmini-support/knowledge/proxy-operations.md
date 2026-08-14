# Vận hành và vòng đời Proxy

## Quản lý

Tại Quản lý Proxy, khách xem IP, Port, Username, Password; thao tác nhiều Proxy; tự Transfer Proxy sang tài khoản khác. Khuyến nghị chủ sở hữu tự chuyển để giảm tranh chấp. Lịch sử thao tác nằm tại Log History.

## Hết hạn, gia hạn và giữ dữ liệu

- Gói 5k và 50k: giữ khoảng 3–5 ngày sau hết hạn.
- Gói 40k, 80k và 120k: xóa ngay khi hết hạn.
- Proxy còn hiển thị trong tài khoản: khách tự gia hạn; Auto Renew chỉ thành công khi đủ số dư.
- Không cam kết IP cũ còn tồn tại sau khi bị xóa.

## Khôi phục Proxy đã xóa / Gia hạn Proxy đã xóa (QUY TRÌNH GATEWAY)

Khi khách yêu cầu khôi phục hoặc gia hạn lại Proxy hết hạn đã bị xóa:

1. **GATE 1 — Xin email tài khoản**: Kiểm tra xem trong cuộc trò chuyện khách đã cung cấp **email tài khoản Cloudmini** chưa. NẾU CHƯA CÓ EMAIL, TUYỆT ĐỐI KHÔNG GỌI TOOL TRA CỨU, KHÔNG KẾT LUẬN TRẠNG THÁI VÀ CẤM NÓI HẠN DÙNG CỦA IP. Phải phản hồi ngay hỏi xin email tài khoản Cloudmini trước.
2. **GATE 2 — Tra cứu IP**: Sau khi ĐÃ CÓ EMAIL KHÁCH HÀNG, gọi tool `cloudmini_proxy_check(operation: "service_info", ip: "<IP>", account_email: "<email_khach>")`.
3. **GATE 3 — Phân loại & Đối chiếu email nội bộ**:
   - **Trường hợp 1 — IP KHÔNG có liên kết dịch vụ active nào (`service_status == "deleted"` hoặc không gắn dịch vụ)**:
     - Thông báo cho khách rằng IP có khả năng khôi phục/gia hạn được.
     - Gọi `escalate_to_admin` để tạo case chuyển Admin xử lý.
     - **Thông báo chắc nịch**: *"Dạ em đã chuyển case sang cho bộ phận Admin/Kỹ thuật chờ xử lý rồi ạ. Thời gian xử lý có thể mất từ vài phút đến 1 giờ, anh/chị vui lòng chờ giúp em nhé!"*
   - **Trường hợp 2 — IP ĐANG gắn với 1 dịch vụ đang hoạt động (`service_status == "active"`)**:
     - **Định danh & Đối chiếu email nội bộ**: So sánh email dịch vụ `user_email` (từ kết quả tool) với email tài khoản khách hàng đã cung cấp:
       - **CÙNG EMAIL**: Báo cho khách biết Proxy IP đó vẫn nằm trong tài khoản của khách và đang ở trạng thái hoạt động (nêu hạn sử dụng). Hướng dẫn khách: *"Anh/chị có thể tự gia hạn trực tiếp tại trang Quản lý Proxy trên trang web Cloudmini nhé!"*
       - **KHÁC EMAIL (QUY TẮC CỨNG BẢO MẬT 100%)**: ⛔ Cấm nói hạn sử dụng của IP (Cấm nói "còn hạn đến..."). Cấm dùng từ "không khớp", "quyền sở hữu", "tài khoản khác", "liên kết email khác". Tuyệt đối 100% không đọc/tiết lộ email khác từ kết quả tool. Không chuyển admin. ✅ Bắt buộc báo cho khách: *"Dạ IP đó hiện tại không còn khả dụng, không thể khôi phục hay gia hạn lại được nữa ạ."*

### Residential Static

- Phí khôi phục: **25.000đ/proxy**.
- Chỉ Proxy bị xóa trong vòng **3 ngày trở lại** mới có thể yêu cầu khôi phục.
- Hướng dẫn khách nạp đủ số dư vào tài khoản Cloudmini để Admin tiến hành khôi phục.
- Sau khi khách cung cấp email, danh sách IP và đã nạp đủ số dư, chuyển Admin kiểm tra và xử lý.
- Khi chuyển Admin xong, thông báo chắc nịch đã chuyển case cho Admin chờ xử lý (thời gian từ vài phút đến 1 giờ).

### Proxy khác

- Miễn phí nếu còn khả năng khôi phục.
- Vẫn cần email tài khoản và danh sách IP để Admin kiểm tra.

## Thuộc tính kỹ thuật

- Residential Proxy hiện hỗ trợ chọn ISP, không hỗ trợ chọn chi tiết Bang/Thành phố trừ khi policy/hệ thống thay đổi.
- BudgetV4 và Budget Residential Static mặc định dùng HTTPS 50100 và SOCKS5 50101.
- GeoIP có thể khác giữa database; có thể đối chiếu https://www.iplocation.net/ip-lookup nhưng không cam kết mọi nguồn cùng thành phố.
- Không kết luận IP không dùng được chỉ vì có mặt trên một blacklist. Spamhaus SBL chủ yếu liên quan uy tín chống spam/email.

## Đổi thông tin kết nối Proxy

- Khách có thể xem Port, Username và Password hiện có tại Quản lý Proxy nhưng **không tự thay đổi** các thông tin này trên trang quản lý.
- Không hướng dẫn khách tìm nút Reset hoặc thao tác dashboard để đổi Port, Username hoặc Password.
- Khi khách cần đổi thông tin kết nối vì bảo mật hoặc phục vụ cấu hình, ghi nhận yêu cầu và chuyển Admin hỗ trợ đổi thủ công.
- Không xác nhận đã đổi hoặc cam kết thời gian hoàn thành khi chưa có kết quả từ Admin.
- Không yêu cầu khách gửi Username, Password, token, cookie, API key hoặc thông tin xác thực qua chat.
- Nếu khách vô tình gửi thông tin xác thực trong ảnh/tin nhắn, nhắc khách không gửi lại; chỉ ghi nhận yêu cầu đổi thủ công nếu cần.

## Sai lệch vị trí địa lý Proxy / GeoIP

- Không kết luận Proxy sai quốc gia chỉ từ vị trí hiển thị trên giao diện điền Proxy của antidetect browser hoặc từ một website kiểm tra duy nhất.
- Dữ liệu GeoIP có thể chưa được cập nhật hoặc dùng database khác nhau giữa các phần mềm và website; cùng một IP có thể được trả về vị trí khác nhau.
- Nếu đa số nguồn đối chiếu nhận diện đúng khu vực thì chưa có căn cứ kết luận Proxy sai vị trí chỉ vì một nguồn hiển thị khác.
- Hướng dẫn khách mở profile đang gắn Proxy rồi kiểm tra trực tiếp qua nhiều nguồn. Có thể gợi ý: Whoer, MaxMind, DB-IP, IPAPI, IP Location, IPdata hoặc Criminal IP.
- Không khẳng định mọi sai lệch đều do API miễn phí; chỉ dùng cách diễn đạt “có thể do dữ liệu GeoIP chưa cập nhật hoặc khác database”.
