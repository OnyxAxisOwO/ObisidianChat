# 当前部署

最近部署时间：2026-09-10 16:53。首次部署使用独立数据目录，未上传本机的用户和聊天数据。

| 项目 | 当前值 |
| --- | --- |
| 访问地址 | https://chat.onyxaxis.org/ |
| Cloudflare 模式 | 灵活（Flexible），按用户要求 |
| 回源链路 | Cloudflare → HTTP 80 Caddy → `127.0.0.1:8090` |
| 系统 | Debian 13，Linux amd64，4 个逻辑 CPU |
| 运行账号 | `obsidianchat`，禁止交互登录 |
| systemd 单元 | `/etc/systemd/system/obsidianchat.service` |
| 环境配置 | `/etc/obsidianchat/server.env`，仅 root 可读 |
| 数据目录 | `/data/obsidianchat`，运行账号独占 |
| 程序入口 | `/opt/obsidianchat/current/obsidianchat` |
| 当前版本目录 | `/opt/obsidianchat/releases/202609101655` |
| Linux 程序大小 | 11,325,600 B |
| 程序 SHA256 | `284b063f435c45359ac9420b49c7f25baeb5162cdf53b7e888cdd78a9c1a22ca` |
| 防火墙 | 已撤销 TCP 8090 公网规则；应用仅监听回环地址 |
| 启动策略 | 开机启动，失败后 2 秒重启 |
| 资源边界 | Go 软内存限制 192 MiB，systemd MemoryHigh 256 MiB / MemoryMax 512 MiB，文件描述符 65,536 |

现有 OA 的 Docker 容器和 Caddy 站点段未修改；其 8080 后端检查正常。2026-09-10 新增 `http://chat.onyxaxis.org` 站点段，见 `chat-flexible.caddy`。聊天域名不在源站签发 TLS 证书，使用 Cloudflare 灵活模式。`OC_ORIGIN=https://chat.onyxaxis.org`，`OC_SECURE_COOKIE=true` 对应浏览器到 Cloudflare 的 HTTPS 连接。

仅当 `X-Forwarded-Proto: http` 时跳转到浏览器 HTTPS 地址；Cloudflare 传来的 HTTPS 访问经 HTTP 回源后直接提供内容，避免自重定向。反向代理立即刷新流式输出。此次变更的备份：`/etc/caddy/Caddyfile.before-chat-flexible-20260910100741` 和 `/etc/obsidianchat/server.env.before-flexible-20260910100741`。

## 验证结果

- 服务器上执行 Linux 版本的全部 7 项后端测试，全部通过。
- 服务器临时数据库的 1,000 SSE 连接测试通过：100 条消息完成 10,000 次接收，无慢连接丢弃。
- 本轮并行写入窗口 173.24 ms，约 577.2 条/秒；POST p50 / p95 / p99 为 53.06 / 133.64 / 150.12 ms。客户端和服务端处于同一个测试进程，测试进程堆约 36.33 MiB。这是短时回环测试，不代表公网延迟或持续容量。
- 外部网络访问首页、健康检查、初始化状态接口和实际动效 CSS，均正常。
- 域名接入后，经过 Cloudflare 的 HTTPS 首页、健康检查和状态接口均返回 200；API 为 `Cache-Control: no-store`、`CF-Cache-Status: DYNAMIC`，不再自重定向。
- 本机前端 6 项测试、类型检查和生产构建通过；包含 HTTP 地址没有 `crypto.randomUUID` 时的消息 ID 生成验证。

## 运维

```sh
systemctl status obsidianchat
journalctl -u obsidianchat -n 80 --no-pager
systemctl restart obsidianchat
curl -fsS http://127.0.0.1:8090/healthz
```

初始化令牌保存在 `/etc/obsidianchat/server.env`，不写入此文档或仓库。管理员通过网页首次初始化。

后续维护保持 Cloudflare 灵活模式和聊天站点的显式 `http://` 地址。不要将聊天站点改为源站强制 HTTPS，否则会使当前回源方式产生重定向循环。只有用户明确要求变更加密模式时，再迁移源站 TLS。

更新前备份数据；上传到新的版本目录并核对 SHA256，再切换 `current` 链接并重启服务。`install.sh` 提供程序版本切换和健康检查失败时的旧版本回退，但不执行数据库降级；含 schema 变更的版本需单独规划迁移和备份。

2026-09-10 16:53 更新前的数据备份位于 `/opt/obsidianchat/backups/before-202609101655.tar.gz`。
