# Chẩn đoán VPS

## Không kết nối VPS/RDP

1. Hướng dẫn Restart tại Quản lý VPS.
2. Chờ khoảng 30 giây rồi thử lại.
3. Mở VNC/Console để kiểm tra VPS có hoạt động không.
4. Nếu nghi tuyến quốc tế, thử Cloudflare WARP theo hướng dẫn chính thức.
5. Vẫn lỗi → lấy IP VPS và chuyển Kỹ thuật.

Không tự kết luận do mạng khách hoặc sai mật khẩu khi chưa có dữ liệu.

## VPS chậm hoặc lag

- Kiểm tra CPU/RAM trong VPS nếu truy cập được.
- Thử tuyến mạng khác/WARP nếu liên quan kết nối quốc tế.
- Tài nguyên bình thường nhưng lỗi kéo dài → lấy IP và chuyển Kỹ thuật.

## Copy/Paste RDP

Thử restart máy cá nhân hoặc thành phần RDP/clipboard phù hợp, sau đó dùng hướng dẫn trong `vps-operations.md`.
