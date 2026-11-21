# opencode2api (bản fork: chỉ kênh miễn phí ẩn danh)

[English](README.md) | [简体中文](README.zh-CN.md) | Tiếng Việt | [Italiano](README.it.md)

Gateway Go chuyển đổi giữa OpenAI Chat Completions, OpenAI Responses và Anthropic Messages, chỉ dùng **kênh miễn phí ẩn danh** của OpenCode Zen. Không cần API Key thượng nguồn, không có OAuth, gói Go hay tuyến trả phí. Dịch vụ, API suy luận, WebUI và API quản trị dùng **cùng một cổng 8080**:

| Đường dẫn | Mục đích |
| --- | --- |
| `/v1/*` | API suy luận tương thích OpenAI/Anthropic. |
| `/web` | WebUI quản trị nhúng trong tệp thực thi. |
| `/api/*` | API quản trị JSON (phiên + CSRF). |

Tài liệu chi tiết: [README tiếng Trung](README.zh-CN.md), [hướng dẫn quản trị](docs/management.vi.md).

## Bắt đầu nhanh

Cần **Go 1.24 trở lên**, hoặc dùng binary phát hành/Docker.

```bash
if [ ! -f config.json ]; then
  cp config.example.json config.json
fi
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
```

Trước khi chạy, sửa `config.json`:

1. Thay `server_keys` bằng khóa cục bộ của bạn (client dùng khóa này, không phải khóa upstream).
2. Đặt `webui.password` (tối thiểu 10 ký tự) và `webui.username` nếu muốn.
3. Không cần `zen_keys`/`go_keys`; `anonymous` luôn là `true`.
4. Cấu hình nhiều lối ra qua `proxies`, `proxyfile` hoặc bật `warp` để tăng tỉ lệ thành công.

```bash
./opencode2api -config config.json
```

Mở `http://127.0.0.1:8080/web` để đăng nhập quản trị, và dùng `http://127.0.0.1:8080/v1` làm base URL cho client. Windows: build `opencode2api.exe` rồi chạy `.\opencode2api.exe -config .\config.json`.

## Cấu hình chính

| Khóa | Ý nghĩa |
| --- | --- |
| `listen` | Địa chỉ duy nhất cho API + WebUI + quản trị, mặc định `0.0.0.0:8080`; dùng `127.0.0.1:8080` nếu chỉ chạy cục bộ. |
| `server_keys` | Danh sách khóa cục bộ client dùng để gọi gateway. |
| `anonymous` | Luôn `true`; backend ép buộc, giá trị trong tệp cũ bị bỏ qua. |
| `proxies`, `proxyfile` | Danh sách proxy HTTP/SOCKS cho các lối ra ẩn danh. |
| `upstream.anonymous` | URL kênh miễn phí, mặc định `https://opencode.ai/inference`. |
| `warp.enabled`, `warp.pool_size`, `warp.stack` | Bể lối ra Cloudflare WARP trong tiến trình (1–32, mặc định 2; `ipv6`). |
| `models.free_only` | Chỉ công bố mô hình miễn phí (nên `true`). |
| `models.active_probes` | Thử nghiệm chủ động (mặc định `false`), có thể tiêu hao hạn mức ẩn danh. |
| `webui.enabled`, `webui.username`, `webui.password` | Bật WebUI và tài khoản quản trị. |

Các trường cũ như `zen_keys`, `go_keys`, `go_oauth`, `zen_oauth`, `prefer`, `upstream.zen`, `upstream.go` vẫn nạp được để tương thích nhưng bị bỏ qua và bị xóa khi lưu. `webui.listen` không còn dùng.

## Gọi API

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $LOCAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Xin chào"}]}'
```

Ba giao thức dùng chung base URL và khóa cục bộ: `/v1/chat/completions`, `/v1/responses`, `/v1/messages`. Hỗ trợ streaming SSE. `GET /v1/models` liệt kê mô hình miễn phí hiện có.

Mọi suy luận đi qua kênh ẩn danh; khi một lối ra bị giới hạn, gateway chuyển sang proxy/WARP khác theo sức khỏe. Không có chuyển tiếp sang Key, OAuth hay gói trả phí.

## Docker

```bash
docker build -t opencode2api .
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.json:/data/config.json" \
  opencode2api
```

Chỉ một cổng 8080 được publish. Biến môi trường: `OPENCODE2API_PORT` (mặc định 8080) và `OPENCODE2API_LISTEN` (tùy chọn). Không còn `OPENCODE2API_WEBUI_PORT` hay `WEBUI_LISTEN`.

## Sức khỏe và phát triển

`GET /healthz` không cần xác thực, trả 503 khi danh mục chưa sẵn sàng, WARP không có lối ra hoặc proxy bắt buộc không khỏe; trả 200 (có thể `degraded`) khi dùng được. Không có server key, phản hồi chỉ gồm `status` và `ready`; server key hợp lệ nhận thêm bản tóm tắt tài nguyên.

Chỉ dùng loopback khi chạy cục bộ. Truy cập mạng cần HTTPS và hạn chế quyền vào phần quản trị. Reverse proxy phải truyền nguyên Host gốc và đặt `X-Forwarded-Proto: https`. Liệt kê địa chỉ proxy trong `webui.trusted_proxies` (IP hoặc CIDR) để mục `X-Forwarded-For` bên phải nhất trở thành danh tính client cho giới hạn đăng nhập và Playground, đồng thời chỉ proxy tin cậy mới được chấp nhận `X-Forwarded-Proto`; proxy phải tự đặt hoặc nối thêm header này, không chuyển nguyên giá trị do client gửi. Không có cấu hình đó, giới hạn dùng địa chỉ peer trực tiếp nên các client sau cùng một proxy dùng chung hạn mức. Bản thân chương trình phục vụ HTTP; phản hồi quản trị qua HTTPS mang HSTS và `X-Robots-Tag: noindex`.

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
for file in webui/js/*.js webui/js/pages/*.js; do
  node --input-type=module --check < "$file" || exit 1
done
```

Không commit `config.json`, bản sao lưu hay tệp trạng thái: chúng chứa khóa cục bộ và mật khẩu proxy.
