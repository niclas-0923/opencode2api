# Quản trị và vận hành

[English](management.md) | [简体中文](management.zh-CN.md) | Tiếng Việt | [Italiano](management.it.md)

Bản fork này chỉ dùng kênh miễn phí ẩn danh của OpenCode Zen và phục vụ mọi thứ trên **một cổng 8080**. Hướng dẫn đầy đủ bằng tiếng Anh/Trung: [management.md](management.md), [management.zh-CN.md](management.zh-CN.md).

| Đường dẫn | Mục đích |
| --- | --- |
| `/v1/*` | API suy luận. |
| `/web` | WebUI quản trị. |
| `/api/*` | API quản trị JSON. |

Không còn Key upstream, OAuth Go/Zen, hạn mức hay luân chuyển mô hình; các trường cũ trong tệp cấu hình bị bỏ qua và xóa khi lưu.

## Khởi động

```bash
cp config.example.json config.json
# sửa server_keys, webui.password
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
./opencode2api -config config.json
```

Cần `server_keys` (khóa cục bộ) và `webui.password` (≥ 10 ký tự). `anonymous` luôn `true`; lối ra do `proxies`, `proxyfile` hoặc `warp` cung cấp. Dùng `listen: "127.0.0.1:8080"` nếu chỉ chạy cục bộ. Lần chạy đầu, mật khẩu được chuyển thành hash Argon2id.

## Đăng nhập và trang

Mở `http://127.0.0.1:8080/web`. Đăng nhập bằng `webui.username`/`webui.password`; phiên dùng cookie `HttpOnly`, `SameSite=Strict`. Mọi API quản trị (trừ đăng nhập) cần phiên; thao tác ghi cần `X-CSRF-Token` lấy từ phản hồi đăng nhập/phiên. Khóa `server_keys` không cấp quyền quản trị.

Các trang: Tổng quan, Chẩn đoán (tuyến mô hình, Playground, log trực tiếp), Hướng dẫn kết nối, Quản lý API Key cục bộ, Cấu hình, Bảo mật tài khoản. `#availability` mở Tổng quan; `#playground`/`#logs` mở Chẩn đoán.

## Lưu cấu hình

`GET /api/config` trả cấu hình đang chạy, ID/giá trị đã che và địa chỉ lắng nghe thực tế. `PUT /api/config` nhận **toàn bộ** biểu mẫu, không phải PATCH; để giữ khóa/proxy cũ, gửi `{"id":"FINGERPRINT"}`, giá trị mới gửi `{"value":"..."}`. Lưu sẽ kiểm tra, dựng runtime mới, ghi tệp rồi chuyển; lỗi thì runtime cũ tiếp tục chạy. `listen` và `webui.enabled` cần khởi động lại. Sửa tệp tay thì gọi `POST /api/config/reload`.

`POST /api/config/reveal` cần phiên, CSRF và xác minh lại mật khẩu; chỉ trả `server_keys` và proxy, không bao giờ trả mật khẩu/hash.

## Mô hình miễn phí

Chỉ mô hình miễn phí được định tuyến (ID chứa `free` hoặc giá vào/ra bằng 0). Khả dụng học từ lưu lượng thật; chỉ lỗi mô hình không tồn tại/ngừng cung cấp mới vô hiệu hóa. Bật `models.active_probes` để thử chủ động (tốn hạn mức ẩn danh).

| Endpoint | Mục đích |
| --- | --- |
| `GET /api/models/availability` | Trạng thái mô hình miễn phí. |
| `POST /api/models/probe` | Đánh dấu đến hạn và đánh thức bộ lập lịch. |
| `POST /api/models/restore` | `{"model":"ID"}` khôi phục, chuyển sang thủ công. |
| `POST /api/models/manual` | `{"model":"ID","manual":true|false}`. |

## Khóa API cục bộ

`GET/POST /api/api-keys`, `PATCH/DELETE /api/api-keys/{id}`. Tạo trả khóa đầy đủ một lần; danh sách chỉ có fingerprint, tên, giá trị che, trạng thái. Luôn phải còn ít nhất một khóa bật.

## Proxy và WARP

`POST /api/proxies/check` cần phiên + CSRF, gửi `{"id":"..."}` hoặc `{"value":"..."}`. Kiểm tra 20 giây, gọi `upstream.anonymous` + `/v1/models` không xác thực, không suy luận, không đổi sức khỏe production. Trạng thái: `available`, `unavailable`, `limited`, `rejected`, `inconclusive`; chỉ `unavailable` là bằng chứng hỏng.

Nhiều lối ra là cách tăng độ tin cậy chính. WARP mặc định tắt; cần UDP 2408 ra Cloudflare, không cần TUN. `warp.pool_size` 1–32 (mặc định 2), `warp.stack` `ipv6`/`ipv4`/`dual`.

## Playground, log, giám sát

Playground gọi `POST /api/debug/inference` trong tiến trình, không streaming, chỉ `{"protocol":"chat","key":{"mode":"auto"},"request":{...}}`; có thể ghim proxy bằng `relay`.

Log: `GET /api/logs?after=SEQ&limit=N` và `GET /api/logs/stream` (SSE, hỗ trợ `Last-Event-ID`). Giám sát: `GET /api/monitor`, `GET /api/debug/models`. Token chỉ lấy từ upstream báo cáo.

## Sức khỏe, dữ liệu, bảo mật

`GET /healthz` cùng cổng, không cần xác thực; 503 khi danh mục trống/chưa sẵn sàng, WARP không có lối ra hoặc proxy bắt buộc không khỏe. Dữ liệu nhạy cảm: `config.json`, `config.json.bak`, tệp `*.availability.json`; phiên, log và thống kê nằm trong bộ nhớ và mất khi khởi động lại.

Chỉ dùng loopback khi chạy cục bộ; truy cập mạng cần HTTPS và giới hạn. Khi đưa lên sau reverse proxy, liệt kê địa chỉ proxy trong `webui.trusted_proxies` (IP hoặc CIDR) để rate limit dùng IP thật của client (mục `X-Forwarded-For` bên phải nhất) và chỉ chấp nhận `X-Forwarded-Proto` từ proxy đó; proxy phải tự đặt hoặc nối thêm header này. Proxy cũng phải truyền nguyên Host gốc và đặt `X-Forwarded-Proto: https` vì nó ảnh hưởng cờ Secure của cookie và kiểm tra origin. Phản hồi quản trị qua HTTPS mang HSTS và `X-Robots-Tag: noindex`. Không gửi kèm cấu hình, cookie hay nội dung suy luận thật khi báo lỗi.

## Kiểm tra

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
```

Nghiệm thu: `/web` đăng nhập được, `/healthz` sẵn sàng, `/v1/models` đọc được bằng khóa cục bộ, không có cổng 8081, và một suy luận thật qua proxy/WARP thành công.
