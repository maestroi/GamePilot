package boxxle

import (
	"context"
	"fmt"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

const maxMoveFrames = 200
const stepHoldFrames = 16

type controllerEmulator interface {
	memoryReader
	Press(gomeboy.Button)
	Release(gomeboy.Button)
	StepFrame()
}

// MoveFrameObserver is called after each emulator frame the walker already
// executed. It cannot cause extra stepping.
type MoveFrameObserver func(Observation) error

func buttonFor(dir Direction) (gomeboy.Button, bool) {
	switch dir {
	case DirUp:
		return gomeboy.ButtonUp, true
	case DirDown:
		return gomeboy.ButtonDown, true
	case DirLeft:
		return gomeboy.ButtonLeft, true
	case DirRight:
		return gomeboy.ButtonRight, true
	default:
		return 0, false
	}
}

// ExecuteMove holds one D-pad direction until Willy finishes a cell step, then
// waits until the walker is aligned and idle again.
func ExecuteMove(ctx context.Context, emu controllerEmulator, move Move) (Observation, error) {
	return executeMove(ctx, emu, move, nil)
}

// ExecuteMoveObserved is identical to ExecuteMove at the emulator boundary.
func ExecuteMoveObserved(ctx context.Context, emu controllerEmulator, move Move, observer MoveFrameObserver) (Observation, error) {
	return executeMove(ctx, emu, move, observer)
}

func executeMove(ctx context.Context, emu controllerEmulator, move Move, observer MoveFrameObserver) (Observation, error) {
	obs := Observe(emu)
	if !obs.Playing {
		return Observation{}, fmt.Errorf("boxxle: cannot move at frame %d: not in a warehouse", obs.Frame)
	}
	if !obs.Ready {
		return Observation{}, fmt.Errorf("boxxle: cannot move at frame %d: walker is not idle", obs.Frame)
	}
	button, ok := buttonFor(move.Direction)
	if !ok {
		return Observation{}, fmt.Errorf("boxxle: unknown direction %q", move.Direction)
	}

	startX, startY := obs.PlayerX, obs.PlayerY
	if err := waitPushToIdle(ctx, emu, observer); err != nil {
		return Observation{}, err
	}
	heldPush := playerPushing(emu)
	emu.Press(button)
	for i := 0; i < stepHoldFrames; i++ {
		if err := ctx.Err(); err != nil {
			emu.Release(button)
			return Observation{}, err
		}
		now, err := stepObserved(ctx, emu, observer)
		if err != nil {
			emu.Release(button)
			return Observation{}, err
		}
		if !heldPush && i >= 4 && !playerAligned(emu) {
			break
		}
		obs = now
	}
	emu.Release(button)

	moved := false
	var last string
	stable := 0
	now := Observe(emu)
	if observer != nil {
		if err := observer(now); err != nil {
			return Observation{}, err
		}
	}
	for i := 0; i < maxMoveFrames; i++ {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		if now.PlayerX != startX || now.PlayerY != startY {
			moved = true
		}
		sig := RenderGrid(now)
		if now.Ready && moved && sig == last {
			stable++
			if stable >= 3 {
				return now, nil
			}
		} else {
			stable = 0
		}
		last = sig
		if now.Ready && i > 16 && !moved {
			return Observation{}, fmt.Errorf("boxxle: %s from (%d,%d) did not change the walker cell", move.Direction, startX, startY)
		}
		var stepErr error
		now, stepErr = stepObserved(ctx, emu, observer)
		if stepErr != nil {
			return Observation{}, stepErr
		}
	}
	return Observation{}, fmt.Errorf("boxxle: walker did not settle after %s", move.Direction)
}

func waitPushToIdle(ctx context.Context, emu controllerEmulator, observer MoveFrameObserver) error {
	for i := 0; i < 120; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !playerPushing(emu) {
			return nil
		}
		if _, err := stepObserved(ctx, emu, observer); err != nil {
			return err
		}
	}
	return nil
}

func stepObserved(ctx context.Context, emu controllerEmulator, observer MoveFrameObserver) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	emu.StepFrame()
	obs := Observe(emu)
	if observer == nil {
		return obs, nil
	}
	if err := observer(obs); err != nil {
		return Observation{}, err
	}
	return obs, nil
}
