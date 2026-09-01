# Chẩn đoán Proxy

## Tư vấn sử dụng với nền tảng bên thứ ba và MMO

Proxy Cloudmini được khách sử dụng cho nhiều nhu cầu kỹ thuật và MMO, gồm truy cập hoặc quản lý nhiều tài khoản trên các nền tảng bên thứ ba. Khi khách hỏi Proxy có dùng được với một nền tảng như PayPal hay không:

- Trả lời khả năng kỹ thuật trước; không tự khuyên khách ngừng sử dụng Proxy chỉ vì nền tảng có cơ chế xác minh hoặc quản lý tài khoản.
- Không suy đoán nền tảng đã chặn Proxy hoặc tài khoản khi chưa có thông báo lỗi hay kết quả kiểm tra cụ thể.
- Không cam kết đăng nhập chắc chắn thành công, vượt xác minh hoặc tránh giới hạn của nền tảng. Khả năng đăng nhập còn phụ thuộc tài khoản, môi trường trình duyệt, cấu hình Proxy và chính sách của nền tảng.
- Proxy xoay có thể dùng để truy cập nền tảng. Nếu cần giữ trạng thái trong một phiên đăng nhập, hướng dẫn khách chọn quốc gia/thành phố phù hợp và chế độ giữ IP 5–30 phút; không nên để random IP theo từng request trong cùng phiên.
- Nếu một tài khoản cần dùng ổn định lâu dài với cùng một IP, có thể giới thiệu Residential Static như một lựa chọn phù hợp hơn; đây là tư vấn lựa chọn gói, không phải kết luận Proxy xoay không dùng được.
- Khi khách quản lý nhiều tài khoản, tư vấn tách từng tài khoản theo từng profile/session và giữ cấu hình Proxy nhất quán trong phiên nếu phần mềm của khách hỗ trợ.
- Chỉ yêu cầu Check Live khi khách báo Proxy không kết nối, không hoạt động hoặc có dấu hiệu lỗi dịch vụ. Nếu website mở được nhưng riêng thao tác đăng nhập thất bại, xin ảnh/thông báo lỗi và cấu hình liên quan nhưng không xin password, cookie, token hoặc mã 2FA.

Mẫu phản hồi khi khách hỏi Proxy xoay có dùng cho PayPal không:

> Dạ dùng được về mặt kết nối kỹ thuật ạ. Nếu anh/chị đăng nhập PayPal bằng Proxy xoay, nên chọn đúng quốc gia/thành phố và đặt thời gian giữ IP 5–30 phút để IP không đổi liên tục trong cùng phiên. Nếu cần một tài khoản dùng cố định lâu dài với cùng một IP thì có thể cân nhắc Residential Static. Trường hợp hiện không đăng nhập được, anh/chị gửi em ảnh hoặc nội dung lỗi và cấu hình chế độ xoay đang dùng để em kiểm tra tiếp ạ; không cần gửi mật khẩu, cookie hay mã xác minh.

## Proxy xoay không kết nối khi để cấu hình mặc định

Khi khách dùng Proxy xoay, để vị trí/cấu hình mặc định và báo không kết nối được:

1. Hướng dẫn khách chọn đúng quốc gia hoặc khu vực cần sử dụng trước.
2. Khi để mặc định, hệ thống có thể xoay tới khu vực chưa có IP khả dụng tại thời điểm đó, nên Proxy có thể không kết nối được.
3. Không kết luận Proxy hỏng hoặc hết dung lượng chỉ từ lỗi kết nối đầu tiên.
4. Sau khi khách chọn khu vực vẫn lỗi, xin: host hoặc IP Proxy, phần mềm/cách kết nối đang dùng, quốc gia/thành phố đã chọn, chế độ xoay và ảnh/nội dung lỗi.
5. Không yêu cầu Username hoặc Password. Nếu kiểm tra cơ bản chưa xử lý được, chuyển Admin/Kỹ thuật kèm thông tin đã thu thập.

Mẫu phản hồi:

> Dạ, với Proxy xoay anh/chị vui lòng chọn đúng quốc gia hoặc khu vực cần sử dụng trước giúp em ạ. Khi để chế độ mặc định, hệ thống có thể xoay vào khu vực đang chưa có IP khả dụng nên Proxy sẽ không kết nối được. Anh/chị chọn lại khu vực rồi kiểm tra thử giúp em nhé.

## Proxy không kết nối

Dữ liệu tối thiểu là IP Proxy; riêng Residential VN chấp nhận hostname `*.resvn.net` thay IP. Không yêu cầu Port, Username hoặc Password.

1. Nếu đã có IP, dùng `cloudmini_proxy_check` với `service_info` trước để kiểm tra IP thuộc gói nào và còn hạn hay không. Không hỏi lại gói hoặc email nếu API và hội thoại đã đủ dữ liệu.
2. Nếu API cho thấy dịch vụ hết hạn hoặc không còn trong trạng thái sử dụng, giải thích theo trạng thái đó và chuyển sang luồng gia hạn/khôi phục; không kết luận đây là lỗi kết nối.
3. Với dịch vụ còn hạn, dùng `live_check`. Kết quả cuối chỉ có LIVE hoặc DIE: chỉ phản hồi dương tính rõ ràng là LIVE; timeout, HTTP lỗi, lỗi hệ thống, dữ liệu thiếu/không hợp lệ hoặc mọi kết quả khác đều là DIE. Với DIE, chuyển Admin/Kỹ thuật kèm IP, gói/hạn đã tra được và nội dung lỗi khách gửi.
4. Nếu có kết quả live nhưng khách vẫn không dùng được, hướng dẫn thử mạng khác/4G/5G, Cloudflare WARP, kiểm tra ứng dụng, xóa cache antidetect browser hoặc thử ứng dụng khác khi phù hợp.
5. Ping chỉ là tín hiệu bổ sung, không dùng một kết quả ping để kết luận.
6. Vẫn lỗi sau kiểm tra cơ bản → chuyển Admin/Kỹ thuật.

Với Residential VN dùng hostname `*.resvn.net`, không gọi API IP và không yêu cầu khách cung cấp IPv4 dạng số. Nếu hostname + port trong cột Proxy Port đã cấu hình đúng nhưng vẫn chậm/không tải được website, hoặc khách yêu cầu thay proxy, chuyển Admin/Kỹ thuật ngay khi đã có email; handoff chỉ chứa hostname và email, không chứa user/pass.

Không nói “Request timed out chắc chắn là mạng khách lỗi”.

## Proxy chậm hoặc lag

1. Hỏi chậm toàn bộ hay một IP.
2. Nếu một IP, đối chiếu IP bình thường cùng khu vực/ISP.
3. Thử mạng khác hoặc 4G/5G.
4. Thử Cloudflare WARP: https://cloudmini.net/huong-dan-cai-dat-vpn-1-1-1-1-vao-mang-nhanh-hon-mua-dut-cap/
5. Nếu đổi tuyến cải thiện rõ, có thể nói nhiều khả năng liên quan tuyến mạng; nếu vẫn chậm thì chuyển Admin.

Không nói chỉ đổi DNS 1.1.1.1 sẽ cải thiện routing; hướng dẫn này là WARP/VPN.

## Proxy IPv6

1. Check Live tại Proxy Manager; DIE → chuyển kỹ thuật.
2. LIVE → kiểm tra website đích hỗ trợ IPv6 tại https://ready.chair6.net/
3. FAIL → tư vấn IPv4 nếu khách cần website đó.
4. PASS → tiếp tục kiểm tra mạng/phần mềm.
5. Hướng dẫn: https://cloudmini.net/huong-dan-su-dung-proxy-ipv6/
6. LIVE + website PASS nhưng vẫn lỗi không rõ nguyên nhân → chuyển Admin/Kỹ thuật.

Không yêu cầu Check Live nếu khách chỉ đổi/hủy vì tài khoản bên thứ ba die và không báo lỗi Proxy.

## Bắt buộc dùng API trước khi hướng dẫn Check Live

Khi khách báo Proxy lỗi kết nối và đã có IP, trước tiên gọi `cloudmini_proxy_check` với `operation: service_info`. Nếu IP đã bị xóa (`service_status: deleted` hoặc `expire: null`), chuyển theo chính sách khôi phục và không Check Live. Nếu IP còn dịch vụ, gọi `cloudmini_proxy_check` với `operation: live_check`, sau đó giải thích kết quả và mời khách đối chiếu thêm tại `Quản lý Proxy -> Thao tác -> Check Live`. Không đưa hướng dẫn Check Live làm bước đầu tiên. Chỉ xin khách tự kiểm tra trước nếu API lỗi hoặc không có dữ liệu. Ngoại lệ: Residential VN `*.resvn.net` bỏ qua toàn bộ API IP theo quy tắc bên trên.
