// Package gaming delivers durable authoritative Arena events to Angel Gaming.
package gaming

import (
	"arena-server/internal/db"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type sender struct {
	endpoint, key string
	client        *http.Client
}

func newSender(origin, key string, client *http.Client) *sender {
	// A redirect must never forward the service credential or mark an event sent.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copyClient.Timeout = 5 * time.Second
	return &sender{endpoint: origin + "/api/integrations/arena/events", key: key, client: &copyClient}
}
func (s *sender) send(ctx context.Context, event db.GamingEvent) (int, bool) {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.endpoint, bytes.NewReader(event.Payload))
	if err != nil {
		return 0, false
	}
	request.Header.Set("Authorization", "Bearer "+s.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return 0, false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, response.StatusCode >= 200 && response.StatusCode < 300
}

// Run owns no gameplay state. Claims and acknowledgments are durable database
// operations, so shutdown, a network failure or a lost acknowledgment is retried.
func Run(ctx context.Context, origin, key string) {
	if origin == "" || key == "" {
		return
	}
	s := newSender(origin, key, &http.Client{})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		events, err := db.ClaimGamingEvents(claimCtx)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("Gaming event queue temporarily unavailable")
			}
		} else {
			for _, event := range events {
				status, ack := s.send(ctx, event)
				completeCtx, completeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := db.CompleteGamingEvent(completeCtx, event.ID, status, ack)
				completeCancel()
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					slog.Warn("Gaming event acknowledgment could not be persisted")
				} else if !ack {
					slog.Warn("Gaming event delivery deferred", "status", status)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
