package gaming

import (
	"arena-server/internal/db"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSenderAcknowledgesSuccessAndRetainsFailuresWithoutFollowingRedirects(t *testing.T) {
	status := 200
	calls := 0
	key := strings.Repeat("a", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/integrations/arena/events" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("incorrect event request")
		}
		if status == 302 {
			w.Header().Set("Location", "/steal-token")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"duplicate":true}`))
	}))
	defer server.Close()
	sender := newSender(server.URL, key, server.Client())
	for _, tc := range []struct {
		status int
		ack    bool
	}{{200, true}, {201, true}, {400, false}, {409, false}, {503, false}, {302, false}} {
		status = tc.status
		got, ack := sender.send(context.Background(), db.GamingEvent{ID: "round:bot", Payload: []byte(`{"eventId":"round:bot"}`)})
		if got != tc.status || ack != tc.ack {
			t.Fatalf("delivery (%d,%v) want (%d,%v)", got, ack, tc.status, tc.ack)
		}
	}
	if calls != 6 {
		t.Fatalf("followed redirect: %d requests", calls)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ack := sender.send(canceled, db.GamingEvent{ID: "round:bot", Payload: []byte(`{}`)}); ack {
		t.Fatal("canceled request acknowledged")
	}
}
