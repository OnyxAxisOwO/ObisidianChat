# Obsidian Chat

Go + Vue 在线聊天。桌面默认左侧联系人、右侧消息；好友管理、群聊详情和账号设置以第三列展开。布局和黑白灰主题参考 Obsidian Arc。支持深色模式和移动端。

## 已实现

- 首次管理员初始化、注册、登录、退出、昵称和密码修改。
- 用户搜索、双向好友申请、接受/拒绝/撤回、解除好友、私聊。
- 群聊创建、邀请好友、移除成员、退群、群主转让、归档和恢复。
- 持久化消息、历史分页、未读数、多设备已读同步、在线状态、断线恢复、发送重试去重。
- 管理后台：用户角色和停用、群聊归档、注册开关、操作日志、运行指标。

## 运行

需要 Go 1.27、Node.js 22.12+。前端只在构建时使用 Node，运行时不需要。

```powershell
cd E:\Project\ObisidianChat\web
npm ci
npm run build
cd ..
go build -trimpath -ldflags="-s -w" -o bin/obsidianchat.exe ./cmd/obsidianchat
.\bin\obsidianchat.exe
```

打开 <http://127.0.0.1:8090>。首次启动会在终端输出 `setup_token`，填入网页初始化表单并创建管理员。没有预置账号或默认密码。后续用户可自行注册，管理员可关闭注册。

Linux 构建时把输出名改为 `bin/obsidianchat`。二进制已内嵌网页，部署只需程序和数据目录。

开发时先 `go run ./cmd/obsidianchat`，再在 `web` 目录运行 `npm run dev`。Vite 的 `/api` 代理默认指向 `127.0.0.1:8090`。

| 环境变量 | 默认值 | 作用 |
| --- | --- | --- |
| `OC_ADDR` | `127.0.0.1:8090` | HTTP 监听地址 |
| `OC_DATABASE` | `data/chat.db` | SQLite 数据文件 |
| `OC_SETUP_TOKEN` | 每次未初始化启动随机生成 | 首次管理员初始化令牌 |
| `OC_ORIGIN` | 请求自身协议和 Host | 反向代理后的公开来源，如 `https://chat.example.com` |
| `OC_SECURE_COOKIE` | `false` | HTTPS 部署时设为 `true` |

## 检查

```powershell
cd web
npm ci
npm test
npm run build
cd ..
go test ./...
go vet ./...
```

Linux CI 还执行 `go test -race ./...` 和 Docker 镜像构建/健康检查。本机没有 C 编译器，因此未运行 race 检查或 Docker；CI 配置已提供，尚未提交远端执行。

连接测试仅使用本机临时数据库和本机测试服务：

```powershell
$env:OC_LOADTEST = '1'
go test ./internal/chat -run TestLoad1000Connections -v -count=1
Remove-Item Env:OC_LOADTEST
go test ./internal/chat -run '^$' -bench BenchmarkHubFanout100 -benchmem
```

## 部署与边界

已部署实例和维护信息见 [生产部署记录](deploy/PRODUCTION.md)。

见 [部署说明](docs/DEPLOYMENT.md) 和 [架构与实测](docs/ARCHITECTURE.md)。当前面向单实例；SQLite 写入串行执行，实时连接存在进程内。不要对同一个数据库启动多个应用实例或通过负载均衡随机分流长连接。

当前支持文字消息，不包含附件、语音/视频、端到端加密、离线邮件/系统通知。消息以明文存储在本机数据库。新群成员可查看该群既有历史，退出或被移除后失去访问权限。解除好友会归档原私聊，重新成为好友时恢复历史。

群聊最多 256 人，每个用户最多 200 个会话、100 条待处理好友申请。每个账号最多 4 条实时连接、10 个登录会话；全局实时连接上限为 10,000，这是资源保护值，**不是实测承载保证**。历史每页 50 条，前端最多保留 200 条，可继续向前翻页。
