package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// AlertService delivers operational alerts (failed operations, reconciliation repairs,
// lifecycle errors, budget overruns) to a configured webhook URL as JSON. When no URL
// is configured it logs the alert and does nothing else, so callers can always invoke it.
type AlertService struct {
	webhookURL string
	httpClient *http.Client
	log        *slog.Logger
	metrics    *Metrics
}

type AlertEvent struct {
	Event     string         `json:"event"`
	Severity  string         `json:"severity"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
}

func NewAlertService(webhookURL string, metrics *Metrics, log *slog.Logger) *AlertService {
	return &AlertService{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        log,
		metrics:    metrics,
	}
}

// Send fires the alert asynchronously; webhook failures are logged, never propagated.
func (a *AlertService) Send(event, severity, message string, details map[string]any) {
	if a == nil {
		return
	}

	payload := AlertEvent{
		Event:     event,
		Severity:  severity,
		Message:   message,
		Details:   details,
		Timestamp: time.Now().UTC(),
		Source:    "docklab",
	}

	a.log.Warn("alert raised", "event", event, "severity", severity, "message", message)

	if a.webhookURL == "" {
		return
	}

	go func() {
		body, err := json.Marshal(payload)
		if err != nil {
			a.log.Error("alert: failed to marshal payload", "error", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.webhookURL, bytes.NewReader(body))
		if err != nil {
			a.log.Error("alert: failed to build webhook request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		a.metrics.RecordAlertSent()
		resp, err := a.httpClient.Do(req)
		if err != nil {
			a.log.Error("alert: webhook delivery failed", "error", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			a.log.Error("alert: webhook returned non-success status", "status", resp.StatusCode)
		}
	}()
}
