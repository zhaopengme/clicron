# 开发计划（MVP）

本地定时任务器（仅支持 5 字段 cron）+ 极简 Web 页面 + HTTP/JSON API。
聚焦三件事：任务 CRUD、按 cron 调度执行、运行日志查看。

## 范围与原则
- 只支持标准 5 字段 cron：`min hour dom mon dow`；允许 `* , - /`；不支持任何 `@...` 宏。
- 同任务不重入：到点仍在运行则记录一次 `skipped`（不排队）。
- 错过触发（服务停止/睡眠）：统一跳过，不补跑。
- 日志：合并 stdout+stderr；支持 tail、follow；保留最近 N=20 次运行。
- 安全：仅监听 `127.0.0.1`；MVP 默认无鉴权（可开关）。

## 技术选型（Go）
- 语言/版本：Go ≥ 1.21。
- HTTP 路由：`go-chi/chi`（轻量；或可退回标准库 `net/http`）。
- Cron 解析：`robfig/cron/v3` 自定义 Parser（仅 5 字段，拒绝 `@` 宏）。
- 存储：SQLite（`modernc.org/sqlite` 纯 Go 驱动），`database/sql` 直接使用。
- 日志：标准库 `log/slog`。
- 静态资源：Go `embed` 内嵌 `web/`（原生 HTML/JS/CSS，无构建步骤）。

## API（v1 概览）
- 创建任务：`POST /v1/tasks` → `{id,status,next_run_at}`
- 列表任务：`GET /v1/tasks?status=active|paused`
- 查看任务：`GET /v1/tasks/{id}`
- 更新任务：`PATCH /v1/tasks/{id}`（改 `name|command|cron|timeout_s|paused`）
- 删除任务：`DELETE /v1/tasks/{id}`
- 立即运行：`POST /v1/tasks/{id}/run`（在跑返回 409）
- 运行列表：`GET /v1/tasks/{id}/runs?limit=&offset=`
- 运行详情：`GET /v1/runs/{run_id}`
- 查看日志（合并）：`GET /v1/runs/{run_id}/log?tail=200&follow=1`（`text/plain`）
- Cron 预览：`POST /v1/cron/preview`（入参 `expr`；仅校验 5 字段，不接受 `@...`）

错误结构统一：`{error:{code,message}}`；API 时间返回 RFC3339 UTC（Z 结尾）。

## 调度与执行语义
- Cron 使用“服务时区”（默认系统本地，可切到 UTC）。
- `dom` 与 `dow` 使用“或”逻辑（任一匹配即触发）。
- 不重入：到点仍在运行 → 记录一条 `skipped`，不额外排队。
- 手动运行：不影响 `next_run_at`；在跑中返回 409。
- 超时：`timeout_s>0` 生效；先优雅终止，宽限 5s 后强制结束，标记 `timed_out`。

## 极简页面（单页，无框架）
- 入口：`GET /` 返回 `web/index.html`；同源 `fetch` 调用 `/v1/*`。
- 模块：
  - 任务列表：`name/command/cron/status/last_run/next_run`；操作“运行/暂停·恢复/删除/详情”。
  - 新建/编辑表单：`name? | command | cron | timeout_s?`；右侧展示“未来 5 次触发预览”。
  - 运行历史：最近 N 次运行；点击打开“日志窗口”，支持跟随（`follow=1`）。
- 刷新：列表每 5 秒轮询或手动刷新。
- 校验：cron 仅 5 字段；含 `@` 直接报错；`command` 必填；`timeout_s` 为非负整数。

## 文件树与说明

```
.
├─ cmd/
│  └─ clicrontabd/
│     └─ main.go                 
├─ internal/
│  ├─ api/
│  │  ├─ router.go              
│  │  ├─ handlers_tasks.go      
│  │  ├─ handlers_runs.go       
│  │  └─ handlers_cron.go       
│  ├─ core/
│  │  ├─ scheduler.go           
│  │  ├─ executor.go            
│  │  └─ types.go               
│  ├─ store/
│  │  ├─ sqlite.go              
│  │  ├─ tasks_repo.go          
│  │  ├─ runs_repo.go           
│  │  └─ migrations/
│  │     └─ 0001_init.sql       
│  ├─ config/
│  │  └─ config.go              
│  └─ logging/
│     └─ logger.go              
├─ web/
│  ├─ index.html                
│  ├─ app.js                    
│  ├─ styles.css                
│  └─ embed.go                  
├─ api/
│  └─ openapi.yaml              
├─ go.mod                       
├─ dev.md                       
└─ README.md                    
```

- `cmd/clicrontabd/main.go`：程序入口；解析启动参数，初始化存储、调度、HTTP 服务。
- `internal/api/router.go`：注册路由/中间件，挂载静态页；提供 `/v1/*` 与 `/`。
- `internal/api/handlers_tasks.go`：任务 CRUD、立即运行、暂停/恢复等处理函数。
- `internal/api/handlers_runs.go`：运行列表/详情与合并日志的 tail/follow 输出。
- `internal/api/handlers_cron.go`：cron 表达式校验与未来 N 次触发预览。
- `internal/core/scheduler.go`：基于 cron 的触发计算；不重入/跳过逻辑；推进 `next_run_at`。
- `internal/core/executor.go`：创建子进程执行命令；处理超时；合并 stdout+stderr 写入日志；上报 `Run` 状态。
- `internal/core/types.go`：领域模型（Task/Run）及状态枚举；错误码常量。
- `internal/store/sqlite.go`：SQLite 连接与启动迁移；WAL/BusyTimeout 等默认配置。
- `internal/store/tasks_repo.go`：任务的增删改查；维护 `next_run_at/last_run_at`。
- `internal/store/runs_repo.go`：运行记录的增删改查；日志文件路径管理；滚动保留最近 N 次。
- `internal/store/migrations/0001_init.sql`：建表（tasks/runs）与必要索引初始化。
- `web/embed.go`：使用 `embed` 打包静态资源供 HTTP 服务使用。
- `internal/config/config.go`：读取 `--addr/--state-dir/--log-level/--use-utc` 等配置与默认值。
- `internal/logging/logger.go`：集中化的 `slog` 初始化与日志格式设置。
- `web/index.html`：极简单页骨架（列表、表单、运行历史、日志窗口）。
- `web/app.js`：与 API 交互、DOM 更新、轮询刷新、日志流式/轮询跟随。
- `web/styles.css`：基础样式与状态色；等宽字体用于日志。
- `api/openapi.yaml`：可选，OpenAPI 草案（便于外部工具集成）。
- `go.mod`（以及待拉取依赖后的 `go.sum`）：Go 依赖管理。
- `README.md`：使用说明（安装、启动、API 概览、页面预览）。

运行期状态目录（不纳入仓库）：
- `~/.local/state/clicrontab/`（或等价平台目录）
  - `db.sqlite`：数据库文件
  - `runs/<run_id>/combined.log`：单次运行的合并日志

## 里程碑与验收
M1：项目骨架与路由（任务空实现）
- 路由/静态页可访问；`POST /v1/cron/preview` 正确校验 5 字段、拒绝 `@...`。

M2：存储与任务 CRUD
- SQLite 建表；`/v1/tasks*` 全部可用；`next_run_at` 计算正确。

M3：调度与执行
- 按 cron 触发；同任务不重入并记录 `skipped`；支持 `timeout_s`。

M4：日志与页面
- 合并日志落盘；`tail`/`follow` 可用；页面可新建/编辑/暂停/删除/运行/看日志。

M5：稳定性与打包
- 重启恢复正确；错过触发跳过；二进制单文件分发（含内嵌静态页）。

## 基础测试清单
- 创建合法 cron 任务（如：`0 2 * * *`），`next_run_at` 正确。
- 输入含 `@` 的表达式被拒（400），错误文案明确。
- 任务执行中再次到点 → 产生 `skipped` 记录。
- `POST /v1/tasks/{id}/run` 在运行中返回 409。
- `timeout_s` 生效并标记 `timed_out`，日志保留至结束。
- 日志接口 `tail` 与 `follow` 正常；关闭窗口时终止流。
- 删除任务不影响历史运行与日志可读性。
- 服务重启后任务/下次触发保持一致；错过触发不补跑。
