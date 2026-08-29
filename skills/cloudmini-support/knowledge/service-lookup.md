# Tra cứu dịch vụ Cloudmini bằng API

## Phạm vi

Tool `cloudmini_proxy_check` dùng cho IP Proxy hoặc VPS mà khách đã gửi trong cuộc trò chuyện. Không dùng để quét IP không liên quan.

## service_info

Dùng trước khi xử lý lỗi, đổi, hủy, gia hạn, khôi phục hoặc nâng cấp khi đã có IP.

Kết quả có thể cho biết:
- IP, gói (`plan`) và hạn (`expire`);
- nhóm gói chuẩn hóa (`plan_family`) để phân biệt chính xác `residential_static` với `budget_residential_static`;
- khu vực/nhà mạng (`region`, ví dụ: `"region": "Việt Nam - Viettel"`);
- email tài khoản Cloudmini (`user_email`) để đối chiếu và handoff nội bộ.

Quy tắc:
1. Nếu khách đã cung cấp email, truyền qua `account_email`. Chỉ dùng `account_email_matches` làm điều kiện sở hữu khi IP còn gắn với dịch vụ; không dùng trường này để chặn yêu cầu khôi phục IP `deleted`/`expire: null`.
2. Không hỏi lại loại Proxy/VPS hoặc gói khi API đã xác định được.
3. Khi API trả về trường `region` (ví dụ: `"region": "Việt Nam - Viettel"`), hãy sử dụng thông tin này để tư vấn khách hàng hoặc xác nhận vị trí địa lý/nhà mạng của gói dịch vụ mà không cần hỏi lại khách hàng.
4. Không hiển thị, đọc lại hoặc xác nhận email API với khách; email chỉ dùng nội bộ hoặc trong `escalate_to_admin`.
5. Dùng `plan` cùng các file chính sách để xác định điều kiện đổi/hủy/nâng cấp. Không tự suy ra chính sách ngoài tài liệu.
6. Phân biệt trạng thái: `active` là còn hạn; `expired` là hết hạn nhưng bản ghi còn tồn tại; `deleted`/`expire: null` là đã xóa khỏi dịch vụ; `unknown` là không đọc được hạn. Không gộp `expired` và `deleted` thành một trạng thái.
7. Với `expired`, không gọi `live_check`; hướng dẫn khách kiểm tra khả năng tự gia hạn. Với `deleted`, áp dụng luồng khôi phục và không khẳng định có thể khôi phục nếu chưa có Admin xác nhận.
8. Nếu API không có dữ liệu hoặc lỗi, không kết luận IP không thuộc Cloudmini. Xin bằng chứng không nhạy cảm hoặc chuyển Admin khi cần thao tác thủ công.

## live_check

Dùng sau `service_info` cho case Proxy không kết nối, Check Live lỗi hoặc cần kiểm tra vị trí GeoIP.

- Kết quả trả về vị trí IP để đối chiếu kỹ thuật.
- Không kết luận lỗi do khách, ứng dụng, nền tảng bên thứ ba hoặc Cloudmini chỉ từ một lần kiểm tra.
- Khi `live_check` lỗi hoặc kết quả không giải thích được lỗi thực tế, chuyển Admin/Kỹ thuật với IP, gói/hạn từ `service_info`, phần mềm đang dùng và ảnh/nội dung lỗi.

## Handoff

Khi yêu cầu cần Admin/Kỹ thuật, đưa vào `escalate_to_admin`:
- yêu cầu của khách;
- IP, gói, hạn và region API trả về;
- email tài khoản từ API hoặc email khách đã cung cấp, chỉ trong nội dung nội bộ;
- kết quả `live_check` nếu có;
- lỗi, ảnh và các bước đã thử.

## Diễn giải expire: null

Khi service_info trả expire: null cùng service_status: "deleted", IP đó đã bị xóa và không còn gắn với dịch vụ Cloudmini nào.

- Không dùng live_check để kết luận lỗi kết nối cho IP này.
- Khi khách yêu cầu khôi phục, không so sánh email khách với email chủ sở hữu cũ. Email khách cung cấp là tài khoản nhận khôi phục và vẫn phải có trong handoff.
- Không tiết lộ email chủ cũ và không từ chối chỉ vì `account_email_matches` là false hoặc không có.
- Xác định đúng luồng khôi phục Proxy hoặc VPS theo gói nếu API còn trả plan; không tự cam kết khả năng khôi phục.
- Khi cần Admin xử lý, handoff gồm IP, gói nếu có, email tài khoản nhận khôi phục và yêu cầu của khách.
