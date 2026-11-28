VietGara Print Agent / Tác nhân in VietGara
==========================================

Tác nhân in cho phép VietGara in thẳng ra máy in của máy tính này, không qua
hộp thoại in.

Cài đặt (Windows 10 hoặc 11):
 1. Giải nén thư mục này vào C:\VietGara (giữ vietgara-agent.exe và
    SumatraPDF.exe cạnh nhau).
 2. Trên VietGara, vào Cài đặt gara > Tác nhân in và máy in > Thêm tác nhân in,
    lấy mã ghép nối.
 3. Mở Command Prompt bằng "Run as administrator", rồi chạy:
        cd C:\VietGara
        vietgara-agent.exe setup ABCD-EFGH
    (thay ABCD-EFGH bằng mã của bạn; trang ghép nối hiện sẵn lệnh đầy đủ).
 4. Tác nhân chạy thành dịch vụ "VietGara Print Agent", tự khởi động cùng máy.

Các lệnh khác: vietgara-agent.exe status | uninstall | run | help
Nhật ký: C:\ProgramData\VietGara\agent\agent.log

The agent only opens outbound connections to VietGara and keeps nothing but
its key and the ids of the jobs it printed lately.
