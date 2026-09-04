package sessions

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalizePacingDefaultsToFast(t *testing.T) {
	got, err := normalizePacing("")
	if err != nil {
		t.Fatal(err)
	}
	if got != PacingFast {
		t.Fatalf("default pacing = %q, want %q", got, PacingFast)
	}
	if _, err := normalizePacing("turbo"); err == nil {
		t.Fatal("expected unsupported pacing error")
	}
}

func TestRealtimeFramePacerTargetsGameBoyCadenceWithoutSleepingInTest(t *testing.T) {
	now := time.Unix(100, 0)
	var sleeps []time.Duration
	p := &realtimeFramePacer{
		now: func() time.Time { return now },
		sleep: func(_ context.Context, delay time.Duration) error {
			sleeps = append(sleeps, delay)
			now = now.Add(delay)
			return nil
		},
	}

	ctx := context.Background()
	if err := p.Wait(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 0 {
		t.Fatalf("first frame sleeps = %v, want none", sleeps)
	}
	if err := p.Wait(ctx, 201); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(ctx, 202); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleep count = %d, want 2", len(sleeps))
	}
	for i, delay := range sleeps {
		if delay != gameBoyFrameDuration {
			t.Fatalf("sleep[%d] = %s, want %s", i, delay, gameBoyFrameDuration)
		}
	}
}

func TestRealtimeFramePacerAccountsForWorkTime(t *testing.T) {
	base := time.Unix(200, 0)
	now := base
	var got time.Duration
	p := &realtimeFramePacer{
		now: func() time.Time { return now },
		sleep: func(_ context.Context, delay time.Duration) error {
			got = delay
			return nil
		},
	}
	if err := p.Wait(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Millisecond)
	if err := p.Wait(context.Background(), 11); err != nil {
		t.Fatal(err)
	}
	want := gameBoyFrameDuration - 5*time.Millisecond
	if got != want {
		t.Fatalf("sleep = %s, want %s", got, want)
	}
}

func TestSleepContextCancelsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := sleepContext(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("cancelled sleep did not return promptly")
	}
}
