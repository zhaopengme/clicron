# clicrontab v1.1 升级计划

## 1. 目标

增强安全性、可配置性和可观测性，使项目更适合生产环境和 AI 集成。

**核心功能：**
1. **Token 认证**：保护 `/mcp` 端点
2. **高级配置**：支持 YAML 配置文件 (Host/Port, Token, Bark)
3. **Bark 通知**：任务执行结果实时推送

---

## 2. 架构设计

### 2.1 配置管理 (`internal/config`)

使用环境变量和 `.env` 文件。优先级：**命令行参数 > 环境变量 > 默认值**。

**`.env` 示例：**
```env
CLICRON_ADDR=0.0.0.0:7070
CLICRON_AUTH_TOKEN=secret-token-123
CLICRON_LOG_LEVEL=info
CLICRON_BARK_URL=https://api.day.app/YOUR_KEY/
CLICRON_BARK_ENABLED=true
```

### 2.2 通知系统 (`internal/notify`)

新建 `notify` 包，定义通用接口以支持未来扩展（如 Email, Slack）。

```go
type Notifier interface {
    Send(ctx context.Context, title, body string, level Level) error
}
```

**集成点**：
`internal/core/executor.go` -> `Execute()` 方法结束时调用。

### 2.3 鉴权中间件 (`internal/api/middleware`)

实现轻量级 Token 校验。

- **范围**：仅应用于 `/mcp` 路由。
- **方式**：Header `Authorization: Bearer <token>` 或 Query `?token=<token>`。

---

## 3. 实现步骤

### Phase 1: 基础建设
1. **添加依赖**：`gopkg.in/yaml.v3`
2. **重构 Config**：
   - 定义 `Config` 结构体映射 YAML
   - 实现 `Load(path)`
   - 修改 `main.go` 适配新配置加载逻辑

### Phase 2: 通知系统
3. **实现 Notify 包**：
   - `Notifier` 接口
   - `BarkNotifier` 实现
4. **集成 Executor**：
   - `NewCommandExecutor` 接收 `Notifier`
   - `Execute` 方法中添加通知逻辑（使用 `defer` 确保发送）

### Phase 3: 鉴权与路由
5. **实现 Auth Middleware**：
   - 检查配置中是否有 Token，无则跳过
   - 验证 Token
6. **更新 Router**：
   - 在挂载 `/mcp` 时应用中间件

---

## 4. 验证计划

1. **配置测试**：创建 `config.yaml`，验证端口和 Token 是否生效。
2. **鉴权测试**：
   - 不带 Token 访问 `/mcp` -> 401
   - 带 Token 访问 `/mcp` -> 200/SSE
3. **通知测试**：手动触发任务 (`cron_run_task`)，检查手机是否收到 Bark 推送。

## 5. 依赖变更

- `go get gopkg.in/yaml.v3`
