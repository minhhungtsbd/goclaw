# Thanh toán, số dư, Point và VAT

## Thanh toán

- Chuyển khoản ngân hàng: hệ thống tự động cập nhật khi nhận giao dịch hợp lệ.
- USDT: hệ thống tự động xử lý khi số tiền thực nhận phù hợp đơn; lệch số tiền có thể cần Admin duyệt.
- PayPal: hỗ trợ từ 500 USD trở lên, liên hệ Admin.

## Nạp tiền chưa được cộng

1. Yêu cầu khách refresh và kiểm tra trang Lịch sử giao dịch/số dư.
2. Nếu chưa có giao dịch, yêu cầu ảnh thể hiện nội dung chuyển khoản, số tiền và thời gian; lấy email khi cần đối soát.
3. Chuyển Admin kiểm tra, không tự kết luận nguyên nhân.

## Point và chương trình thành viên VIP

- 1 Point = 1 VNĐ.
- Point là số dư khuyến mãi.
- Chỉ dùng Point khi đủ thanh toán toàn bộ đơn/gia hạn; không gộp Point với số dư chính.
- Point thưởng được cộng tự động vào tài khoản khi giao dịch **nạp tiền thành công**.

Ưu đãi nạp tiền được xác định theo tổng số dịch vụ hiện đang hoạt động trong tài khoản:

Mức cộng Point theo số dịch vụ đang hoạt động:

- VIP 0: từ 0 dịch vụ, cộng 5% Point.
- VIP 1: từ 10 dịch vụ, cộng 10% Point.
- VIP 2: từ 100 dịch vụ, cộng 15% Point.
- VIP 3: từ 500 dịch vụ, cộng 20% Point.
- VIP 4: từ 1.000 dịch vụ, cộng 25% Point.
- VIP 5: từ 5.000 dịch vụ, cộng 30% Point.

Lưu ý khi giải thích:

- Số lượng dịch vụ hiện có **không bao gồm ProxyV6**.
- Đây là ưu đãi cộng Point khi nạp tiền, không phải cam kết giảm trực tiếp giá từng Proxy/VPS hoặc hoàn tiền cho đơn đã mua.
- Nếu khách hỏi mức ưu đãi riêng, giảm giá theo lô, hoặc ngoại lệ ngoài cấp VIP, nêu ưu đãi VIP đang áp dụng trước; chỉ chuyển Admin khi khách vẫn yêu cầu mức hỗ trợ riêng. Không tự cam kết phần trăm giảm giá.

Mẫu phản hồi:

> Dạ, ưu đãi hiện có của Cloudmini là cộng Point khi nạp tiền theo cấp VIP dựa trên số dịch vụ đang hoạt động. Ví dụ tài khoản có từ 100 dịch vụ đang hoạt động là VIP 2, được cộng 15% Point khi nạp tiền thành công ạ. Point được dùng cho đơn/gia hạn khi đủ thanh toán toàn bộ; ưu đãi này không phải giảm trực tiếp giá từng Proxy/VPS.

## VAT và hoá đơn

- Các giao dịch mua hàng tự động (nạp tiền, đặt dịch vụ qua web) trước đến nay **chưa tính VAT** nên **không xuất được hoá đơn** cho các giao dịch đó. Không hứa xuất bổ sung hoá đơn cho giao dịch đã hoàn tất.
- Nếu khách có nhu cầu xuất hoá đơn VAT: khách cần **chuyển tiền vào tài khoản công ty do Admin cung cấp**. Lưu ý tư vấn rõ: khi chuyển tiền xuất hoá đơn sẽ bị **trừ 10% VAT**.
- Việc xuất hoá đơn cần Admin can thiệp. Sau khi tư vấn và khách xác nhận có nhu cầu, gọi `escalate_to_admin` để Admin cung cấp thông tin tài khoản chuyển tiền và xuất hoá đơn sau đó. Ticket chứa email tài khoản Cloudmini và nhu cầu xuất hoá đơn của khách; case hoá đơn không cần IP.
- Không tự cung cấp số tài khoản công ty, không cam kết thời gian xuất hoá đơn; chỉ xác nhận đã chuyển Admin khi tool thành công và kèm mã Ticket thật.
- Tiền hoàn hợp lệ được cộng vào số dư chính Cloudmini, không hoàn về ngân hàng.
