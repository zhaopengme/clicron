#!/bin/bash

# clicrontab 控制脚本

LOG_FILE="clicrontabd.log"

# 从 .env 读取端口配置
get_port() {
    # 先检查 .env 文件
    if [ -f ".env" ]; then
        # 提取 CLICRON_ADDR，解析端口
        ADDR=$(grep "^CLICRON_ADDR=" .env | cut -d'=' -f2 | tr -d '"')
        if [ -n "$ADDR" ]; then
            # 从 addr:port 或 host:port 格式中提取端口
            PORT=$(echo "$ADDR" | sed 's/.*://' | grep -oE '^[0-9]+')
            if [ -n "$PORT" ]; then
                echo "$PORT"
                return
            fi
        fi
    fi
    # 默认端口
    echo "7070"
}

PORT=$(get_port)

# 通过端口查找进程
find_pid_by_port() {
    # 尝试 lsof (macOS/Linux)
    PID=$(lsof -ti :$PORT 2>/dev/null)
    if [ -n "$PID" ]; then
        echo "$PID"
        return
    fi

    # 尝试 ss (Linux)
    PID=$(ss -tlnp "sport = :$PORT" 2>/dev/null | grep -oP 'pid=\K\d+' | head -1)
    if [ -n "$PID" ]; then
        echo "$PID"
        return
    fi

    # 尝试 netstat (老系统)
    PID=$(netstat -tlnp 2>/dev/null | grep ":$PORT " | grep -oP 'pid=\K\d+' | head -1)
    if [ -n "$PID" ]; then
        echo "$PID"
        return
    fi
}

# 检查是否运行
is_running() {
    PID=$(find_pid_by_port)
    [ -n "$PID" ]
}

case "$1" in
    start)
        if is_running; then
            PID=$(find_pid_by_port)
            echo "clicrontabd is already running (PID: $PID, Port: $PORT)"
            exit 1
        fi

        echo "Starting clicrontabd..."
        nohup go run ./cmd/clicrontabd/main.go >> "$LOG_FILE" 2>&1 &
        sleep 2

        if is_running; then
            PID=$(find_pid_by_port)
            echo "clicrontabd started (PID: $PID)"
            echo "Logs: tail -f $LOG_FILE"
        else
            echo "Failed to start clicrontabd, check $LOG_FILE"
            exit 1
        fi
        ;;

    stop)
        PID=$(find_pid_by_port)
        if [ -z "$PID" ]; then
            echo "clicrontabd is not running (port $PORT is free)"
            exit 0
        fi

        echo "Stopping clicrontabd (PID: $PID)..."
        kill "$PID" 2>/dev/null

        # 等待进程结束
        for i in {1..20}; do
            if ! is_running; then
                echo "clicrontabd stopped"
                exit 0
            fi
            sleep 0.3
        done

        # 强制杀死
        echo "Force killing..."
        kill -9 "$PID" 2>/dev/null
        sleep 0.5

        if is_running; then
            echo "Warning: Process may still be running"
            exit 1
        else
            echo "clicrontabd stopped"
        fi
        ;;

    force-stop)
        PID=$(find_pid_by_port)
        if [ -z "$PID" ]; then
            echo "No process found on port $PORT"
            exit 0
        fi
        echo "Force killing clicrontabd (PID: $PID)..."
        kill -9 "$PID" 2>/dev/null
        sleep 0.5
        echo "Done"
        ;;

    restart)
        $0 stop
        sleep 1
        $0 start
        ;;

    status)
        PID=$(find_pid_by_port)
        if [ -n "$PID" ]; then
            echo "clicrontabd is running"
            echo "  PID: $PID"
            echo "  Port: $PORT"
            ps -p "$PID" -o pid,cmd 2>/dev/null | tail -1 | sed 's/^/   /'
            exit 0
        else
            echo "clicrontabd is not running"
            exit 1
        fi
        ;;

    logs)
        if [ -f "$LOG_FILE" ]; then
            tail -f "$LOG_FILE"
        else
            echo "Log file not found: $LOG_FILE"
            exit 1
        fi
        ;;

    *)
        echo "Usage: $0 {start|stop|force-stop|restart|status|logs}"
        exit 1
        ;;
esac
