# Vận hành và vòng đời VPS

## Kiểm tra dịch vụ trước khi áp dụng chính sách

- Khi khách cung cấp IP VPS, dùng `cloudmini_proxy_check` với `service_info` để xác định gói và hạn trước khi hỏi lại hoặc áp dụng chính sách.
- Với Custom, Mini, Promo và YT còn hiệu lực: có thể hướng dẫn luồng đổi/hủy theo chính sách hoặc chuyển Admin cho yêu cầu nâng cấp cấu hình. Không tự hứa Admin duyệt nâng cấp hay có tài nguyên.
- Với NN1-NN6: không đổi IP/VPS, không hủy/hoàn theo nhu cầu.
- Nếu VPS hết hạn/đã xóa hoặc API không trả dữ liệu, không tự xác nhận khả năng khôi phục; chuyển Admin kèm IP, kết quả API và email tài khoản nhận khôi phục. Với VPS đã xóa, không yêu cầu email này khớp chủ sở hữu cũ.
- Không đọc hoặc tiết lộ email API cho khách. Nếu khách đã gửi email, dùng kết quả `account_email_matches` để đối chiếu nội bộ.

## Windows và bản quyền

- Windows cài trên VPS là **bản dùng thử, chưa kèm bản quyền Windows** trong gói VPS. Khi khách hỏi giá VPS có bao gồm Windows bản quyền hay không, nêu rõ điều này ngay.
- Không tự xác nhận Windows đã kích hoạt, có bản quyền vĩnh viễn, thời hạn dùng thử cụ thể hoặc phí mua bản quyền khi chưa có thông tin được xác nhận. Phân biệt hạn thuê VPS và thời hạn dùng thử/kích hoạt Windows.
- Với cảnh báo hết dùng thử/chưa kích hoạt: giải thích chính sách trên; không tự kết luận VPS hết hạn hay lỗi hạ tầng. Nếu khách cần xử lý thực tế trên VPS, tra `service_info` theo core và chuyển Admin/Kỹ thuật khi cần; không hứa tự động được cấp bản quyền miễn phí.
- Nếu khách hỏi cách có bản quyền hoặc cần báo giá, ghi nhận phiên bản Windows/nhu cầu và nhờ Admin xác nhận phương án. Không xin/gửi key bản quyền, hướng dẫn crack, KMS không được cấp phép hay tự hứa kéo dài dùng thử.

## Mật khẩu

Mật khẩu VPS không gửi qua email. Khách lấy hoặc đặt lại mật khẩu tại Quản lý VPS. Không yêu cầu khách gửi mật khẩu qua chat.

## Nâng cấp

- Nếu đã có IP, tra `service_info` trước để nhận diện gói. Với gói NN, báo không hỗ trợ đổi IP/VPS hoặc hủy/hoàn theo nhu cầu; với các gói còn lại, chuyển Admin duyệt yêu cầu nâng cấp.
- Nâng ổ cứng theo quy trình thông thường không làm mất dữ liệu hiện có.
- Hệ thống xử lý tự động nhưng yêu cầu nâng cấu hình cần Admin duyệt để đảm bảo máy chủ đủ tài nguyên; hoàn tất thì khách F5 trang quản lý.
- Giá nâng cấp có thể khác giá VPS mới, đặc biệt với Promo.

## Gia hạn và khôi phục

- VPS còn hạn trong tài khoản: khách tự gia hạn nếu đủ số dư.
- Mua mới theo năm/gia hạn thêm 1 năm và ưu đãi nạp tiền: đọc `billing-and-balance.md`. Chưa có gói mua mới theo năm; sau mua thành công có thể gia hạn thêm 1 năm theo điều kiện gói, khuyến mãi thêm cần Admin xác nhận và cộng thủ công.
- VPS đã xóa: dùng IP tra `service_info` nếu có; tái sử dụng email khách đã gửi làm tài khoản nhận khôi phục, không đối chiếu với email chủ sở hữu cũ, rồi chuyển Admin kiểm tra; không cam kết khôi phục và không tiết lộ email cũ.

## Thời gian thay VPS hoặc IP VPS

- Với yêu cầu thay VPS/IP cần Admin xử lý, thông thường hoàn tất vào ngày kế tiếp sau khi khách gửi yêu cầu.
- Một số trường hợp đặc biệt có thể cần đến 24 giờ để hoàn thành.
- Không hứa giờ hoàn tất cụ thể; chỉ thông báo hoàn thành khi Admin/Kỹ thuật đã xác nhận kết quả.

## Hướng dẫn

- Windows hỏi lại mật khẩu RDP: https://cloudmini.net/huong-dan-cach-fix-loi-remote-vps-bi-hoi-lai-mat-khau-trong-windows/
- Không copy/paste qua RDP: https://cloudmini.net/cach-fix-loi-khong-copy-duoc-file-ben-ngoai-vao-vps/
