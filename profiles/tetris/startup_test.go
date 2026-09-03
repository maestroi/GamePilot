package tetris

import (
	"context"
	"testing"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

type fakeStartupEmulator struct {
	state       byte
	initialized bool
	startHeld   bool
	frames      int
}

func (e *fakeStartupEmulator) Peek8(addr uint16) byte {
	if addr == gameStateAddr {
		return e.state
	}
	return 0
}

func (e *fakeStartupEmulator) Press(button gomeboy.Button) {
	if button == gomeboy.ButtonStart {
		e.startHeld = true
	}
}

func (e *fakeStartupEmulator) Release(button gomeboy.Button) {
	if button == gomeboy.ButtonStart {
		e.startHeld = false
	}
}

func (e *fakeStartupEmulator) StepFrame() {
	e.frames++
	if !e.initialized {
		if e.frames >= 2 {
			e.initialized = true
			e.state = 0x24
		}
		return
	}

	switch e.state {
	case 0x24:
		e.state = 0x25
	case 0x25:
		e.state = 0x35
	case 0x35:
		if e.startHeld {
			e.state = 0x06
		}
	case 0x06:
		e.state = 0x07
	case 0x07:
		if e.startHeld {
			e.state = 0x08
		}
	case 0x08:
		e.state = 0x0E
	case 0x0E:
		if e.startHeld {
			e.state = 0x10
		}
	case 0x10:
		e.state = 0x11
	case 0x11:
		if e.startHeld {
			e.state = 0x0A
		}
	case 0x0A:
		e.state = 0x00
	}
}

func TestStartTypeAZeroDoesNotMistakeFreshZeroedWRAMForGameplay(t *testing.T) {
	emu := &fakeStartupEmulator{}
	if err := StartTypeAZero(context.Background(), emu); err != nil {
		t.Fatalf("StartTypeAZero() error = %v", err)
	}
	if !emu.initialized || emu.state != 0 {
		t.Fatalf("startup ended initialized=%v state=0x%02x, want true/0x00", emu.initialized, emu.state)
	}
	if emu.frames <= 2 {
		t.Fatalf("startup returned too early after %d frames", emu.frames)
	}
}
