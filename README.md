# Mio 图床

单用户轻量图床，**Go + SQLite + Vue 3**。管理界面编译后嵌入同一个可执行文件，图片存在本地磁盘，不需要单独的数据库或 Node.js 运行时。

- **上传图片**：选择文件夹、拖拽或多选上传、查看进度、复制 URL 或 Markdown。
- **文件夹管理**：搜索、缩略图列表、预览原图、分页；创建或重命名文件夹；移动、下载和删除图片。
- **系统设置**：浅色 / 深色 / 跟随系统，默认分享格式，ShareX 截图上传，查看用量，修改密码。

格式：JPG / PNG / GIF / WebP，单张不超过 20 MB、3200 万像素，不接收 SVG。管理需要登录；`/i/{id}`、`/t/{id}` 和 `/download/{id}` 公开访问。列表网格走 JPEG 缩略图，点开预览和分享链接仍是原图。单层文件夹，移动图片不会改变直链。删除文件夹后图片回到未分类。

## 部署

用 Docker 即可：

```sh
git clone https://github.com/amazonmio/Mio-Image-Hosting.git
cd Mio-Image-Hosting
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

## 配置

通过环境变量配置运行参数，不会读取 `.env`。

| 变量 | 默认 | 用途 |
| --- | --- | --- |
| `ADDR` | `127.0.0.1:8080` | 监听地址。容器内必须是 `0.0.0.0:8080` |
| `DATA_DIR` | `data` | 数据目录；生产建议用绝对路径 |
| `PUBLIC_BASE_URL` | 空 | 直链根地址，例如 `https://img.example.com` |
| `ADMIN_TOKEN` | 空 | 可选。给 ShareX 等自动化客户端做 Bearer 鉴权，网页仍走账号登录 |

## ShareX

截图后可以不打开网页，直接传到本图床。结果和网页上传一样，只是省去登录和选文件。ShareX 走 `ADMIN_TOKEN`，不使用管理员密码。

1. 设置环境变量 `ADMIN_TOKEN`（用足够长的随机串），建议同时设置 `PUBLIC_BASE_URL`。
2. 重启服务。
3. 登录后台打开系统设置，粘贴 Token 后下载 `.sxcu`；也可以复制仓库里的 `sharex/mio-image-hosting.sxcu`，把 RequestURL、URL、ThumbnailURL 中的域名和 Token 改成自己的。
4. ShareX：目标 → 自定义上传器设置 → 导入，再设为默认图片上传器。

不要把 Token 发给别人或写进公开仓库。网页登录上传仍然可用。

## 站点外观

首次启动会在数据目录生成这些文件（Docker 容器内是 `/data`）：

```text
config.yaml     站点名称
avatar.webp     侧栏和登录页头像
favicon.webp    浏览器标签页图标
```

默认头像是内置这张。更换头像或标签页图标时，用自己的图片**覆盖**对应文件（不要放进 `uploads/`），然后重启服务。单个不超过 2 MB。

站点名称改 `config.yaml`：

```yaml
site_name: Mio 图床
```

若文件不是 webp，可改名后在配置里指定，支持 png、jpg、webp、gif、ico：

```yaml
site_name: Mio 图床
avatar: avatar.png
favicon: favicon.ico
```

## 备份

```text
DATA_DIR/config.yaml    站点名称
DATA_DIR/avatar.webp    头像
DATA_DIR/favicon.webp   标签页图标
DATA_DIR/app.db         SQLite 元数据
DATA_DIR/uploads/       图片原文件
DATA_DIR/thumbs/        列表缩略图（丢失后会按需再生成）
DATA_DIR/quarantine/    未登记或冲突文件的隔离副本
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

打开 Vite 给出的地址（默认 http://127.0.0.1:5173）。`/api`、`/i/`、`/t/`、`/download/`、`/branding` 会代理到 8080。请固定用 `127.0.0.1` 或 `localhost` 其中一种，不要混用。

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
| GET | `/api/config` | 限制、直链域名、站点名称与图标（公开） |
| GET | `/api/images?page=1&folder=0&q=` | 列表；`folder` 不传为全部，`0` 为未分类 |
| POST | `/api/images` | 上传：`file`，可选 `folder_id` |
| PATCH | `/api/images/{id}` | `{"folder_id":1}`，`null` 表示未分类 |
| DELETE | `/api/images/{id}` | 永久删除 |
| GET / POST | `/api/folders` | 列出 / 新建 |
| PATCH | `/api/folders/{id}` | 重命名 |
| DELETE | `/api/folders/{id}` | 删除文件夹，保留图片 |
| GET | `/i/{id}` | 原图直链 |
| GET | `/t/{id}` | 列表缩略图（长边约 480，JPEG） |
| GET | `/download/{id}` | 按原文件名下载 |

## 上传完整性与读取期限

图片入库前会完整解码 JPG、PNG、WebP；GIF 会检查并解码全部帧。保留原文件，不重新压缩。单图最多 3200 万像素，GIF 最多 500 帧且所有帧累计不超过 3200 万像素。每个服务实例一次只进行一个解码校验，繁忙时返回 503，可稍后重试。像素存储预算约 256 MB，实际进程内存还包含压缩数据、解码器和运行时开销。

请求头最多读取 10 秒；普通 API 请求体最多 15 秒，图片上传请求体最多 4 分钟。读取超时返回 408，提示重试。前端上传总超时仍为 5 分钟；该读取期限不限制下载时间，也不是图像解码的 CPU 超时。

## 恢复与停机行为

启动时，数据库未登记的原文件、未完成上传和无法确定归属的删除残留会移动到 `DATA_DIR/quarantine/时间-随机编号/`，保留原文件名并记录日志，不自动删除。该目录不会通过图片接口公开。请先备份，再人工核对；恢复旧数据库时可将对应批次里的原图复制回 `uploads/`，同时恢复匹配的元数据。仅复制图片而不恢复元数据，仍会在下次启动时被隔离。隔离文件会占用磁盘，确认无需保留后再手动清理。

若同一图片的原文件和 `.trash` 副本同时存在，会保留原文件并隔离冲突副本。服务收到退出信号后停止接受新连接，等待当前请求完成，最长 15 秒，之后强制关闭仍未完成的连接。Docker Compose 提供 20 秒的停止宽限期。

品牌文件只接受数据目录内的普通图片文件，不接受符号链接或指向数据库、配置和初始化密钥的硬链接。启动时会校验真实图片内容，并从只读内存副本提供头像和图标；运行中替换文件不会直接改变对外响应，重启后重新校验生效。响应 Content-Type 根据实际图片格式确定。ICO 支持 PNG 以及标准 Windows DIB 帧，最多 64 帧。

### 缩略图并发与 ShareX 链接

同一张图片的缩略图生成、缓存读取和删除通过图片级锁协调：并发请求复用首次生成的缓存，删除会等待该图片的生成任务结束后清理原图和缓存。不同图片使用独立锁，空闲锁会回收。已有大图按需生成缩略图时也会重新检查像素上限。

新导出的 ShareX 配置使用上传地址的域名与响应中的图片 ID 生成完整直链，不依赖响应 URL 是否为相对路径。未设置 `PUBLIC_BASE_URL` 时，本机访问会生成带端口的本机链接；公网分享仍需配置可公开访问的地址。已有 ShareX 配置请重新导出并导入。
