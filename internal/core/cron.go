package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ParseCron ensures the expression is a valid 5-field cron definition and returns the underlying schedule.
func ParseCron(expr string) (cron.Schedule, error) {
	if strings.HasPrefix(strings.TrimSpace(expr), "@") {
		return nil, fmt.Errorf("only 5-field cron expressions are supported")
	}
	schedule, err := cronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return schedule, nil
}

// NextOccurrences returns the next n execution times from a base time.
func NextOccurrences(schedule cron.Schedule, base time.Time, n int) []time.Time {
	times := make([]time.Time, 0, n)
	next := base
	for i := 0; i < n; i++ {
		next = schedule.Next(next)
		times = append(times, next)
	}
	return times
}
