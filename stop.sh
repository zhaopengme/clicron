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
    else
        echo "找到进程 PID: $PID"
    fi
else
    # 从 PID 文件读取进程 ID
    PID=$(cat "$PID_FILE")
    echo "从 PID 文件读取到进程 ID: $PID"
fi

# 如果找到了主进程，尝试停止它
if [ -n "$PID" ] && ps -p "$PID" > /dev/null; then
    # 尝试优雅停止
    echo "正在停止 clicrontabd (PID: $PID)..."
    if kill -TERM "$PID" 2>/dev/null; then

        # 等待进程结束（最多 10 秒）
        for i in {1..10}; do
            if ! ps -p "$PID" > /dev/null; then
                echo "clicrontabd 已停止"
                rm -f "$PID_FILE"
                break
            fi
            sleep 1
        done

        # 如果进程还在，强制停止
        if ps -p "$PID" > /dev/null; then
            echo "优雅停止失败，正在强制停止..."
            if kill -KILL "$PID" 2>/dev/null; then
                echo "clicrontabd 已被强制停止"
            else
                echo "停止失败，进程可能已经结束"
            fi
        fi
    else
        echo "停止失败，进程可能已经结束"
    fi

    # 清理 PID 文件
    rm -f "$PID_FILE"
else
    if [ -n "$PID" ]; then
        echo "进程 $PID 不存在，可能已经结束"
        rm -f "$PID_FILE"
    fi
fi

# 检查7070端口是否被占用并提供操作选项
echo ""
echo "检查7070端口占用情况..."

# 检查端口占用的函数，兼容不同系统
check_port_7070() {
    if command -v lsof >/dev/null 2>&1; then
        lsof -i :7070 -P -n 2>/dev/null || echo ""
    elif command -v netstat >/dev/null 2>&1; then
        netstat -tlnp 2>/dev/null | grep ':7070' 2>/dev/null || echo ""
    elif command -v ss >/dev/null 2>&1; then
        ss -tlnp 2>/dev/null | grep ':7070' 2>/dev/null || echo ""
    else
        echo "无法检查端口，系统缺少 lsof、netstat 或 ss 命令"
    fi
}

PORT_INFO=$(check_port_7070)
if [ -n "$PORT_INFO" ]; then
    echo "7070端口当前被占用:"
    echo "$PORT_INFO"
    
    # 尝试从不同输出格式中提取PID (lsof format: COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME)
    if echo "$PORT_INFO" | head -n 1 | grep -q "COMMAND\|lsof"; then
        # For lsof output format
        PORT_PID=$(echo "$PORT_INFO" | grep "LISTEN" | awk '{print $2}' | head -n 1)
    elif echo "$PORT_INFO" | grep -q "LISTEN"; then
        # For netstat/ss output format
        PORT_PID=$(echo "$PORT_INFO" | awk '{print $7}' | cut -d'/' -f1 | head -n 1)
    else
        # Default to lsof format
        PORT_PID=$(echo "$PORT_INFO" | awk '{print $2}' | head -n 1)
    fi
    
    if [ -n "$PORT_PID" ] && [ "$PORT_PID" != "" ] && [ "$PORT_PID" != "$PID" ]; then
        echo "占用7070端口的进程PID: $PORT_PID"
        
        # 获取进程详细信息
        PROCESS_NAME=$(ps -p "$PORT_PID" -o comm= 2>/dev/null || echo "Unknown process")
        PROCESS_CMDLINE=$(ps -p "$PORT_PID" -o args= 2>/dev/null || echo "")
        echo "进程名称: $PROCESS_NAME"
        echo "进程命令: $PROCESS_CMDLINE"
        
        read -p "是否停止占用7070端口的进程? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "正在优雅停止PID $PORT_PID..."
            if kill -TERM "$PORT_PID" 2>/dev/null; then
                # 等待5秒看进程是否结束
                for i in {1..5}; do
                    if ! ps -p "$PORT_PID" > /dev/null 2>/dev/null; then
                        echo "进程 $PORT_PID 已停止"
                        break
                    fi
                    sleep 1
                done
                # 如果进程仍在运行，询问是否强制停止
                if ps -p "$PORT_PID" > /dev/null 2>/dev/null; then
                    read -p "优雅停止失败，是否强制停止? (y/N): " -n 1 -r
                    echo
                    if [[ $REPLY =~ ^[Yy]$ ]]; then
                        if kill -KILL "$PORT_PID" 2>/dev/null; then
                            echo "进程 $PORT_PID 已被强制停止"
                        else
                            echo "强制停止失败"
                        fi
                    else
                        echo "取消强制停止"
                    fi
                fi
            else
                echo "停止进程失败"
            fi
        else
            echo "跳过停止占用7070端口的进程"
        fi
    elif [ "$PORT_PID" = "$PID" ]; then
        echo "该进程即为正在停止的主要 clicrontabd 进程"
    else
        echo "无法获取占用7070端口的进程PID"
    fi
else
    echo "7070端口当前未被占用或没有权限查看"
fi

# 额外：使用 lsof 搜索可能的相关进程（特别是包含 "main" 的进程）
echo ""
echo "检查是否有其他相关的 Go main 进程..."
MAIN_PROCESSES=$(lsof -i :7070 -c main 2>/dev/null || echo "")
if [ -n "$MAIN_PROCESSES" ]; then
    echo "发现与 'main' 相关的进程使用 7070 端口:"
    echo "$MAIN_PROCESSES"
    
    MAIN_PID=$(echo "$MAIN_PROCESSES" | grep "LISTEN" | awk '{print $2}' | head -n 1)
    if [ -n "$MAIN_PID" ]; then
        read -p "是否停止 'main' 相关进程 (PID: $MAIN_PID)? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "正在停止 main 进程 (PID: $MAIN_PID)..."
            if kill -TERM "$MAIN_PID" 2>/dev/null; then
                echo "已发送终止信号给 main 进程"
            else
                echo "停止 main 进程失败"
            fi
        else
            echo "跳过停止 main 相关进程"
        fi
    fi
else
    # 如果没有在端口7070上找到main进程，查找系统中所有可能的main进程
    ALL_MAIN_PROCESSES=$(ps aux | grep -i "main" | grep -v grep | grep -v lsof | head -n 5)
    if [ -n "$ALL_MAIN_PROCESSES" ]; then
        echo "发现系统中可能的相关 'main' 进程:"
        echo "$ALL_MAIN_PROCESSES"
        
        read -p "是否需要停止这些 'main' 进程? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            MAIN_PIDS=$(echo "$ALL_MAIN_PROCESSES" | awk '{print $2}')
            for main_pid in $MAIN_PIDS; do
                echo "正在停止 main 进程 (PID: $main_pid)..."
                if kill -TERM "$main_pid" 2>/dev/null; then
                    echo "已发送终止信号给进程 $main_pid"
                else
                    echo "停止进程 $main_pid 失败"
                fi
            done
        else
            echo "跳过停止 'main' 进程"
        fi
    fi
fi

# 再次检查是否还有相关进程
echo ""
REMAINING_PIDS=$(pgrep -f "clicrontabd" || pgrep -f "go run cmd/clicrontabd/main.go" || echo "")
if [ -n "$REMAINING_PIDS" ]; then
    echo "警告: 发现其他相关进程: $REMAINING_PIDS"
    read -p "是否停止所有相关进程? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if pkill -f "clicrontabd" 2>/dev/null; then
            echo "已发送停止信号给所有相关进程"
        else
            echo "停止相关进程失败"
        fi
    else
        echo "跳过停止其他相关进程"
    fi
else
    echo "未发现其他相关进程"
fi

echo "停止脚本执行完成"