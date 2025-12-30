package notify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// BarkNotifier sends notifications via Bark app.
type BarkNotifier struct {
	baseURL string
	client  *http.Client
}

// NewBarkNotifier creates a new Bark notifier.
func NewBarkNotifier(baseURL string) (*BarkNotifier, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("bark url is empty")
	}
	return &BarkNotifier{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (b *BarkNotifier) Send(ctx context.Context, title, body string) error {
	// Bark format: /{key}/{title}/{body}
	// We need to properly escape title and body
	// Alternatively, Bark supports POST requests which are safer for long content

	reqURL := b.baseURL
	// Remove trailing slash if present
	if reqURL[len(reqURL)-1] == '/' {
		reqURL = reqURL[:len(reqURL)-1]
	}

	// Use POST for better reliability with long text
	form := url.Values{}
	form.Set("title", title)
	form.Set("body", body)
	form.Set("group", "clicrontab")
	form.Set("icon", "https://github.com/clicrontab.png") // Optional icon

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return fmt.Errorf("create bark request: %w", err)
	}

	req.URL.RawQuery = form.Encode()

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("send bark notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("bark api returned status: %d", resp.StatusCode)
	}

	return nil
}
