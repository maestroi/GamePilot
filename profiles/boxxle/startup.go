package boxxle

import (
	"context"
	"fmt"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

const maxBootFrames = 2500

type startupEmulator interface {
	Peek8(addr uint16) byte
	Press(button gomeboy.Button)
	Release(button gomeboy.Button)
	StepFrame()
}

// StartRoom1 deterministically navigates a freshly loaded BOXXLE dump to
// set 1 room 1 (Play -> Play, skipping Create and Passkey).
func StartRoom1(ctx context.Context, emu startupEmulator) error {
	for i := 0; i < 240; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		emu.StepFrame()
	}
	if err := pulse(ctx, emu, gomeboy.ButtonStart); err != nil {
		return err
	}
	for i := 0; i < 90; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		emu.StepFrame()
	}
	if err := pulse(ctx, emu, gomeboy.ButtonA); err != nil {
		return err
	}
	for i := 0; i < 90; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		emu.StepFrame()
	}
	if err := pulse(ctx, emu, gomeboy.ButtonA); err != nil {
		return err
	}

	for frames := 0; frames < maxBootFrames; frames++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if warehouseReady(emu) {
			return nil
		}
		emu.StepFrame()
	}
	return fmt.Errorf("boxxle: did not reach room 1 within %d frames", maxBootFrames)
}

func warehouseReady(emu startupEmulator) bool {
	if emu.Peek8(cellTileAddr(0, 0)) != tileWall {
		return false
	}
	if emu.Peek8(playerOAMTile)&0xE0 != 0x80 {
		return false
	}
	x := int(emu.Peek8(playerOAMX)) - oamXOrigin
	y := int(emu.Peek8(playerOAMY)) - oamYOrigin
	return x >= 0 && y >= 0 && x%cellPixels == 0 && y%cellPixels == 0
}

func pulse(ctx context.Context, emu startupEmulator, button gomeboy.Button) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	emu.Press(button)
	emu.StepFrame()
	emu.Release(button)
	if err := ctx.Err(); err != nil {
		return err
	}
	emu.StepFrame()
	return ctx.Err()
}
