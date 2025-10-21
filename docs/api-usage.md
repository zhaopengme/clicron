# clicrontab API 使用手册

本手册面向希望通过 HTTP/JSON 与本地 `clicrontabd` 交互的 AI/自动化工具。所有示例均假设守护进程运行在默认地址 `http://127.0.0.1:7070`，未启用鉴权。

## 基本约定

- **协议**：HTTP/1.1 + JSON。
- **基地址**：`http://127.0.0.1:7070`.
- **版本前缀**：所有 API 均挂载在 `/v1`。
- **鉴权**：MVP 默认不要求；若启用 Bearer Token，请在 Header 里附加 `Authorization: Bearer <token>`。
- **时间格式**：统一使用 RFC3339 UTC（例如 `2025-03-01T02:00:00Z`）。UI 会再按本地时区展示。
- **错误返回**：HTTP 状态码 + JSON 结构

```json
{
  "error": {
    "code": "invalid_input",
    "message": "cron expression is required"
  }
}
```

## 任务相关端点

### 创建任务

- `POST /v1/tasks`
- 用于注册一个基于 5 字段 cron 的任务，默认立即启用。

请求体：

```json
{
  "name": "Build Docs",
  "command": "bash -lc 'make docs'",
  "cron": "0 2 * * *",
  "timeout_s": 1800,
  "paused": false
}
```

字段说明：

| 字段 | 类型 | 说明 |
| ---- | ---- | ---- |
| `name` | string，可选 | UI/列表中展示名称。省略则使用命令概览。 |
| `command` | string，必填 | 运行命令，后台通过 `/bin/sh -c`（Windows 用 `cmd /C`）执行。 |
| `cron` | string，必填 | 标准 5 字段 cron，允许 `* , - /`，不支持 `@daily` 等宏。 |
| `timeout_s` | int，可选 | 秒数，>0 时启用超时；未提供或为 0 表示不限时。 |
| `paused` | bool，可选 | `true` 则创建后保持暂停。 |

响应示例：

```json
{
  "id": "f2b7f6f8bf34f06ee3b8d1ae6a0d4a7b",
  "name": "Build Docs",
  "command": "bash -lc 'make docs'",
  "cron": "0 2 * * *",
  "status": "active",
  "timeout_s": 1800,
  "next_run_at": "2025-03-01T02:00:00Z",
  "created_at": "2025-02-28T15:12:03Z",
  "updated_at": "2025-02-28T15:12:03Z"
}
```

### 列出任务

- `GET /v1/tasks`
- 可通过查询参数 `status=active|paused` 过滤。

```bash
curl -s http://127.0.0.1:7070/v1/tasks?status=active | jq .
```

### 查看单个任务

- `GET /v1/tasks/{taskID}`

### 更新任务

- `PATCH /v1/tasks/{taskID}`
- 不需要修改的字段可以省略，仅包含要变更的内容。

```json
{
  "cron": "0 */6 * * *",
  "timeout_s": 1200,
  "paused": false
}
```

成功后返回完整任务对象。

### 删除任务

- `DELETE /v1/tasks/{taskID}`
- 删除后不再调度，历史运行记录与日志保留。

### 立即执行一次

- `POST /v1/tasks/{taskID}/run`
- 如果任务正在运行会返回 `409 conflict`。

成功返回：

```json
{ "run_id": "6c0f3a5e6e5248d1a6c34eba5d5ce9a3" }
```

## 运行记录 & 日志

### 查看任务的运行历史

- `GET /v1/tasks/{taskID}/runs?limit=20&offset=0`
- 响应为按创建时间倒序排列的运行记录数组。

返回字段：

| 字段 | 说明 |
| --- | --- |
| `status` | `queued`/`running`/`succeeded`/`failed`/`timed_out`/`skipped` |
| `scheduled_at` | 计划触发时间（UTC） |
| `started_at`/`ended_at` | 实际运行时间；可能为空 |
| `exit_code` | 成功或失败后的退出码 |
| `error` | 失败或超时时的消息 |

### 查看单条运行

- `GET /v1/runs/{runID}`

### 获取日志

- `GET /v1/runs/{runID}/log`
- 响应类型为 `text/plain`。
- 查询参数：
  - `tail=<行数>`：仅返回末尾 N 行。
  - `follow=1`：开启流式返回（类似 `tail -f`），直到客户端断开或运行结束。

示例：获取最新 200 行并跟随

```bash
curl -N "http://127.0.0.1:7070/v1/runs/<runID>/log?tail=200&follow=1"
```

## Cron 表达式预览

- `POST /v1/cron/preview`
- 用于校验 5 字段 cron 并展示未来若干次触发时间。

请求：

```json
{ "expr": "*/15 9-18 * * 1-5", "count": 5 }
```

响应：

```json
{
  "valid": true,
  "next_times": [
    "2025-03-03T09:00:00Z",
    "2025-03-03T09:15:00Z",
    "2025-03-03T09:30:00Z",
    "2025-03-03T09:45:00Z",
    "2025-03-03T10:00:00Z"
  ]
}
```

若表达式无效：

```json
{ "valid": false, "message": "only 5-field cron expressions are supported" }
```

## 状态枚举

- **任务状态** (`task.status`)
  - `active`：正常调度。
  - `paused`：暂停；不会触发，`next_run_at` 为空。

- **运行状态** (`run.status`)
  - `queued`：已入队即将执行。
  - `running`：正在执行。
  - `succeeded`：成功结束。
  - `failed`：命令退出码非 0，或启动失败。
  - `timed_out`：达到 `timeout_s` 被终止。
  - `skipped`：因任务仍在运行而跳过的触发。
  - `canceled`：保留状态，当前未主动使用。

## 常见错误码

| HTTP 状态 | `error.code` | 场景 |
| -------- | ------------ | ---- |
| 400 | `invalid_json` | 请求体不可解析。 |
| 400 | `invalid_input` | 缺少 command/cron、timeout 为负数等。 |
| 400 | `invalid_cron` | cron 表达式非法或包含 `@` 宏。 |
| 404 | `not_found` | 任务或运行不存在。 |
| 409 | `conflict` | 任务正在运行，无法立即执行。 |
| 500 | `internal_error` | 数据库或调度器内部错误。 |

## 典型工作流示例

1. **新建任务**：`POST /v1/tasks`，设置命令和 cron。
2. **使用预览**：在写 cron 前先调用 `/v1/cron/preview` 检查输出。
3. **查询状态**：周期性调用 `GET /v1/tasks` 获取 `next_run_at` 和最新运行情况。
4. **立即执行**：需要重跑时调用 `POST /v1/tasks/{id}/run`。
5. **查看日志**：从运行列表里取 `run_id`，再访问 `/v1/runs/{run_id}/log?tail=200`。
6. **暂停/恢复**：`PATCH /v1/tasks/{id}`，设置 `{"paused": true | false}`。

## 注意事项

- 所有命令在任务所在用户环境运行，默认工作目录为守护进程启动时的目录；可在命令里自行 `cd`。
- 调度精度为 1 分钟；同一任务如果仍在运行会跳过本次触发并记录为 `skipped`。
- 日志仅保留最近 `run_log_keep`（默认 20）次运行，再旧的会自动清理。
- 若要为 AI 工具提供“新增任务”能力，务必校验用户输入，比如：限制 `command` 白名单、提前调用 `/v1/cron/preview`。

## Curl 速查表

```bash
# 创建任务
curl -X POST http://127.0.0.1:7070/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"command":"bash -lc \"make lint\"","cron":"*/10 * * * *"}'

# 列出任务
curl http://127.0.0.1:7070/v1/tasks | jq .

# 立即运行
curl -X POST http://127.0.0.1:7070/v1/tasks/<taskID>/run

# 查看运行日志（末尾 100 行并跟随）
curl -N "http://127.0.0.1:7070/v1/runs/<runID>/log?tail=100&follow=1"
```

将本手册提供给 AI 代理或自动化脚本，可确保其了解如何与守护进程安全地交互。若后续引入鉴权或秒级 cron，将在接口层做兼容扩展。欢迎按需补充具体使用范式。
