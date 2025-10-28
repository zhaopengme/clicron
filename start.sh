#!/bin/bash

# clicrontab 启动脚本

# 检查是否已经在运行
if pgrep -f "clicrontabd" > /dev/null; then
    echo "clicrontabd 已经在运行中"
    exit 1
fi

# 设置默认参数
ADDR="${CLICRON_ADDR:-127.0.0.1:7070}"
DATA_DIR="${CLICRON_DATA_DIR:-data}"
LOG_DIR="${CLICRON_LOG_DIR:-logs}"
LOG_LEVEL="${CLICRON_LOG_LEVEL:-info}"

# 创建必要的目录
mkdir -p "$DATA_DIR" "$LOG_DIR"

# 使用 go run 启动服务
echo "正在启动 clicrontabd..."
echo "地址: $ADDR"
echo "数据目录: $DATA_DIR"
echo "日志目录: $LOG_DIR"
echo "日志级别: $LOG_LEVEL"

nohup go run cmd/clicrontabd/main.go \
    --addr "$ADDR" \
    --state-dir "$DATA_DIR" \
    --log-level "$LOG_LEVEL" \
    > "$LOG_DIR/clicrontabd.log" 2>&1 &

echo "正在启动服务..."

# 等待服务启动并获取真实 PID
sleep 2

# 从 ADDR 中提取端口号（格式：host:port）
PORT=${ADDR##*:}
PID=$(lsof -ti:$PORT)

if [ -z "$PID" ]; then
    echo "服务启动失败，端口 $ADDR 未被监听，请检查日志: $LOG_DIR/clicrontabd.log"
    exit 1
fi

echo "clicrontabd 已启动，PID: $PID"
echo $PID > "$LOG_DIR/clicrontabd.pid"

echo "服务启动成功！访问 http://$ADDR"
echo "日志文件: $LOG_DIR/clicrontabd.log"
echo "数据文件: $DATA_DIR/"