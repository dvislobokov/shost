package main

import (
	"context"
	"errors"
	"time"

	"github.com/dvislobokov/shost"
)

// queueWorker simulates a message-queue consumer. Every third batch it
// "crashes" to demonstrate the shost.WithRestart supervision: watch the
// logs for restart attempts with backoff.
type queueWorker struct {
	log     shost.Logger
	batches int
}

func (w *queueWorker) Name() string { return "queue-worker" }

func (w *queueWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.batches++
			if w.batches%3 == 0 {
				return errors.New("simulated consumer crash")
			}
			w.log.Information("processed batch {Batch}", w.batches)
		}
	}
}

func (w *queueWorker) Stop(ctx context.Context) error {
	w.log.Information("worker flushing in-flight batch")
	return nil
}
