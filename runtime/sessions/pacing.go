package sessions

import (
	"context"
	"fmt"
	"time"
)

// PacingMode controls only wall-clock presentation of already-required emulator
// frames. It never changes Tetris gravity, controller inputs, or frame counts.
type PacingMode string

const (
	PacingFast     PacingMode = "fast"
	PacingRealtime PacingMode = "realtime"

	// DMG Game Boy frame duration: 70224 CPU clocks per frame at 4194304 Hz.
	gameBoyFrameDuration = time.Duration(int64(time.Second) * 70224 / 4194304)

	// PNG publication at ~30 fps is enough to show movement/rotation while the
	// pacer still schedules every emulator frame at the native cadence.
	presentationSampleEveryFrames = 2
)

type framePacer interface {
	Wait(ctx context.Context, emulatorFrame uint64) error
}

type pacerFactory func(mode PacingMode) framePacer

type fastFramePacer struct{}

func (fastFramePacer) Wait(ctx context.Context, _ uint64) error { return ctx.Err() }

type realtimeFramePacer struct {
	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	started   bool
	baseFrame uint64
	baseTime  time.Time
}

func defaultPacerFactory(mode PacingMode) framePacer {
	if mode == PacingRealtime {
		return &realtimeFramePacer{now: time.Now, sleep: sleepContext}
	}
	return fastFramePacer{}
}

func (p *realtimeFramePacer) Wait(ctx context.Context, emulatorFrame uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.started {
		p.started = true
		p.baseFrame = emulatorFrame
		p.baseTime = p.now()
		return nil
	}
	if emulatorFrame <= p.baseFrame {
		return nil
	}

	frames := emulatorFrame - p.baseFrame
	target := p.baseTime.Add(time.Duration(frames) * gameBoyFrameDuration)
	delay := target.Sub(p.now())
	if delay <= 0 {
		return ctx.Err()
	}
	return p.sleep(ctx, delay)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizePacing(mode PacingMode) (PacingMode, error) {
	if mode == "" {
		return PacingFast, nil
	}
	switch mode {
	case PacingFast, PacingRealtime:
		return mode, nil
	default:
		return "", fmt.Errorf("sessions: unsupported pacing mode %q (want %q or %q)", mode, PacingFast, PacingRealtime)
	}
}
