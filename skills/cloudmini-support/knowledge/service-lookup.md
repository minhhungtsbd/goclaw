# Tra cứu dịch vụ Cloudmini bằng API

## Phạm vi

Tool `cloudmini_proxy_check` dùng cho IP Proxy hoặc VPS mà khách đã gửi trong cuộc trò chuyện. Không dùng để quét IP không liên quan.

### Ngoại lệ Residential VN dùng hostname

- Residential VN có thể cấp hostname `*.resvn.net`, ví dụ `ipv4-vt-04.resvn.net`, thay vì IPv4 dạng số. Đây là định dạng kết nối hợp lệ; không nói hostname “chưa đủ” và không yêu cầu khách tìm IP số.
- `service_info`/`live_check` hiện là luồng theo IP và có thể không dùng được với hostname Residential VN. Bỏ qua API cho case này.
- Câu hỏi cấu hình: dùng hostname ở trường Host/IP và port đúng trong cột Proxy Port. Không suy ra một số khác là port nếu giao diện không ghi như vậy.
- Lỗi kết nối/chậm, riêng website không tải hoặc yêu cầu thay proxy: hỗ trợ bước an toàn nếu phù hợp; nếu vẫn lỗi hoặc khách cần thao tác, chuyển Admin bằng hostname + email đã có. Không đưa user/pass hoặc chuỗi `host:port:user:pass` vào handoff.

## service_info

Dùng trước khi xử lý lỗi, đổi, hủy, gia hạn, khôi phục hoặc nâng cấp khi đã có IP.

Kết quả có thể cho biết:
- IP, gói (`plan`) và hạn (`expire`);
- nhóm gói chuẩn hóa (`plan_family`) để phân biệt chính xác `residential_static` với `budget_residential_static`;
- khu vực/nhà mạng (`region`, ví dụ: `"region": "Việt Nam - Viettel"`);
- kết quả xác minh email qua `account_email_matches`; tool không trả email hệ thống cho LLM.

Quy tắc:
1. Nếu khách đã cung cấp email, truyền qua `account_email`. Chỉ dùng `account_email_matches` để xác minh khi IP còn gắn với dịch vụ; với IP `deleted`/`expire: null`, email khách cung cấp là tài khoản nhận khôi phục và tool không đối chiếu với chủ cũ.
2. Không hỏi lại loại Proxy/VPS hoặc gói khi API đã xác định được.
3. Khi API trả về trường `region` (ví dụ: `"region": "Việt Nam - Viettel"`), hãy sử dụng thông tin này để tư vấn khách hàng hoặc xác nhận vị trí địa lý/nhà mạng của gói dịch vụ mà không cần hỏi lại khách hàng.
4. Không hiển thị, đọc lại hoặc suy đoán email hệ thống. Khi cần handoff, chỉ dùng email do khách đã cung cấp trong hội thoại.
5. Dùng `plan` cùng các file chính sách để xác định điều kiện đổi/hủy/nâng cấp. Không tự suy ra chính sách ngoài tài liệu.
6. Phân biệt trạng thái: `active` là còn hạn; `expired` là hết hạn nhưng bản ghi còn tồn tại; `deleted`/`expire: null` là đã xóa khỏi dịch vụ; `unknown` là không đọc được hạn. Không gộp `expired` và `deleted` thành một trạng thái.
7. Với `expired`, không gọi `live_check`; hướng dẫn khách kiểm tra khả năng tự gia hạn. Với `deleted`, áp dụng luồng khôi phục và không khẳng định có thể khôi phục nếu chưa có Admin xác nhận.
8. Nếu API không có dữ liệu hoặc lỗi, không kết luận IP không thuộc Cloudmini. Xin bằng chứng không nhạy cảm hoặc chuyển Admin khi cần thao tác thủ công.
9. Nếu API trả `service_status: "email_required"`, chỉ xin email tài khoản Cloudmini. Không nêu plan, region, expire, trạng thái, quyền sở hữu, phí hoặc khả năng khôi phục/gia hạn trước khi có email.
10. Nếu API trả `service_status: "not_verified"`, đây là kết quả không xác minh được theo email đã cung cấp, không phải bằng chứng để nói IP thuộc tài khoản khác. Không tiết lộ hoặc suy đoán nguyên nhân; không gọi `live_check`. Với yêu cầu khôi phục/gia hạn, gọi `escalate_to_admin` bằng đúng IP và email khách đã cung cấp để Admin kiểm tra trực tiếp. Chỉ xác nhận đã chuyển sau khi tool thành công và phải kèm mã Ticket thật; không đề xuất mua/đăng ký Proxy mới.

## live_check

Chỉ dùng sau `service_info` khi khách báo Proxy không kết nối và kết quả vừa xác minh đúng email, dịch vụ Proxy đang `active`. Không dùng cho VPS, `expired`, `deleted`, `not_verified`, câu hỏi chính sách hoặc yêu cầu xác định vị trí.

- Kết quả chỉ cho biết Proxy đang LIVE hay DIE tại thời điểm kiểm tra.
- Không dùng `live_check` hoặc GeoIP để kết luận location; vị trí/nhà mạng chỉ lấy từ `region` của `service_info`.
- Không kết luận lỗi do khách, ứng dụng, nền tảng bên thứ ba hoặc Cloudmini chỉ từ một lần kiểm tra.
- Khi kết quả LIVE, đọc `proxy-troubleshooting.md` và hướng dẫn bước phù hợp trước khi chuyển xử lý. Khi DIE, tool lỗi hoặc khách đã thử các bước phù hợp nhưng vẫn lỗi, đọc `escalation.md` rồi chuyển Admin/Kỹ thuật nếu đủ dữ kiện.

## Handoff

Khi yêu cầu cần Admin/Kỹ thuật, đưa vào `escalate_to_admin`:
- yêu cầu của khách;
- IP, gói, hạn và region API trả về; riêng Residential VN dùng hostname `*.resvn.net` nếu không có IP số;
- email Cloudmini do khách đã cung cấp, chỉ trong nội dung nội bộ;
- kết quả `live_check` nếu có;
- lỗi, ảnh và các bước đã thử.

## Diễn giải expire: null

Khi service_info trả expire: null cùng service_status: "deleted", IP đó đã bị xóa và không còn gắn với dịch vụ Cloudmini nào.

- Không dùng live_check để kết luận lỗi kết nối cho IP này.
- Khi khách yêu cầu khôi phục, không so sánh email khách với email chủ sở hữu cũ. Email khách cung cấp là tài khoản nhận khôi phục và vẫn phải có trong handoff.
- Không tiết lộ email chủ cũ và không từ chối chỉ vì `account_email_matches` là false hoặc không có.
- Xác định đúng luồng khôi phục Proxy hoặc VPS theo gói nếu API còn trả plan; không tự cam kết khả năng khôi phục.
- Khi cần Admin xử lý, handoff gồm IP, gói nếu có, email tài khoản nhận khôi phục và yêu cầu của khách.
