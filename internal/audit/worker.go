package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"neighborparking/internal/domain"
)

type Writer interface {
	InsertAudit(context.Context, domain.AuditEvent) error
}

type Queue struct {
	jobs   chan domain.AuditEvent
	writer Writer
	log    *slog.Logger
	wg     sync.WaitGroup
	stop   sync.Once
}

func New(writer Writer, workers, buffer int, log *slog.Logger) *Queue {
	q := &Queue{jobs: make(chan domain.AuditEvent, buffer), writer: writer, log: log}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	return q
}

func (q *Queue) Enqueue(e domain.AuditEvent) bool {
	select {
	case q.jobs <- e:
		return true
	default:
		q.log.Error("audit queue full", "action", e.ActionType, "actor", e.ActorUserID)
		return false
	}
}

func (q *Queue) worker(id int) {
	defer q.wg.Done()
	for e := range q.jobs {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = q.writer.InsertAudit(ctx, e)
			cancel()
			if err == nil {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
		if err != nil {
			q.log.Error("audit write failed", "worker", id, "event", e.ID, "error", err)
		}
	}
}

func (q *Queue) Shutdown(ctx context.Context) error {
	q.stop.Do(func() { close(q.jobs) })
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
