# clicrontab MCP Server 实现计划

## 1. 目标概述

将 clicrontab 改造为支持 MCP (Model Context Protocol) 的服务，让 AI 大模型（如 Claude Code）可以通过 MCP 协议创建和管理定时任务。

### 核心设计

- **MCP Tool 接收 `prompt` 参数**，后台自动拼接成 `claude -p "prompt"` 命令执行
- **保留 cron 调度**：所有任务使用标准 5 字段 cron 表达式
- **动态 working_dir**：支持在创建任务和立即执行时指定工作目录
- **可扩展**：未来可支持其他 AI CLI 工具

---

## 2. 架构设计

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Claude Code / AI 大模型                   │
└─────────────────────┬───────────────────────────────────────┘
                      │ MCP Protocol (stdio)
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                     clicrontab                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ MCP Server  │  │ HTTP Server │  │ Scheduler (cron)    │  │
│  │ (stdio)     │  │ (Web UI)    │  │                     │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │             │
│         └────────────────┴─────────────────────┘             │
│                          │                                   │
│              ┌───────────┴───────────┐                       │
│              │  Core (Store/Executor) │                       │
│              └───────────────────────┘                       │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 运行模式

| 模式 | 命令 | 说明 |
|------|------|------|
| HTTP | `./clicrontabd --mode http` | 仅 HTTP + Web UI（默认） |
| MCP | `./clicrontabd --mode mcp` | 仅 MCP Server (stdio) |
| Both | `./clicrontabd --mode both` | 同时运行 HTTP 和 MCP |

### 2.3 命令拼接逻辑

用户提交 `prompt`，后台拼接为完整命令：

```go
func buildClaudeCommand(prompt string) string {
    // 转义 prompt 中的特殊字符
    escaped := shellescape.Quote(prompt)
    return fmt.Sprintf("claude -p %s --output-format json --dangerously-skip-permissions", escaped)
}
```

---

## 3. 数据模型变更

### 3.1 Task 结构修改

```go
// internal/core/types.go
type Task struct {
    ID             string
    Name           *string
    Prompt         string     // 新增：用户提交的 prompt
    Command        string     // 保留：后台拼接的完整命令
    Cron           string
    WorkingDir     *string
    TimeoutSeconds *int
    Status         TaskStatus
    LastRunAt      *time.Time
    NextRunAt      *time.Time
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

### 3.2 数据库迁移

新增 `prompt` 字段：

```sql
-- migrations/0003_add_prompt.sql
ALTER TABLE tasks ADD COLUMN prompt TEXT;
```

---

## 4. MCP Tools 设计

### 4.1 Tool 列表

| Tool 名称 | 功能 | 必填参数 | 可选参数 |
|-----------|------|----------|----------|
| `cron_create_task` | 创建定时任务 | prompt, cron, working_dir | name, timeout_minutes |
| `cron_list_tasks` | 列出所有任务 | - | status |
| `cron_get_task` | 获取任务详情 | task_id | - |
| `cron_update_task` | 更新任务 | task_id | prompt, cron, working_dir, paused |
| `cron_delete_task` | 删除任务 | task_id | - |
| `cron_run_task` | 立即执行 | task_id | working_dir (覆盖) |
| `cron_list_runs` | 运行历史 | task_id | limit |
| `cron_get_run_log` | 获取日志 | run_id | tail |
| `cron_preview` | 预览触发时间 | cron_expr | count |

### 4.2 Tool 参数定义

#### cron_create_task

```json
{
  "name": "cron_create_task",
  "description": "创建一个定时执行 Claude 命令的任务",
  "inputSchema": {
    "type": "object",
    "required": ["prompt", "cron", "working_dir"],
    "properties": {
      "name": {
        "type": "string",
        "description": "任务名称（可选）"
      },
      "prompt": {
        "type": "string",
        "description": "要执行的 Claude prompt"
      },
      "cron": {
        "type": "string",
        "description": "Cron 表达式（5 字段：分 时 日 月 周）"
      },
      "working_dir": {
        "type": "string",
        "description": "命令执行的工作目录"
      },
      "timeout_minutes": {
        "type": "integer",
        "description": "超时时间（分钟），默认 30"
      }
    }
  }
}
```

#### cron_run_task

```json
{
  "name": "cron_run_task",
  "description": "立即执行指定任务",
  "inputSchema": {
    "type": "object",
    "required": ["task_id"],
    "properties": {
      "task_id": {
        "type": "string",
        "description": "任务 ID"
      },
      "working_dir": {
        "type": "string",
        "description": "临时覆盖工作目录（可选）"
      }
    }
  }
}
```

---

## 5. 文件结构变更

### 5.1 新增文件

```
internal/
├── mcp/
│   ├── server.go           # MCP Server 主逻辑
│   ├── tools.go             # Tool 定义和 Handler
│   ├── types.go             # MCP 相关类型
│   └── command_builder.go   # 命令拼接逻辑
```

### 5.2 修改文件

| 文件 | 修改内容 |
|------|----------|
| `cmd/clicrontabd/main.go` | 添加 `--mode` 参数，支持 MCP 模式启动 |
| `internal/config/config.go` | 新增 `Mode` 配置项 |
| `internal/core/types.go` | Task 结构添加 `Prompt` 字段 |
| `internal/core/executor.go` | 支持 `ExecuteOptions`（working_dir 覆盖） |
| `internal/core/scheduler.go` | `RunTaskNow` 支持 working_dir 覆盖参数 |
| `internal/store/tasks_repo.go` | 支持 `prompt` 字段的增删改查 |
| `internal/store/sqlite.go` | 添加新迁移 |
| `go.mod` | 添加 MCP SDK 依赖 |

---

## 6. 实现步骤

### Step 1: 添加依赖

```bash
go get github.com/mark3labs/mcp-go
```

### Step 2: 修改配置

**文件**: `internal/config/config.go`

```go
type Config struct {
    // ... existing fields
    Mode string  // "http", "mcp", "both"
}

// 添加 --mode 参数
flag.StringVar(&cfg.Mode, "mode", "http", "Run mode: http, mcp, or both")
```

### Step 3: 数据库迁移

**文件**: `internal/store/migrations/0003_add_prompt.sql`

```sql
ALTER TABLE tasks ADD COLUMN prompt TEXT;
```

**更新** `internal/store/sqlite.go` 注册新迁移。

### Step 4: 修改 Task 结构

**文件**: `internal/core/types.go`

```go
type Task struct {
    // ... existing fields
    Prompt string  // 新增
}
```

### Step 5: 实现命令拼接

**文件**: `internal/mcp/command_builder.go`

```go
package mcp

import "github.com/alessio/shellescape"

// BuildClaudeCommand 根据 prompt 构建完整的 claude 命令
func BuildClaudeCommand(prompt string) string {
    return fmt.Sprintf("claude -p %s --output-format json --dangerously-skip-permissions",
        shellescape.Quote(prompt))
}
```

### Step 6: 实现 MCP Server

**文件**: `internal/mcp/server.go`

```go
package mcp

import (
    "context"
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

type MCPServer struct {
    store     *store.Store
    scheduler *core.Scheduler
    logger    *slog.Logger
    location  *time.Location
}

func NewMCPServer(store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location) *MCPServer {
    return &MCPServer{
        store:     store,
        scheduler: scheduler,
        logger:    logger,
        location:  location,
    }
}

func (s *MCPServer) Run() error {
    mcpServer := server.NewMCPServer(
        "clicrontab",
        "1.0.0",
        server.WithToolCapabilities(true),
    )

    // 注册 Tools
    s.registerTools(mcpServer)

    // 启动 stdio server
    return server.ServeStdio(mcpServer)
}
```

### Step 7: 实现 Tools Handler

**文件**: `internal/mcp/tools.go`

```go
package mcp

func (s *MCPServer) registerTools(mcpServer *server.MCPServer) {
    // cron_create_task
    mcpServer.AddTool(mcp.NewTool("cron_create_task",
        mcp.WithDescription("创建定时执行 Claude 命令的任务"),
        mcp.WithString("name", mcp.Description("任务名称")),
        mcp.WithString("prompt", mcp.Required(), mcp.Description("要执行的 Claude prompt")),
        mcp.WithString("cron", mcp.Required(), mcp.Description("Cron 表达式")),
        mcp.WithString("working_dir", mcp.Required(), mcp.Description("工作目录")),
        mcp.WithNumber("timeout_minutes", mcp.Description("超时时间（分钟）")),
    ), s.handleCreateTask)

    // cron_list_tasks
    mcpServer.AddTool(mcp.NewTool("cron_list_tasks",
        mcp.WithDescription("列出所有定时任务"),
        mcp.WithString("status", mcp.Description("过滤状态: active/paused")),
    ), s.handleListTasks)

    // cron_run_task
    mcpServer.AddTool(mcp.NewTool("cron_run_task",
        mcp.WithDescription("立即执行指定任务"),
        mcp.WithString("task_id", mcp.Required(), mcp.Description("任务 ID")),
        mcp.WithString("working_dir", mcp.Description("临时覆盖工作目录")),
    ), s.handleRunTask)

    // ... 其他 tools
}

func (s *MCPServer) handleCreateTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    prompt := request.Params.Arguments["prompt"].(string)
    cronExpr := request.Params.Arguments["cron"].(string)
    workingDir := request.Params.Arguments["working_dir"].(string)

    // 验证 cron 表达式
    schedule, err := core.ParseCron(cronExpr)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("无效的 cron 表达式: %v", err)), nil
    }

    // 构建完整命令
    command := BuildClaudeCommand(prompt)

    // 创建任务
    task := &core.Task{
        ID:         core.NewID(),
        Prompt:     prompt,
        Command:    command,
        Cron:       cronExpr,
        WorkingDir: &workingDir,
        Status:     core.TaskStatusActive,
    }

    // ... 保存并调度
}
```

### Step 8: 修改 Executor 支持 working_dir 覆盖

**文件**: `internal/core/executor.go`

```go
type ExecuteOptions struct {
    WorkingDirOverride *string
}

func (e *CommandExecutor) Execute(ctx context.Context, task *Task, run *Run, opts *ExecuteOptions) error {
    // 确定实际工作目录
    workingDir := task.WorkingDir
    if opts != nil && opts.WorkingDirOverride != nil && *opts.WorkingDirOverride != "" {
        workingDir = opts.WorkingDirOverride
    }

    // ... 其余逻辑不变
}
```

### Step 9: 修改 main.go 支持多模式

**文件**: `cmd/clicrontabd/main.go`

```go
func main() {
    cfg, err := config.Parse()
    // ...

    switch cfg.Mode {
    case "http":
        runHTTPServer(cfg, storeInst, scheduler, logger, location)
    case "mcp":
        runMCPServer(storeInst, scheduler, logger, location)
    case "both":
        go runHTTPServer(cfg, storeInst, scheduler, logger, location)
        runMCPServer(storeInst, scheduler, logger, location)
    }
}

func runMCPServer(store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location) {
    mcpServer := mcp.NewMCPServer(store, scheduler, logger, location)
    if err := mcpServer.Run(); err != nil {
        logger.Error("mcp server error", "err", err)
    }
}
```

---

## 7. Claude Code 配置

### 7.1 settings.json

```json
{
  "mcpServers": {
    "clicrontab": {
      "command": "/path/to/clicrontabd",
      "args": ["--mode", "mcp", "--state-dir", "~/.local/state/clicrontab"]
    }
  }
}
```

### 7.2 使用示例

```
用户: 创建一个每天早上 9 点执行代码审查的任务

Claude: 我来创建这个定时任务。

[调用 cron_create_task]
{
  "name": "daily-code-review",
  "prompt": "审查 src/ 目录下最近 24 小时内修改的代码，检查潜在问题并生成报告",
  "cron": "0 9 * * *",
  "working_dir": "/home/user/my-project",
  "timeout_minutes": 30
}

任务已创建成功！
- 任务 ID: task_abc123
- 下次执行: 2025-12-31 09:00:00
- 工作目录: /home/user/my-project
```

---

## 8. 测试清单

### 8.1 单元测试

- [ ] `BuildClaudeCommand` 正确转义特殊字符
- [ ] `ParseCron` 拒绝 `@` 宏
- [ ] Task 的 `Prompt` 字段正确存取

### 8.2 集成测试

- [ ] MCP Server 启动并响应 `tools/list`
- [ ] `cron_create_task` 创建任务并正确调度
- [ ] `cron_run_task` 立即执行并返回 run_id
- [ ] `cron_run_task` 支持 working_dir 覆盖
- [ ] 定时触发正确执行 `claude -p` 命令
- [ ] 任务在运行中时返回 conflict 错误

### 8.3 端到端测试

- [ ] Claude Code 可以通过 MCP 调用所有 Tools
- [ ] 创建的任务按时执行
- [ ] 日志正确记录命令输出

---

## 9. 未来扩展

### 9.1 支持其他 AI CLI

```go
// internal/mcp/command_builder.go
type CommandBuilder interface {
    Build(prompt string) string
}

var builders = map[string]CommandBuilder{
    "claude": &ClaudeBuilder{},
    // "gemini": &GeminiBuilder{},
    // "gpt": &GPTBuilder{},
}
```

### 9.2 配置文件支持

```yaml
# ~/.config/clicrontab/config.yaml
default_engine: claude
engines:
  claude:
    command: "claude -p {{.prompt}} --output-format json"
  custom:
    command: "my-ai-cli run {{.prompt}}"
```

---

## 10. 依赖清单

| 依赖 | 版本 | 用途 |
|------|------|------|
| github.com/mark3labs/mcp-go | latest | MCP SDK |
| github.com/alessio/shellescape | latest | Shell 转义 |

---

## 11. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| prompt 注入攻击 | 高 | 使用 shellescape 严格转义 |
| 长时间运行任务 | 中 | 设置合理超时，支持取消 |
| API 费用失控 | 中 | 支持 `--max-budget-usd` 参数 |
| 并发执行冲突 | 低 | 保留现有不可重入机制 |
