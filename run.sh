#!/bin/bash

# clicrontab 控制脚本

PID_FILE=".clicrontabd.pid"
LOG_FILE="clicrontabd.log"

case "$1" in
    start)
        if [ -f "$PID_FILE" ]; then
            PID=$(cat "$PID_FILE")
            if ps -p "$PID" > /dev/null 2>&1; then
                echo "clicrontabd is already running (PID: $PID)"
                exit 1
            else
                rm -f "$PID_FILE"
            fi
        fi

        echo "Starting clicrontabd..."
        nohup go run ./cmd/clicrontabd/main.go >> "$LOG_FILE" 2>&1 &
        echo $! > "$PID_FILE"
        echo "clicrontabd started (PID: $!)"
        echo "Logs: $LOG_FILE"
        ;;

    stop)
        if [ ! -f "$PID_FILE" ]; then
            echo "clicrontabd is not running"
            exit 1
        fi

        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            echo "Stopping clicrontabd (PID: $PID)..."
            kill "$PID"
            rm -f "$PID_FILE"
            echo "clicrontabd stopped"
        else
            echo "clicrontabd is not running (stale PID file)"
            rm -f "$PID_FILE"
        fi
        ;;

    restart)
        $0 stop
        sleep 1
        $0 start
        ;;

    status)
        if [ -f "$PID_FILE" ]; then
            PID=$(cat "$PID_FILE")
            if ps -p "$PID" > /dev/null 2>&1; then
                echo "clicrontabd is running (PID: $PID)"
                exit 0
            else
                echo "clicrontabd is not running (stale PID file)"
                exit 1
            fi
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
        echo "Usage: $0 {start|stop|restart|status|logs}"
        exit 1
        ;;
esac
