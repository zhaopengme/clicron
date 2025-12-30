package notify

import (
	"context"
)

// Notifier defines the interface for sending notifications.
type Notifier interface {
	Send(ctx context.Context, title, body string) error
}

// MultiNotifier combines multiple notifiers.
type MultiNotifier struct {
	notifiers []Notifier
}

func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Send(ctx context.Context, title, body string) error {
	for _, n := range m.notifiers {
		if err := n.Send(ctx, title, body); err != nil {
			// Log error but continue with other notifiers
			// Since we don't have a logger here, we just return the last error
			// In a real app, we might want to aggregate errors
			return err
		}
	}
	return nil
}

// NoOpNotifier does nothing.
type NoOpNotifier struct{}

func (n *NoOpNotifier) Send(ctx context.Context, title, body string) error {
	return nil
}
