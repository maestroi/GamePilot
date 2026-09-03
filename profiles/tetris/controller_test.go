package tetris

import (
	"context"
	"strings"
	"testing"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

func TestPieceCellsUseROMGeometry(t *testing.T) {
	cells, err := PieceCells(PieceI, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []CellOffset{{1, 0}, {1, 1}, {1, 2}, {1, 3}}
	if len(cells) != len(want) {
		t.Fatalf("len(cells) = %d, want %d", len(cells), len(want))
	}
	for i := range want {
		if cells[i] != want[i] {
			t.Fatalf("cells[%d] = %+v, want %+v", i, cells[i], want[i])
		}
	}
}

func TestExecutePlacementMovesAndWaitsForNextPiece(t *testing.T) {
	emu := newControllerFake()

	obs, err := ExecutePlacement(context.Background(), emu, Placement{
		Rotation:     1,
		TargetColumn: 6,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !obs.Ready {
		t.Fatal("final observation is not ready")
	}
	if obs.CurrentPiece.Kind != PieceJ {
		t.Fatalf("final current piece = %s, want J", obs.CurrentPiece.Kind)
	}

	var bPresses, rightPresses, downPresses int
	for _, button := range emu.pressLog {
		switch button {
		case gomeboy.ButtonB:
			bPresses++
		case gomeboy.ButtonRight:
			rightPresses++
		case gomeboy.ButtonDown:
			downPresses++
		}
	}
	if bPresses != 1 {
		t.Fatalf("B presses = %d, want 1", bPresses)
	}
	if rightPresses != 2 {
		t.Fatalf("right presses = %d, want 2", rightPresses)
	}
	if downPresses != 1 {
		t.Fatalf("down presses = %d, want 1", downPresses)
	}
}

func TestExecutePlacementFarRightOStopsAtROMWallAnchor(t *testing.T) {
	emu := newControllerFake()
	emu.mem[activePieceAddr] = 0x0C // O, rotation 0
	emu.maxAnchorX = 95             // Rev 1 far-right reachable anchor for this placement.

	_, err := ExecutePlacement(context.Background(), emu, Placement{
		Rotation:     0,
		TargetColumn: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	rightPresses := 0
	for _, button := range emu.pressLog {
		if button == gomeboy.ButtonRight {
			rightPresses++
		}
	}
	if rightPresses != 4 {
		t.Fatalf("right presses = %d, want 4", rightPresses)
	}
}

func TestExecutePlacementRejectsIllegalColumn(t *testing.T) {
	emu := newControllerFake()
	emu.mem[activePieceAddr] = 0x08 // I, rotation 0

	_, err := ExecutePlacement(context.Background(), emu, Placement{
		Rotation:     0,
		TargetColumn: 7,
	})
	if err == nil || !strings.Contains(err.Error(), "illegal") {
		t.Fatalf("error = %v, want illegal target column", err)
	}
}

func TestExecutePlacementReportsBlockedHorizontalMove(t *testing.T) {
	emu := newControllerFake()
	emu.blockRight = true

	_, err := ExecutePlacement(context.Background(), emu, Placement{
		Rotation:     0,
		TargetColumn: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "horizontal move blocked") {
		t.Fatalf("error = %v, want blocked horizontal move", err)
	}
}

type controllerFake struct {
	mem           [1 << 16]byte
	frame         uint64
	pressed       map[gomeboy.Button]bool
	pressLog      []gomeboy.Button
	blockRight    bool
	maxAnchorX    byte
	downFrames    int
	lockCountdown int
}

func newControllerFake() *controllerFake {
	f := &controllerFake{
		pressed:    make(map[gomeboy.Button]bool),
		maxAnchorX: 0xFF,
	}
	for row := 0; row < BoardRows; row++ {
		for col := 0; col < BoardColumns; col++ {
			f.mem[int(playfieldBase)+row*int(playfieldStride)+col] = emptyTile
		}
	}
	f.mem[activeVisibleAddr] = 0
	f.mem[activeYAddr] = 24
	f.mem[activeXAddr] = 63
	f.mem[activePieceAddr] = 0x18 // T, rotation 0
	f.mem[previewPieceAddr] = 0x04 // J
	f.mem[gameStateAddr] = 0
	f.mem[lockPhaseAddr] = 0
	f.mem[wipeCounterAddr] = 0
	return f
}

func (f *controllerFake) Peek8(addr uint16) byte { return f.mem[addr] }

func (f *controllerFake) PeekInto(addr uint16, dst []byte) {
	copy(dst, f.mem[int(addr):int(addr)+len(dst)])
}

func (f *controllerFake) FrameCount() uint64 { return f.frame }

func (f *controllerFake) Press(button gomeboy.Button) {
	f.pressed[button] = true
	f.pressLog = append(f.pressLog, button)
}

func (f *controllerFake) Release(button gomeboy.Button) { f.pressed[button] = false }

func (f *controllerFake) StepFrame() {
	f.frame++

	if f.lockCountdown > 0 {
		f.lockCountdown--
		if f.lockCountdown == 0 {
			f.mem[activeVisibleAddr] = 0
			f.mem[lockPhaseAddr] = 0
			f.mem[activeYAddr] = 24
			f.mem[activeXAddr] = 63
			f.mem[activePieceAddr] = 0x04 // J, rotation 0
			f.mem[previewPieceAddr] = 0x08 // I
		}
		return
	}

	piece := f.mem[activePieceAddr]
	if f.pressed[gomeboy.ButtonA] {
		base := piece &^ 0x03
		rot := (piece - 1) & 0x03
		f.mem[activePieceAddr] = base | rot
	}
	if f.pressed[gomeboy.ButtonB] {
		base := piece &^ 0x03
		rot := (piece + 1) & 0x03
		f.mem[activePieceAddr] = base | rot
	}
	if f.pressed[gomeboy.ButtonLeft] {
		f.mem[activeXAddr] -= 8
	}
	if f.pressed[gomeboy.ButtonRight] && !f.blockRight && f.mem[activeXAddr] < f.maxAnchorX {
		f.mem[activeXAddr] += 8
	}
	if f.pressed[gomeboy.ButtonDown] {
		f.downFrames++
		if f.downFrames < 3 {
			f.mem[activeYAddr] += 8
		} else {
			f.mem[activeVisibleAddr] = 0x80
			f.mem[lockPhaseAddr] = 1
			f.lockCountdown = 2
		}
	}
}
