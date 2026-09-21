# Mio 图床

单用户轻量图床，**Go + SQLite + Vue 3**。管理界面编译后嵌入同一个可执行文件，图片存在本地磁盘，不需要单独的数据库或 Node.js 运行时。

- **上传图片**：选择文件夹、拖拽或多选上传、查看进度、复制 URL 或 Markdown。
- **文件夹管理**：搜索、预览、分页；创建或重命名文件夹；移动、下载和删除图片。
- **系统设置**：浅色 / 深色 / 跟随系统，默认分享格式，查看用量，修改密码。

格式：JPG / PNG / GIF / WebP，单张不超过 20 MB、8000 万像素，不接收 SVG。管理需要登录；`/i/{id}` 和 `/download/{id}` 公开访问。单层文件夹，移动图片不会改变直链。删除文件夹后图片回到未分类。

## 部署

用 Docker 即可：

```sh
git clone https://github.com/amazonmio/mio-image-hosting.git
cd mio-image-hosting
docker compose up -d --build
```

打开 http://127.0.0.1:8080 ，按引导创建管理员账号。数据和图片在命名卷 `mio-data`。同一个数据目录只运行一个实例。

公网请在反代后面提供 HTTPS，并设置直链域名：

```yaml
environment:
  ADDR: 0.0.0.0:8080
  DATA_DIR: /data
  PUBLIC_BASE_URL: https://img.example.com
```

反代需要保留原始 `Host`，允许至少 21 MB 请求体，给上传留够超时。按域名根路径部署，不支持子路径。`PUBLIC_BASE_URL` 只决定分享链接，不会配置 DNS 或证书。

绑定主机目录时，该目录要对容器用户 `65532` 可写：

```sh
docker run --rm -p 8080:8080 \
  -e ADDR=0.0.0.0:8080 -e DATA_DIR=/data \
  -v /var/lib/mio-image-hosting:/data \
  mio-image-hosting:latest
```

## 配置

通过环境变量配置，不会读取 `.env`。

| 变量 | 默认 | 用途 |
| --- | --- | --- |
| `ADDR` | `127.0.0.1:8080` | 监听地址。容器内必须是 `0.0.0.0:8080` |
| `DATA_DIR` | `data` | 数据目录；生产建议用绝对路径 |
| `PUBLIC_BASE_URL` | 空 | 直链根地址，例如 `https://img.example.com` |
| `ADMIN_TOKEN` | 空 | 可选。给自动化客户端做 Bearer 鉴权，网页仍走账号登录 |

不单独编译时，也可以直接构建二进制：

```sh
cd web && npm ci && npm run build && cd ..
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/mio-image-hosting .
./bin/mio-image-hosting
```

生产只需要可执行文件和可写的数据目录。改过前端后要重新编译 Go。Windows 可用 `scripts/build.ps1`。

## 首次启动

1. 打开图床，点击「开始设置」。
2. 管理员账号：3–32 位字母、数字、下划线或短横线；密码至少 10 个字符。
3. 完成后用该账号登录。初始化只能做一次。

用域名或 IP 远程访问时，向导会要求初始化密钥：在服务器 `DATA_DIR/setup-key.txt` 里读取，成功后文件会删掉。不要把这个文件发给别人或提交进仓库。

登录保存在服务端，有效期 7 天，重启不会退出。设置里可以退出或改密码；改密码后所有会话失效。同一来源连续登录失败超过 10 次会限制 15 分钟。反代后的访客可能共享这个限制。没有邮件找回、公开注册和多用户。

## 备份

```text
DATA_DIR/app.db      SQLite 元数据
DATA_DIR/uploads/    图片原文件
```

先停服务，再复制整个 `DATA_DIR`（含可能存在的 WAL），恢复时把目录放回去再启动。不要只备份图片或只备份数据库。

## 开发

```sh
go run .
```

```sh
cd web
npm ci
npm run dev
```

打开 Vite 给出的地址（默认 http://127.0.0.1:5173）。`/api`、`/i/`、`/download/` 会代理到 8080。请固定用 `127.0.0.1` 或 `localhost` 其中一种，不要混用。

```sh
go test ./...
cd web && npm test && npm run build
```

## 接口

浏览器靠登录 Cookie 访问管理 API，写请求需要 `X-Mio-Request: 1`。初始化完成后，自动化客户端可用 `Authorization: Bearer <ADMIN_TOKEN>`；改密码只接受登录会话。失败响应为 `{"error":"说明"}`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/auth/status` | 初始化与登录状态（公开） |
| POST | `/api/auth/setup` | 创建管理员 |
| POST | `/api/auth/login` | 登录 |
| POST | `/api/auth/logout` | 退出 |
| POST | `/api/auth/password` | 修改密码 |
| GET | `/api/config` | 限制与直链域名（公开） |
| GET | `/api/images?page=1&folder=0&q=` | 列表；`folder` 不传为全部，`0` 为未分类 |
| POST | `/api/images` | 上传：`file`，可选 `folder_id` |
| PATCH | `/api/images/{id}` | `{"folder_id":1}`，`null` 表示未分类 |
| DELETE | `/api/images/{id}` | 永久删除 |
| GET / POST | `/api/folders` | 列出 / 新建 |
| PATCH | `/api/folders/{id}` | 重命名 |
| DELETE | `/api/folders/{id}` | 删除文件夹，保留图片 |
| GET | `/i/{id}` | 原图直链 |
| GET | `/download/{id}` | 按原文件名下载 |
