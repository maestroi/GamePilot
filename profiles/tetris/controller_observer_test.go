package tetris

import (
	"context"
	"errors"
	"testing"
)

func TestExecutePlacementObservedPreservesFrameAndState(t *testing.T) {
	plain := newControllerFake()
	observed := newControllerFake()
	placement := Placement{Rotation: 1, TargetColumn: 6}

	plainFinal, err := ExecutePlacement(context.Background(), plain, placement)
	if err != nil {
		t.Fatal(err)
	}

	var callbacks int
	var callbackFrames []uint64
	observedFinal, err := ExecutePlacementObserved(context.Background(), observed, placement, func(obs Observation) error {
		callbacks++
		callbackFrames = append(callbackFrames, obs.Frame)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if observed.frame != plain.frame {
		t.Fatalf("observed emulator frame = %d, plain = %d", observed.frame, plain.frame)
	}
	if observedFinal != plainFinal {
		t.Fatalf("observed final state differs:\nobserved=%+v\nplain=%+v", observedFinal, plainFinal)
	}
	if callbacks != int(observed.frame) {
		t.Fatalf("callbacks = %d, want one per executed frame = %d", callbacks, observed.frame)
	}
	for i, frame := range callbackFrames {
		want := uint64(i + 1)
		if frame != want {
			t.Fatalf("callback[%d] frame = %d, want %d", i, frame, want)
		}
	}
}

func TestExecutePlacementObservedAbortsWithoutExtraFrame(t *testing.T) {
	emu := newControllerFake()
	stop := errors.New("stop presentation")
	calls := 0

	_, err := ExecutePlacementObserved(context.Background(), emu, Placement{Rotation: 1, TargetColumn: 6}, func(Observation) error {
		calls++
		if calls == 3 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("error = %v, want observer error", err)
	}
	if emu.frame != 3 {
		t.Fatalf("emulator frame = %d, want exactly 3", emu.frame)
	}
}
