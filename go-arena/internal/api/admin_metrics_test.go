package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/game"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminDebugMetricsIncludesWebSocketAdmissionsAndDatabasePoolPressure(t *testing.T) {
	originalPool := db.Pool
	poolConfig, err := pgxpool.ParseConfig("postgres://arena:arena@127.0.0.1:1/arena?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	poolConfig.MaxConns = 7
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatalf("create lazy pool: %v", err)
	}
	db.Pool = pool
	t.Cleanup(func() {
		pool.Close()
		db.Pool = originalPool
	})

	handler := &AdminHandler{
		Engine:    game.NewGameEngine(),
		startTime: time.Now().Add(-time.Minute),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/debug/metrics", nil)

	handler.debugMetrics(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		WebSocket struct {
			WindowSeconds int `json:"window_seconds"`
			Bot           struct {
				Totals struct {
					Attempts uint64 `json:"attempts"`
				} `json:"totals"`
			} `json:"bot"`
		} `json:"websocket"`
		DatabasePool struct {
			TotalConnections        int32   `json:"total_connections"`
			AcquiredConnections     int32   `json:"acquired_connections"`
			IdleConnections         int32   `json:"idle_connections"`
			MaxConnections          int32   `json:"max_connections"`
			ConstructingConnections int32   `json:"constructing_connections"`
			AcquireCount            int64   `json:"acquire_count"`
			CanceledAcquireCount    int64   `json:"canceled_acquire_count"`
			EmptyAcquireCount       int64   `json:"empty_acquire_count"`
			AcquireDurationMS       float64 `json:"acquire_duration_ms"`
		} `json:"database_pool"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if response.WebSocket.WindowSeconds != 60 {
		t.Fatalf("websocket.window_seconds = %d, want 60", response.WebSocket.WindowSeconds)
	}
	if response.DatabasePool.MaxConnections != 7 {
		t.Fatalf("database_pool.max_connections = %d, want 7", response.DatabasePool.MaxConnections)
	}
	if response.DatabasePool.TotalConnections != 0 || response.DatabasePool.AcquiredConnections != 0 || response.DatabasePool.IdleConnections != 0 || response.DatabasePool.ConstructingConnections != 0 {
		t.Fatalf("new lazy pool pressure = %#v, want zero current connections", response.DatabasePool)
	}
	if response.DatabasePool.AcquireCount != 0 || response.DatabasePool.CanceledAcquireCount != 0 || response.DatabasePool.EmptyAcquireCount != 0 || response.DatabasePool.AcquireDurationMS != 0 {
		t.Fatalf("new lazy pool cumulative counters = %#v, want zero", response.DatabasePool)
	}
}
