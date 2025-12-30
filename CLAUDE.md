# CLAUDE.md

此文件为 Claude Code (claude.ai/code) 提供在此代码仓库中工作的指导。

## 项目概述

clicrontab 是一个轻量级本地定时任务调度器，具有极简的 Web 界面和 HTTP API。专为个人或小团队使用而设计，注重简单性和安全性。

**核心原则：**
- 仅支持标准 5 字段 cron 表达式 (`min hour dom mon dow`)
- 不可重入：如果任务仍在运行，新的触发将被跳过
- 默认仅本地访问 (127.0.0.1)
- 单二进制分发，包含嵌入式 Web 资源

## 常用开发命令

```bash
# 构建应用程序
go build ./cmd/clicrontabd

# 运行应用程序 (默认: http://127.0.0.1:7070)
./clicrontabd

# 使用 go run 直接运行（开发者常用）
go run cmd/clicrontabd/main.go

# 使用自定义选项运行
./clicrontabd --addr 0.0.0.0:8080 --state-dir ~/.local/state/clicrontab --log-level info

# 或者使用环境变量
CLICRON_ADDR=0.0.0.0:8080 CLICRON_LOG_LEVEL=debug ./clicrontabd

# 安装依赖项
go mod download

# 更新依赖项
go mod tidy

# 格式化代码
go fmt ./...

# 检查问题
go vet ./...
```

## 架构概览

代码库遵循 Clean Architecture 原则，职责分离清晰：

```
cmd/clicrontabd/          # 应用程序入口点
internal/
├── api/                  # HTTP API 层 (处理器, 路由)
├── core/                 # 业务逻辑 (调度器, 执行器, 类型)
├── store/                # 数据持久化 (SQLite, 仓库)
├── config/               # 配置管理
└── logging/              # 日志设置
web/                      # 前端资源 (HTML/JS/CSS)
api/                      # API 文档 (OpenAPI)
```

**核心组件：**

1. **调度器** (`internal/core/scheduler.go`): 管理基于 cron 的任务调度，处理不可重入逻辑，更新 `next_run_at` 时间戳。

2. **执行器** (`internal/core/executor.go`): 在子进程中执行命令，处理超时，合并 stdout/stderr，管理日志文件。

3. **API 处理器** (`internal/api/handlers_*.go`): 任务 CRUD、立即执行、运行历史和日志流式传输的 RESTful 端点。

4. **存储** (`internal/store/`): 基于 SQLite 的持久化，任务和运行有单独的仓库，包含迁移支持。

## 关键设计决策

- **Cron 解析器**: 使用 `robfig/cron/v3` 自定义 5 字段解析器，明确拒绝 `@` 宏
- **数据库**: SQLite 配合纯 Go 驱动 (`modernc.org/sqlite`) 用于单文件部署
- **Web 框架**: `go-chi/chi` 用于路由，但总体依赖最少
- **静态资源**: 使用 Go 1.16+ `embed` 功能嵌入，实现单二进制分发
- **时间处理**: 所有 API 时间为 RFC3339 UTC；前端转换为本地时区
- **日志保留**: 默认仅保留每个任务最近 20 次运行 (可配置)

## API 模式

所有 API 响应遵循一致的模式：
- 成功: 适当的 HTTP 状态 (200, 201 等) 与 JSON 主体
- 错误: 4xx/5xx 状态与 JSON 主体 `{error: {code, message}}`
- 时间: 始终 RFC3339 UTC 格式 (例如 `2025-03-01T02:00:00Z`)

常见错误代码：
- `invalid_input`: 缺少必填字段或无效值
- `invalid_cron`: cron 表达式格式错误或包含 `@` 宏
- `conflict`: 尝试立即执行时任务已在运行
- `not_found`: 任务或运行不存在

## 测试方法

目前不存在自动化测试。添加测试时：
- 核心业务逻辑的单兀测试 (调度器, 执行器)
- API 端点的集成测试
- 测试 cron 表达式验证和下次运行计算
- 验证不可重入行为和超时处理

## 安全考虑

- 默认仅绑定到 127.0.0.1 (无外部访问)
- 命令通过 `/bin/sh -c` (Unix) 或 `cmd /C` (Windows) 执行
- MVP 中无内置认证 (可通过中间件添加)
- SQLite 数据库存储在用户状态目录中
- 日志文件包含来自已执行命令的合并 stdout/stderr