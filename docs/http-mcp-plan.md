# clicrontab HTTP-MCP 改造计划

## 1. 目标

将项目简化为单一 HTTP 服务模式，通过 `/mcp` 端点提供 MCP 协议支持，废弃原有的 Stdio 传输模式。

**核心需求：**
- **单端点**：`/mcp` 同时处理 SSE 连接和 JSON-RPC 消息
- **无鉴权**：方便快速集成
- **架构简化**：移除多模式切换，统一为 HTTP 服务

---

## 2. 架构调整

### 2.1 路由结构

```
HTTP Server (:7070)
├── /                  # Web UI (Index)
├── /assets/*          # 静态资源
├── /v1/*              # REST API (Tasks/Runs)
└── /mcp               # MCP Endpoint (SSE + JSON-RPC)
    ├── GET            # SSE 握手 (text/event-stream)
    └── POST           # JSON-RPC 消息
```

### 2.2 组件交互

```
[Claude Code] <--(HTTP/SSE)--> [HTTP Server] <--(Go Channel)--> [MCP Server] <--> [Scheduler/Store]
```

---

## 3. 实现步骤

### Step 1: 改造 MCP Server

**文件**: `internal/mcp/server.go`

- 移除 `Run()` (Stdio 启动逻辑)
- 新增 `SetupSSE(addr string)`：初始化 SSE Server
- 新增 `ServeHTTP(w, r)`：统一处理 GET/POST 请求

```go
type MCPServer struct {
    mcpServer *server.MCPServer
    sseServer *server.SSEServer
    // ...
}

func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        s.sseServer.HandleSSE().ServeHTTP(w, r)
    } else if r.Method == http.MethodPost {
        s.sseServer.HandleMessage().ServeHTTP(w, r)
    } else {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    }
}
```

### Step 2: 挂载路由

**文件**: `internal/api/router.go`

- 在 `NewServer` 中接收 `*mcp.MCPServer`
- 注册路由：`router.Handle("/mcp", mcpServer)`

### Step 3: 清理配置和入口

**文件**: `internal/config/config.go`
- 移除 `Mode` 字段和相关 flag

**文件**: `cmd/clicrontabd/main.go`
- 移除 `runHTTPMode`, `runMCPMode`, `runBothMode`
- 统一启动逻辑：初始化 MCP -> 初始化 API -> 启动 Server

---

## 4. 验证计划

1. **启动服务**：`./clicrontabd`
2. **测试 SSE**：`curl -N http://127.0.0.1:7070/mcp` (应收到 endpoint 事件)
3. **测试 Claude 连接**：配置 `url` 为 `http://127.0.0.1:7070/mcp` 并验证 Tool 调用

---

## 5. 依赖

无需新增依赖，复用 `github.com/mark3labs/mcp-go/server`。
