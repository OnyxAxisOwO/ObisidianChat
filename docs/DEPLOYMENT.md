# 部署

## 单程序

构建命令见 README。创建可写的数据目录，给运行账号该目录的读写权限。运行账号不需要管理员/root 权限。默认仅监听 `127.0.0.1:8090`。

对外服务时使用 HTTPS 反向代理，设置：

```text
OC_ADDR=127.0.0.1:8090
OC_DATABASE=/var/lib/obsidianchat/chat.db
OC_ORIGIN=https://chat.example.com
OC_SECURE_COOKIE=true
```

Nginx 示例，TLS 证书配置由现有网关负责：

```nginx
location / {
    proxy_pass http://127.0.0.1:8090;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_read_timeout 75s;
    client_max_body_size 16k;
}
```

`/api/events` 必须允许流式响应，不进行缓存、缓冲或 gzip。仅静态 JS/CSS/HTML 可启用 gzip。不要信任来自互联网的 `X-Forwarded-For` 来识别限流来源；当前服务使用直接连接 IP。反代部署时登录限流会共享网关 IP，需要网关另设按真实客户端限流，并按业务需要调整应用登录速率。

首次启动从程序输出读取初始化令牌，在 HTTPS 页面创建管理员；完成后可移除 `OC_SETUP_TOKEN`。开放注册默认开启，可在管理后台关闭。

## Docker

```sh
docker build -t obsidianchat .
docker volume create obsidianchat-data
docker run -d --name obsidianchat \
  -p 127.0.0.1:8090:8090 \
  -v obsidianchat-data:/data \
  -e OC_ORIGIN=https://chat.example.com \
  -e OC_SECURE_COOKIE=true \
  --restart unless-stopped obsidianchat
docker logs obsidianchat
```

镜像以 UID 10001 运行。挂载宿主机目录时，需预先给该 UID 读写权限。当前本机没有 Docker，镜像构建和启动检查由提供的 CI 执行，尚未取得远端运行结果。

## 数据备份

最简单的方式是停止程序后备份整个数据目录，再启动。不要在运行中只复制 `chat.db` 而忽略 WAL；在线备份请使用 SQLite backup API 或 `VACUUM INTO`，并验证备份可以打开。

恢复时停止程序、替换完整备份，再启动。每次升级前备份数据。当前为首版 schema，`CREATE TABLE IF NOT EXISTS` 仅用于初始建表，不代表未来所有版本都能自动迁移。

`GET /healthz` 返回数据库连接健康状态；`/api/admin/stats` 需管理员会话，提供堆内存、协程、连接和慢连接断开数。收到 SIGTERM / Ctrl+C 时先退出 SSE，再等待 HTTP 请求结束，最长 10 秒。
