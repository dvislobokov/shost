package cron_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/shost/cron"
)

func mustParse(t *testing.T, layout, s string) time.Time {
	t.Helper()
	v, err := time.Parse(layout, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExprNext(t *testing.T) {
	const layout = "2006-01-02 15:04"
	cases := []struct {
		expr  string
		after string
		want  string
	}{
		{"* * * * *", "2026-07-14 10:30", "2026-07-14 10:31"},
		{"*/15 * * * *", "2026-07-14 10:16", "2026-07-14 10:30"},
		{"0 3 * * *", "2026-07-14 10:30", "2026-07-15 03:00"},
		{"0 3 * * *", "2026-07-14 02:59", "2026-07-14 03:00"},
		{"30 9-17 * * *", "2026-07-14 17:31", "2026-07-15 09:30"},
		{"0 0 1 * *", "2026-07-14 10:30", "2026-08-01 00:00"},
		{"0 12 * * mon", "2026-07-14 10:30", "2026-07-20 12:00"}, // 2026-07-14 is Tuesday
		{"0 12 * * 1", "2026-07-14 10:30", "2026-07-20 12:00"},
		{"0 12 * * 7", "2026-07-14 10:30", "2026-07-19 12:00"}, // 7 = sunday
		{"0 0 1 jan *", "2026-07-14 10:30", "2027-01-01 00:00"},
		{"5,35 10 * * *", "2026-07-14 10:06", "2026-07-14 10:35"},
		{"0 0 29 2 *", "2026-07-14 10:30", "2028-02-29 00:00"}, // leap year
		// dom OR dow when both are restricted: the 15th or any Friday.
		{"0 0 15 * fri", "2026-07-14 10:30", "2026-07-15 00:00"},
		{"0 0 15 * fri", "2026-07-15 10:30", "2026-07-17 00:00"}, // 17th is Friday
		{"@hourly", "2026-07-14 10:30", "2026-07-14 11:00"},
		{"@daily", "2026-07-14 10:30", "2026-07-15 00:00"},
		{"@monthly", "2026-07-14 10:30", "2026-08-01 00:00"},
		{"5/15 * * * *", "2026-07-14 10:36", "2026-07-14 10:50"}, // 5-59/15
	}
	for _, c := range cases {
		s, err := cron.Expr(c.expr)
		if err != nil {
			t.Errorf("Expr(%q): %v", c.expr, err)
			continue
		}
		got := s.Next(mustParse(t, layout, c.after))
		if got.Format(layout) != c.want {
			t.Errorf("Expr(%q).Next(%s) = %s, want %s", c.expr, c.after, got.Format(layout), c.want)
		}
	}
}

func TestExprNeverMatchesReturnsZero(t *testing.T) {
	s := cron.MustExpr("0 0 30 2 *") // February 30th does not exist
	if got := s.Next(mustParse(t, "2006-01-02 15:04", "2026-07-14 10:30")); !got.IsZero() {
		t.Fatalf("expected zero time for impossible schedule, got %v", got)
	}
}

func TestExprErrors(t *testing.T) {
	for _, spec := range []string{
		"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *",
		"* * 0 * *", "* * 32 * *", "* * * 13 *", "* * * * 8",
		"a * * * *", "1-0 * * * *", "*/0 * * * *", "@fortnightly",
	} {
		if _, err := cron.Expr(spec); err == nil {
			t.Errorf("Expr(%q): expected error", spec)
		}
	}
}

func TestAtRunsOnSchedule(t *testing.T) {
	var runs atomic.Int32
	// Fires 10ms after every Next call.
	sched := cron.ScheduleFunc(func(after time.Time) time.Time {
		return after.Add(10 * time.Millisecond)
	})
	svc := cron.At("tick", sched, func(ctx context.Context) error {
		runs.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()

	deadline := time.After(3 * time.Second)
	for runs.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected >= 3 runs, got %d", runs.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestAtZeroScheduleBlocksUntilCancel(t *testing.T) {
	never := cron.ScheduleFunc(func(time.Time) time.Time { return time.Time{} })
	svc := cron.At("never", never, func(ctx context.Context) error {
		t.Error("job must not run")
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()

	select {
	case err := <-done:
		t.Fatalf("Start returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestWithRunTimeout(t *testing.T) {
	var gotErr atomic.Value
	svc := cron.Every("slow", time.Hour, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	},
		cron.RunImmediately(),
		cron.WithRunTimeout(20*time.Millisecond),
		cron.WithErrorHandler(func(err error) { gotErr.Store(err) }),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()

	deadline := time.After(3 * time.Second)
	for gotErr.Load() == nil {
		select {
		case <-deadline:
			t.Fatal("error handler was not called")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := gotErr.Load().(error); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	cancel()
	<-done
}

func TestWithJitterDelaysRun(t *testing.T) {
	var runs atomic.Int32
	svc := cron.Every("j", time.Hour, func(ctx context.Context) error {
		runs.Add(1)
		return nil
	}, cron.RunImmediately(), cron.WithJitter(time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.Start(ctx) }()

	deadline := time.After(3 * time.Second)
	for runs.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("immediate run did not happen")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
