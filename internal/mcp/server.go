package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"clicrontab/internal/core"
	"clicrontab/internal/store"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer represents the MCP server that handles protocol communication.
type MCPServer struct {
	store     *store.Store
	scheduler *core.Scheduler
	logger    *slog.Logger
	location  *time.Location
}

// NewMCPServer creates a new MCP server instance.
func NewMCPServer(store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location) *MCPServer {
	return &MCPServer{
		store:     store,
		scheduler: scheduler,
		logger:    logger,
		location:  location,
	}
}

// Run starts the MCP server using stdio transport.
func (s *MCPServer) Run() error {
	mcpServer := server.NewMCPServer(
		"clicrontab",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register all tools
	s.registerTools(mcpServer)

	s.logger.Info("MCP server starting on stdio")

	// Start the stdio server
	return server.ServeStdio(mcpServer)
}

// registerTools registers all available MCP tools.
func (s *MCPServer) registerTools(mcpServer *server.MCPServer) {
	// cron_create_task
	mcpServer.AddTool(mcp.NewTool("cron_create_task",
		mcp.WithDescription("创建一个定时执行 Claude 命令的任务。使用标准 5 字段 cron 表达式（分 时 日 月 周）"),
		mcp.WithString("name",
			mcp.Description("任务名称（可选）"),
		),
		mcp.WithString("prompt",
			mcp.Required(),
			mcp.Description("要执行的 Claude prompt"),
		),
		mcp.WithString("cron",
			mcp.Required(),
			mcp.Description("Cron 表达式，例如: '0 9 * * 1-5' 表示工作日早上 9 点"),
		),
		mcp.WithString("working_dir",
			mcp.Required(),
			mcp.Description("命令执行的工作目录"),
		),
		mcp.WithNumber("timeout_minutes",
			mcp.Description("超时时间（分钟），默认 30"),
			mcp.Min(0),
		),
	), s.handleCreateTask)

	// cron_list_tasks
	mcpServer.AddTool(mcp.NewTool("cron_list_tasks",
		mcp.WithDescription("列出所有定时任务"),
		mcp.WithString("status",
			mcp.Description("过滤状态: active 或 paused"),
			mcp.Enum("active", "paused"),
		),
	), s.handleListTasks)

	// cron_get_task
	mcpServer.AddTool(mcp.NewTool("cron_get_task",
		mcp.WithDescription("获取任务详情"),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("任务 ID"),
		),
	), s.handleGetTask)

	// cron_update_task
	mcpServer.AddTool(mcp.NewTool("cron_update_task",
		mcp.WithDescription("更新任务配置"),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("任务 ID"),
		),
		mcp.WithString("prompt",
			mcp.Description("新的 prompt"),
		),
		mcp.WithString("cron",
			mcp.Description("新的 cron 表达式"),
		),
		mcp.WithString("working_dir",
			mcp.Description("新的工作目录"),
		),
		mcp.WithBoolean("paused",
			mcp.Description("是否暂停任务"),
		),
	), s.handleUpdateTask)

	// cron_delete_task
	mcpServer.AddTool(mcp.NewTool("cron_delete_task",
		mcp.WithDescription("删除任务"),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("任务 ID"),
		),
	), s.handleDeleteTask)

	// cron_run_task
	mcpServer.AddTool(mcp.NewTool("cron_run_task",
		mcp.WithDescription("立即执行指定任务"),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("任务 ID"),
		),
		mcp.WithString("working_dir",
			mcp.Description("临时覆盖工作目录（可选）"),
		),
	), s.handleRunTask)

	// cron_list_runs
	mcpServer.AddTool(mcp.NewTool("cron_list_runs",
		mcp.WithDescription("查看任务的运行历史"),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("任务 ID"),
		),
		mcp.WithNumber("limit",
			mcp.Description("返回的运行记录数量，默认 20"),
			mcp.Min(1),
			mcp.Max(100),
		),
	), s.handleListRuns)

	// cron_get_run_log
	mcpServer.AddTool(mcp.NewTool("cron_get_run_log",
		mcp.WithDescription("获取运行的日志输出"),
		mcp.WithString("run_id",
			mcp.Required(),
			mcp.Description("运行记录 ID"),
		),
		mcp.WithNumber("tail",
			mcp.Description("返回最后 N 行日志，默认全部"),
			mcp.Min(0),
		),
	), s.handleGetRunLog)

	// cron_preview
	mcpServer.AddTool(mcp.NewTool("cron_preview",
		mcp.WithDescription("预览 cron 表达式的未来触发时间"),
		mcp.WithString("cron",
			mcp.Required(),
			mcp.Description("Cron 表达式"),
		),
		mcp.WithNumber("count",
			mcp.Description("返回的触发次数，默认 5"),
			mcp.Min(1),
			mcp.Max(10),
		),
	), s.handleCronPreview)

	s.logger.Info("MCP tools registered", "count", 9)
}

// handleCreateTask handles the cron_create_task tool call.
func (s *MCPServer) handleCreateTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Parse required parameters
	prompt := mcp.ParseString(request, "prompt", "")
	cronExpr := mcp.ParseString(request, "cron", "")
	workingDir := mcp.ParseString(request, "working_dir", "")

	// Validate cron expression
	schedule, err := core.ParseCron(cronExpr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("无效的 cron 表达式: %v", err)), nil
	}

	// Build command from prompt
	command := BuildClaudeCommand(prompt)

	// Parse optional parameters
	var namePtr *string
	name := mcp.ParseString(request, "name", "")
	if name != "" {
		namePtr = &name
	}

	var timeoutPtr *int
	timeoutMinutes := mcp.ParseFloat64(request, "timeout_minutes", 0)
	if timeoutMinutes > 0 {
		timeout := int(timeoutMinutes * 60) // Convert to seconds
		timeoutPtr = &timeout
	}

	// Create task
	task := &core.Task{
		ID:             core.NewID(),
		Name:           namePtr,
		Prompt:         prompt,
		Command:        command,
		Cron:           cronExpr,
		WorkingDir:     &workingDir,
		TimeoutSeconds: timeoutPtr,
		Status:         core.TaskStatusActive,
	}

	// Calculate next run time
	now := time.Now().In(s.location)
	nextTimes := core.NextOccurrences(schedule, now, 1)
	if len(nextTimes) > 0 {
		nextUTC := nextTimes[0].UTC()
		task.NextRunAt = &nextUTC
	}

	// Save to database
	if err := s.store.InsertTask(ctx, task); err != nil {
		s.logger.Error("insert task", "err", err)
		return mcp.NewToolResultError(fmt.Sprintf("创建任务失败: %v", err)), nil
	}

	// Schedule the task
	if err := s.scheduler.AddOrUpdateTask(ctx, task); err != nil {
		s.logger.Error("schedule task", "task_id", task.ID, "err", err)
	}

	s.logger.Info("task created", "task_id", task.ID, "cron", cronExpr, "working_dir", workingDir)

	return mcp.NewToolResultText(fmt.Sprintf("任务已创建\nID: %s\n下次执行: %s\n工作目录: %s",
		task.ID,
		formatTime(task.NextRunAt),
		workingDir,
	)), nil
}

// handleListTasks handles the cron_list_tasks tool call.
func (s *MCPServer) handleListTasks(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	statusStr := mcp.ParseString(request, "status", "")
	var statusFilter *core.TaskStatus
	if statusStr == "active" {
		status := core.TaskStatusActive
		statusFilter = &status
	} else if statusStr == "paused" {
		status := core.TaskStatusPaused
		statusFilter = &status
	}

	tasks, err := s.store.ListTasks(ctx, statusFilter)
	if err != nil {
		s.logger.Error("list tasks", "err", err)
		return mcp.NewToolResultError(fmt.Sprintf("获取任务列表失败: %v", err)), nil
	}

	if len(tasks) == 0 {
		return mcp.NewToolResultText("没有找到任务"), nil
	}

	result := fmt.Sprintf("找到 %d 个任务:\n\n", len(tasks))
	for _, t := range tasks {
		statusIcon := "▶️"
		if t.Status == core.TaskStatusPaused {
			statusIcon = "⏸️"
		}
		result += fmt.Sprintf("%s %s\n", statusIcon, t.ID)
		if t.Name != nil {
			result += fmt.Sprintf("  名称: %s\n", *t.Name)
		}
		result += fmt.Sprintf("  Cron: %s\n", t.Cron)
		result += fmt.Sprintf("  Prompt: %s\n", truncateString(t.Prompt, 60))
		result += fmt.Sprintf("  工作目录: %s\n", *t.WorkingDir)
		if t.NextRunAt != nil {
			result += fmt.Sprintf("  下次执行: %s\n", formatTime(t.NextRunAt))
		}
		result += "\n"
	}

	return mcp.NewToolResultText(result), nil
}

// handleGetTask handles the cron_get_task tool call.
func (s *MCPServer) handleGetTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := mcp.ParseString(request, "task_id", "")

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		if err == store.ErrTaskNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("任务不存在: %s", taskID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("获取任务失败: %v", err)), nil
	}

	result := fmt.Sprintf("任务 ID: %s\n", task.ID)
	if task.Name != nil {
		result += fmt.Sprintf("名称: %s\n", *task.Name)
	}
	result += fmt.Sprintf("状态: %s\n", task.Status)
	result += fmt.Sprintf("Prompt: %s\n", task.Prompt)
	result += fmt.Sprintf("Cron: %s\n", task.Cron)
	result += fmt.Sprintf("工作目录: %s\n", *task.WorkingDir)
	if task.TimeoutSeconds != nil {
		result += fmt.Sprintf("超时: %d 秒\n", *task.TimeoutSeconds)
	}
	if task.LastRunAt != nil {
		result += fmt.Sprintf("上次运行: %s\n", formatTime(task.LastRunAt))
	}
	if task.NextRunAt != nil {
		result += fmt.Sprintf("下次运行: %s\n", formatTime(task.NextRunAt))
	}
	result += fmt.Sprintf("创建时间: %s\n", formatTime(&task.CreatedAt))

	return mcp.NewToolResultText(result), nil
}

// handleUpdateTask handles the cron_update_task tool call.
func (s *MCPServer) handleUpdateTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := mcp.ParseString(request, "task_id", "")

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		if err == store.ErrTaskNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("任务不存在: %s", taskID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("获取任务失败: %v", err)), nil
	}

	// Update prompt if provided
	prompt := mcp.ParseString(request, "prompt", "")
	if prompt != "" {
		task.Prompt = prompt
		task.Command = BuildClaudeCommand(prompt)
	}

	// Update cron if provided
	cronExpr := mcp.ParseString(request, "cron", "")
	if cronExpr != "" {
		if _, err := core.ParseCron(cronExpr); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("无效的 cron 表达式: %v", err)), nil
		}
		task.Cron = cronExpr
	}

	// Update working_dir if provided
	workingDir := mcp.ParseString(request, "working_dir", "")
	if workingDir != "" {
		task.WorkingDir = &workingDir
	}

	// Update paused status
	cronChanged := false
	paused := mcp.ParseBoolean(request, "paused", false)
	if paused {
		task.Status = core.TaskStatusPaused
		cronChanged = true
	} else {
		task.Status = core.TaskStatusActive
		cronChanged = true
	}

	// Recalculate next run time if active and cron changed
	if task.Status == core.TaskStatusActive && cronChanged {
		schedule, _ := core.ParseCron(task.Cron)
		nextTimes := core.NextOccurrences(schedule, time.Now().In(s.location), 1)
		if len(nextTimes) > 0 {
			nextUTC := nextTimes[0].UTC()
			task.NextRunAt = &nextUTC
		}
	} else if task.Status == core.TaskStatusPaused {
		task.NextRunAt = nil
	}

	if err := s.store.UpdateTask(ctx, task); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("更新任务失败: %v", err)), nil
	}

	if err := s.scheduler.AddOrUpdateTask(ctx, task); err != nil {
		s.logger.Error("reschedule task", "task_id", task.ID, "err", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("任务已更新: %s\n状态: %s", task.ID, task.Status)), nil
}

// handleDeleteTask handles the cron_delete_task tool call.
func (s *MCPServer) handleDeleteTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := mcp.ParseString(request, "task_id", "")

	if err := s.store.DeleteTask(ctx, taskID); err != nil {
		if err == store.ErrTaskNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("任务不存在: %s", taskID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("删除任务失败: %v", err)), nil
	}

	s.scheduler.RemoveTask(taskID)

	return mcp.NewToolResultText(fmt.Sprintf("任务已删除: %s", taskID)), nil
}

// handleRunTask handles the cron_run_task tool call.
func (s *MCPServer) handleRunTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := mcp.ParseString(request, "task_id", "")

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		if err == store.ErrTaskNotFound {
			return mcp.NewToolResultError(fmt.Sprintf("任务不存在: %s", taskID)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("获取任务失败: %v", err)), nil
	}

	// Check if working_dir override is provided
	// Create a copy of the task if we need to override working_dir
	runTask := task
	workingDir := mcp.ParseString(request, "working_dir", "")
	if workingDir != "" {
		// Create a shallow copy with overridden working_dir
		taskCopy := *task
		taskCopy.WorkingDir = &workingDir
		runTask = &taskCopy
		s.logger.Debug("overriding working_dir", "task_id", taskID, "working_dir", workingDir)
	}

	run, err := s.scheduler.RunTaskNow(ctx, runTask)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("执行任务失败: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("任务已开始执行\n任务 ID: %s\n运行 ID: %s", task.ID, run.ID)), nil
}

// handleListRuns handles the cron_list_runs tool call.
func (s *MCPServer) handleListRuns(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := mcp.ParseString(request, "task_id", "")

	limit := int(mcp.ParseFloat64(request, "limit", 20))

	runs, err := s.store.ListRuns(ctx, taskID, limit, 0)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("获取运行历史失败: %v", err)), nil
	}

	if len(runs) == 0 {
		return mcp.NewToolResultText("该任务暂无运行记录"), nil
	}

	result := fmt.Sprintf("找到 %d 条运行记录:\n\n", len(runs))
	for _, r := range runs {
		statusIcon := statusToIcon(r.Status)
		result += fmt.Sprintf("[%s] 运行 ID: %s\n", statusIcon, r.ID)
		result += fmt.Sprintf("    状态: %s\n", r.Status)
		if r.StartedAt != nil {
			result += fmt.Sprintf("    开始: %s\n", formatTime(r.StartedAt))
		}
		if r.EndedAt != nil {
			result += fmt.Sprintf("    结束: %s\n", formatTime(r.EndedAt))
		}
		if r.ExitCode != nil {
			result += fmt.Sprintf("    退出码: %d\n", *r.ExitCode)
		}
		result += "\n"
	}

	return mcp.NewToolResultText(result), nil
}

// handleGetRunLog handles the cron_get_run_log tool call.
func (s *MCPServer) handleGetRunLog(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	runID := mcp.ParseString(request, "run_id", "")

	logPath := s.store.RunLogPath(runID)

	content, err := s.store.ReadRunLog(logPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("读取日志失败: %v", err)), nil
	}

	// Apply tail if specified
	tailLines := int(mcp.ParseFloat64(request, "tail", 0))
	if tailLines > 0 {
		lines, err := s.store.TailRunLog(content, tailLines)
		if err == nil {
			content = lines
		}
	}

	return mcp.NewToolResultText(content), nil
}

// handleCronPreview handles the cron_preview tool call.
func (s *MCPServer) handleCronPreview(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cronExpr := mcp.ParseString(request, "cron", "")

	schedule, err := core.ParseCron(cronExpr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("无效的 cron 表达式: %v", err)), nil
	}

	count := int(mcp.ParseFloat64(request, "count", 5))

	now := time.Now().In(s.location)
	nextTimes := core.NextOccurrences(schedule, now, count)

	result := fmt.Sprintf("Cron 表达式: %s\n", cronExpr)
	result += fmt.Sprintf("时区: %s\n\n", s.location)
	result += "未来触发时间:\n"
	for i, t := range nextTimes {
		result += fmt.Sprintf("  %d. %s\n", i+1, t.Format("2006-01-02 15:04:05"))
	}

	return mcp.NewToolResultText(result), nil
}

// Helper functions

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func statusToIcon(status core.RunStatus) string {
	switch status {
	case core.RunStatusSucceeded:
		return "✅"
	case core.RunStatusFailed:
		return "❌"
	case core.RunStatusTimedOut:
		return "⏱️"
	case core.RunStatusCanceled:
		return "🚫"
	case core.RunStatusSkipped:
		return "⏭️"
	case core.RunStatusRunning:
		return "▶️"
	case core.RunStatusQueued:
		return "⏳"
	default:
		return "❓"
	}
}
