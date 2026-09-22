package audit

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"neighborparking/internal/domain"
)

type fakeWriter struct {
	mu     sync.Mutex
	events []domain.AuditEvent
}

func (f *fakeWriter) InsertAudit(_ context.Context, e domain.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func TestQueueDrainsOnShutdown(t *testing.T) {
	f := &fakeWriter{}
	q := New(f, 2, 10, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 8; i++ {
		if !q.Enqueue(domain.AuditEvent{ID: string(rune('a' + i)), ActionType: "TEST"}) {
			t.Fatal("enqueue failed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) != 8 {
		t.Fatalf("events = %d, want 8", len(f.events))
	}
}
