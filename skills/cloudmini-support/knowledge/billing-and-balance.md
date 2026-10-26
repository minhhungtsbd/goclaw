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
- Point thưởng theo chương trình VIP được cộng tự động vào tài khoản khi giao dịch **nạp tiền thành công**. Ưu đãi riêng cho gia hạn năm cần Admin cộng thủ công theo mục dưới.

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
- Câu hỏi ưu đãi nạp tiền thông thường: giải thích chương trình VIP. Khi khách hỏi Proxy lô/giá sỉ, đọc `bulk-proxy-ordering.md` và theo luồng báo giá lô; khi hỏi khuyến mãi gia hạn năm, theo mục dưới. Không bắt khách nghe lại VIP hoặc lặp yêu cầu trước khi chuyển đúng luồng. Không tự cam kết phần trăm giảm giá.

Mẫu phản hồi:

> Dạ, ưu đãi hiện có của Cloudmini là cộng Point khi nạp tiền theo cấp VIP dựa trên số dịch vụ đang hoạt động. Ví dụ tài khoản có từ 100 dịch vụ đang hoạt động là VIP 2, được cộng 15% Point khi nạp tiền thành công ạ. Point được dùng cho đơn/gia hạn khi đủ thanh toán toàn bộ; ưu đãi này không phải giảm trực tiếp giá từng Proxy/VPS.

## Mua theo năm và gia hạn thêm 1 năm

- Các gói VPS và Proxy hiện chưa có gói **mua mới theo năm**. Khách mua dịch vụ thành công trước, sau đó có thể **gia hạn thêm 1 năm** theo điều kiện gia hạn của đúng gói. Không nói "không hỗ trợ dùng theo năm" chỉ vì trang mua mới không có lựa chọn năm.
- Không tự mô tả số lần bấm gia hạn, chu kỳ có trên giao diện hoặc ngày hết hạn sau gia hạn khi chưa có dữ liệu xác nhận. Với Proxy lô, gia hạn phải bao gồm nguyên dải đã đặt theo `bulk-proxy-ordering.md`; không hứa gia hạn lẻ hoặc áp dụng cho gói vốn không gia hạn được.
- Khách nạp tiền để gia hạn năm **có thể được xem xét ưu đãi thêm**; Admin cần xác nhận điều kiện và **cộng tiền khuyến mãi thủ công**. Không nói ưu đãi này tự động, tự chốt số tiền/phần trăm, hoặc đảm bảo cộng dồn với VIP. Chưa có xác nhận tiền khuyến mãi được cộng vào loại số dư nào thì không tự gọi đó là Point hay số dư chính.
- Nếu khách chỉ hỏi chính sách: giải thích ngắn gọn, không bắt xin email/IP hay tạo ticket. Nếu khách muốn nhận báo giá/ưu đãi hoặc nhờ cộng khuyến mãi: lấy email Cloudmini đã có, gói dịch vụ, nhu cầu gia hạn 1 năm; dùng số tiền/mã giao dịch nếu khách đã nạp và chuyển Admin xác nhận. Không bắt khách nạp trước để được hỏi mức khuyến mãi.
- Nếu cần can thiệp gia hạn một dịch vụ cụ thể, dùng IP và `service_info` theo core. Nếu chỉ xét khuyến mãi cho tài khoản, không lấy IP cũ không liên quan hoặc yêu cầu IP chưa được cấp. Chỉ nói Admin đã nhận yêu cầu khi handoff thành công và có Ticket thật.
- Nếu khách đã nạp tiền mà chưa được cộng ưu đãi năm: phân biệt tiền nạp gốc và phần khuyến mãi thủ công. Thu thập bằng chứng giao dịch cần thiết rồi chuyển Admin đối soát; không tự kết luận mất tiền hoặc tự hứa mức bù.

Ví dụ tư vấn:

> Dạ, hiện VPS và Proxy chưa có gói mua mới theo năm. Sau khi mua thành công, anh/chị có thể gia hạn thêm 1 năm theo điều kiện của gói ạ. Với nhu cầu nạp tiền để gia hạn năm, bên em có thể xem xét ưu đãi thêm; Admin sẽ xác nhận mức hỗ trợ và cộng tiền khuyến mãi thủ công.

## VAT và hoá đơn

- Các giao dịch mua hàng tự động (nạp tiền, đặt dịch vụ qua web) trước đến nay **chưa tính VAT** nên **không xuất được hoá đơn** cho các giao dịch đó. Không hứa xuất bổ sung hoá đơn cho giao dịch đã hoàn tất.
- Nếu khách có nhu cầu xuất hoá đơn VAT: khách cần **chuyển tiền vào tài khoản công ty do Admin cung cấp**. Lưu ý tư vấn rõ: khi chuyển tiền xuất hoá đơn sẽ bị **trừ 10% VAT**.
- Việc xuất hoá đơn cần Admin can thiệp. Sau khi tư vấn và khách xác nhận có nhu cầu, gọi `escalate_to_admin` để Admin cung cấp thông tin tài khoản chuyển tiền và xuất hoá đơn sau đó. Ticket chứa email tài khoản Cloudmini và nhu cầu xuất hoá đơn của khách; case hoá đơn không cần IP.
- Không tự cung cấp số tài khoản công ty, không cam kết thời gian xuất hoá đơn; chỉ xác nhận đã chuyển Admin khi tool thành công và kèm mã Ticket thật.
- Tiền hoàn hợp lệ được cộng vào số dư chính Cloudmini, không hoàn về ngân hàng.
