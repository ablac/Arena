package game

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"arena-server/internal/db"
)

func TestPersistBountyBoardSnapshotKeepsNewestGeneration(t *testing.T) {
	t.Run("queued stale snapshot is skipped", func(t *testing.T) {
		engine := NewGameEngine()
		oldGeneration := engine.bountyPersistGeneration.Add(1)
		newGeneration := engine.bountyPersistGeneration.Add(1)
		var applied []string
		write := func(_ context.Context, rows []db.BountyBoardEntry) error {
			applied = append(applied, rows[0].Name)
			return nil
		}

		if err := engine.persistBountyBoardSnapshot(context.Background(), oldGeneration, []db.BountyBoardEntry{{Name: "old"}}, write); err != nil {
			t.Fatalf("persist stale snapshot: %v", err)
		}
		if err := engine.persistBountyBoardSnapshot(context.Background(), newGeneration, []db.BountyBoardEntry{{Name: "new"}}, write); err != nil {
			t.Fatalf("persist newest snapshot: %v", err)
		}
		if want := []string{"new"}; !reflect.DeepEqual(applied, want) {
			t.Fatalf("applied snapshots = %v, want %v", applied, want)
		}
	})

	t.Run("new snapshot follows an in-flight older write", func(t *testing.T) {
		engine := NewGameEngine()
		oldStarted := make(chan struct{})
		releaseOld := make(chan struct{})
		var appliedMu sync.Mutex
		var applied []string
		write := func(_ context.Context, rows []db.BountyBoardEntry) error {
			name := rows[0].Name
			if name == "old" {
				close(oldStarted)
				<-releaseOld
			}
			appliedMu.Lock()
			applied = append(applied, name)
			appliedMu.Unlock()
			return nil
		}

		oldGeneration := engine.bountyPersistGeneration.Add(1)
		oldDone := make(chan error, 1)
		go func() {
			oldDone <- engine.persistBountyBoardSnapshot(context.Background(), oldGeneration, []db.BountyBoardEntry{{Name: "old"}}, write)
		}()
		select {
		case <-oldStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("older bounty snapshot did not start")
		}

		newGeneration := engine.bountyPersistGeneration.Add(1)
		newDone := make(chan error, 1)
		go func() {
			newDone <- engine.persistBountyBoardSnapshot(context.Background(), newGeneration, []db.BountyBoardEntry{{Name: "new"}}, write)
		}()
		close(releaseOld)
		for name, done := range map[string]<-chan error{"old": oldDone, "new": newDone} {
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("persist %s snapshot: %v", name, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("persist %s snapshot timed out", name)
			}
		}

		appliedMu.Lock()
		defer appliedMu.Unlock()
		if want := []string{"old", "new"}; !reflect.DeepEqual(applied, want) {
			t.Fatalf("applied snapshots = %v, want %v", applied, want)
		}
	})
}
