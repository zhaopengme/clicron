# clicrontab

轻量级本地定时任务调度器，具有极简 Web 界面和 HTTP API。专为个人或小团队使用而设计，注重简单性和安全性。

## 特性

- **单二进制分发**：前端资源通过 Go embed 嵌入，无需额外文件
- **标准 Cron 表达式**：仅支持 5 字段格式 (`分 时 日 月 周`)，明确拒绝 `@` 宏
- **不可重入保护**：任务运行中时，新触发自动跳过
- **安全默认值**：仅绑定 localhost (127.0.0.1)
- **优雅关闭**：等待运行中任务完成

## 快速开始

### 构建

```bash
go build ./cmd/clicrontabd
```

### 运行

```bash
# 默认配置 (http://127.0.0.1:7070)
./clicrontabd

# 自定义配置
./clicrontabd --addr 127.0.0.1:8080 --state-dir ~/.local/state/clicrontab --log-level debug
```

### 开发模式

```bash
go run cmd/clicrontabd/main.go
```

## 架构概览

```
clicron/
├── cmd/clicrontabd/main.go      # 应用入口
├── internal/
│   ├── api/                      # HTTP API 层
│   │   ├── router.go             # 路由和服务器
│   │   ├── handlers_tasks.go     # 任务 CRUD
│   │   ├── handlers_runs.go      # 运行记录
│   │   └── handlers_cron.go      # Cron 预览
│   ├── core/                     # 核心业务逻辑
│   │   ├── scheduler.go          # 调度器
│   │   ├── executor.go           # 执行器
│   │   ├── cron.go               # Cron 解析
│   │   ├── types.go              # 领域类型
│   │   └── id.go                 # ID 生成
│   ├── store/                    # 数据持久化
│   │   ├── sqlite.go             # SQLite 连接
│   │   ├── tasks_repo.go         # 任务仓库
│   │   ├── runs_repo.go          # 运行仓库
│   │   └── migrations/           # 数据库迁移
│   ├── config/                   # 配置管理
│   └── logging/                  # 日志设置
├── web/                          # 前端资源
│   ├── index.html
│   ├── app.js
│   ├── styles.css
│   └── embed.go                  # Go embed
└── api/openapi.yaml              # API 文档
```

## 核心组件

### 1. 调度器 (Scheduler)

**文件**: `internal/core/scheduler.go`

调度器是应用的核心，负责管理基于 cron 的任务调度：

```go
type Scheduler struct {
    store    Store                    // 数据库接口
    executor Executor                 // 命令执行器
    cron     *cron.Cron               // robfig/cron 实例
    entries  map[string]cron.EntryID  // taskID -> cron entry 映射
    running  sync.Map                 // 跟踪运行中的任务
}
```

**关键特性**：
- **不可重入机制**：如果任务仍在运行，新触发会被跳过（状态标记为 `skipped`）
- **动态调度**：支持运行时添加/更新/删除任务
- **同步机制**：启动时从数据库加载所有活跃任务

**主要方法**：
| 方法 | 说明 |
|------|------|
| `Start(ctx)` | 启动 cron 调度循环 |
| `Stop()` | 优雅停止调度器 |
| `Sync(ctx)` | 从数据库加载并调度所有活跃任务 |
| `AddOrUpdateTask(ctx, task)` | 更新任务调度 |
| `RemoveTask(taskID)` | 取消任务调度 |
| `RunTaskNow(ctx, task)` | 立即执行任务 |

### 2. 执行器 (Executor)

**文件**: `internal/core/executor.go`

负责实际的命令执行：

**执行流程**：
1. 使用用户的 `$SHELL -l -c` 执行命令（登录 shell）
2. 支持自定义工作目录
3. 超时处理：先发 SIGTERM，5 秒后强制 kill
4. 输出捕获：写入日志文件，内存保留最后 8KB

**状态转换**：
```
queued → running → succeeded/failed/timed_out/canceled
```

### 3. Cron 解析器

**文件**: `internal/core/cron.go`

- 使用 `robfig/cron/v3` 自定义 5 字段解析器
- **明确拒绝 `@` 宏**（如 `@daily`、`@hourly`）
- 仅支持标准格式：`分 时 日 月 周`

**示例**：
```
0 2 * * *      # 每天凌晨 2 点
*/15 * * * *   # 每 15 分钟
0 9 * * 1-5    # 工作日早上 9 点
```

## 数据库设计

使用 SQLite（`modernc.org/sqlite` 纯 Go 驱动），无需 CGO。

### Tasks 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT | 主键 |
| name | TEXT | 任务名称 |
| command | TEXT | 执行命令 |
| cron | TEXT | Cron 表达式 |
| timeout_seconds | INTEGER | 超时时间（秒） |
| working_dir | TEXT | 工作目录 |
| status | TEXT | active/paused |
| last_run_at | TEXT | 上次运行时间 |
| next_run_at | TEXT | 下次运行时间 |
| created_at | TEXT | 创建时间 |
| updated_at | TEXT | 更新时间 |

### Runs 表

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT | 主键 |
| task_id | TEXT | 关联任务 ID |
| status | TEXT | 运行状态 |
| exit_code | INTEGER | 退出码 |
| started_at | TEXT | 开始时间 |
| finished_at | TEXT | 结束时间 |

**运行状态**：
- `queued` - 等待执行
- `running` - 正在执行
- `succeeded` - 执行成功（退出码 0）
- `failed` - 执行失败（非零退出码）
- `timed_out` - 超时终止
- `canceled` - 系统关闭时取消
- `skipped` - 因前次运行未完成而跳过

## API 设计

遵循 RESTful 风格，所有时间使用 RFC3339 UTC 格式。

### 端点列表

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/tasks` | GET | 列出所有任务 |
| `/api/tasks` | POST | 创建任务 |
| `/api/tasks/{id}` | GET | 获取任务详情 |
| `/api/tasks/{id}` | PUT | 更新任务 |
| `/api/tasks/{id}` | DELETE | 删除任务 |
| `/api/tasks/{id}/run` | POST | 立即执行（运行中返回 409） |
| `/api/tasks/{id}/runs` | GET | 获取运行历史 |
| `/api/runs/{id}` | GET | 获取运行详情 |
| `/api/runs/{id}/log` | GET | 获取运行日志 |
| `/api/cron/preview` | POST | 预览 Cron 触发时间 |

### 错误响应格式

```json
{
  "error": {
    "code": "invalid_cron",
    "message": "cron expression contains @ macro"
  }
}
```

**常见错误码**：
- `invalid_input` - 缺少必填字段或无效值
- `invalid_cron` - Cron 表达式格式错误或包含 `@` 宏
- `conflict` - 任务正在运行中（409）
- `not_found` - 任务或运行不存在

详细 API 文档请参考 [docs/api-usage.md](docs/api-usage.md)。

## 启动流程

`cmd/clicrontabd/main.go` 中的启动顺序：

```
1. 解析配置      → 命令行参数
2. 初始化日志    → 结构化日志 (slog)
3. 打开数据库    → SQLite + 迁移
4. 创建核心组件  → Executor + Scheduler
5. 同步任务      → 从数据库加载活跃任务
6. 启动 HTTP 服务 → 监听 127.0.0.1:7070
7. 优雅关闭      → 处理 SIGINT/SIGTERM
```

## 技术栈

| 组件 | 技术选型 | 说明 |
|------|----------|------|
| Web 框架 | go-chi/chi | 轻量级路由 |
| Cron 库 | robfig/cron/v3 | 自定义 5 字段解析器 |
| 数据库 | SQLite | modernc.org/sqlite 纯 Go 驱动 |
| 日志 | slog | Go 标准库 |
| 静态资源 | Go embed | 单二进制分发 |

## 配置选项

### 环境变量

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `CLICRON_ADDR` | 0.0.0.0:7070 | 监听地址 |
| `CLICRON_AUTH_TOKEN` | (空) | API 认证令牌 |
| `CLICRON_LOG_LEVEL` | info | 日志级别 (debug/info/warn/error) |
| `CLICRON_LOG_RETENTION` | 20 | 每个任务保留的运行记录数 |
| `CLICRON_STATE_DIR` | ~/.config/clicrontab | 数据目录 |
| `CLICRON_USE_UTC` | false | 使用 UTC 时区 |
| `CLICRON_SHUTDOWN_GRACE` | 5s | 关闭等待时间 |
| `CLICRON_BARK_URL` | (空) | Bark 通知 URL |
| `CLICRON_BARK_ENABLED` | false | 启用 Bark 通知 |

### 命令行参数

命令行参数优先级高于环境变量：

| 参数 | 说明 |
|------|------|
| `--addr` | 监听地址 |
| `--state-dir` | 数据目录 |
| `--log-level` | 日志级别 |
| `--use-utc` | 使用 UTC 时区 |
| `--run-log-keep` | 保留运行记录数 |
| `--shutdown-grace` | 关闭等待时间 |

### .env 文件

可以创建 `.env` 文件来配置环境变量，参考 `.env.example`：

```bash
cp .env.example .env
# 编辑 .env 文件
```

## 数据目录结构

```
~/.local/state/clicrontab/
├── db.sqlite              # SQLite 数据库
└── runs/
    └── <run_id>/
        └── combined.log   # 合并的 stdout/stderr 日志
```

## 设计亮点

1. **单二进制部署**：前端资源通过 `//go:embed` 嵌入，无需额外配置
2. **不可重入保护**：避免同一任务重复执行，防止资源竞争
3. **纯 Go SQLite**：无需 CGO，支持跨平台编译
4. **安全默认值**：仅绑定 localhost，防止未授权访问
5. **优雅关闭**：收到终止信号后等待运行中任务完成
6. **日志保留策略**：默认保留每个任务最近 20 次运行记录

## 待改进

- [ ] 添加自动化测试（单元测试、集成测试）
- [ ] 可选的认证机制
- [ ] 更灵活的日志保留配置
- [ ] 任务分组/标签功能
- [ ] 执行统计和监控

## 许可证

MIT License
