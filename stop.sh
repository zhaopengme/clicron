#!/bin/bash

# clicrontab 停止脚本

# 设置默认日志目录
LOG_DIR="${CLICRON_LOG_DIR:-logs}"
PID_FILE="$LOG_DIR/clicrontabd.pid"

# 检查 PID 文件是否存在
if [ ! -f "$PID_FILE" ]; then
    echo "PID 文件不存在: $PID_FILE"
    echo "正在尝试通过进程名查找 clicrontabd..."

    # 尝试通过进程名查找
    PID=$(pgrep -f "clicrontabd" || pgrep -f "go run cmd/clicrontabd/main.go")

    if [ -z "$PID" ]; then
        echo "未找到 clicrontabd 进程"
        exit 1
    fi

    echo "找到进程 PID: $PID"
else
    # 从 PID 文件读取进程 ID
    PID=$(cat "$PID_FILE")
    echo "从 PID 文件读取到进程 ID: $PID"
fi

# 检查进程是否存在
if ! ps -p "$PID" > /dev/null; then
    echo "进程 $PID 不存在，可能已经结束"
    rm -f "$PID_FILE"
    exit 0
fi

# 尝试优雅停止
echo "正在停止 clicrontabd (PID: $PID)..."
if kill -TERM "$PID" 2>/dev/null; then

    # 等待进程结束（最多 10 秒）
    for i in {1..10}; do
        if ! ps -p "$PID" > /dev/null; then
            echo "clicrontabd 已停止"
            rm -f "$PID_FILE"
            exit 0
        fi
        sleep 1
    done

    # 如果进程还在，强制停止
    echo "优雅停止失败，正在强制停止..."
    if kill -KILL "$PID" 2>/dev/null; then
        echo "clicrontabd 已被强制停止"
    else
        echo "停止失败，进程可能已经结束"
    fi
else
    echo "停止失败，进程可能已经结束"
fi

# 清理 PID 文件
rm -f "$PID_FILE"

# 再次检查是否还有相关进程
REMAINING_PIDS=$(pgrep -f "clicrontabd" || pgrep -f "go run cmd/clicrontabd/main.go" || echo "")
if [ -n "$REMAINING_PIDS" ]; then
    echo "警告: 发现其他相关进程: $REMAINING_PIDS"
    echo "如需停止所有相关进程，请手动执行: pkill -f clicrontabd"
fi