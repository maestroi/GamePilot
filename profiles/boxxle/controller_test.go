package boxxle

import (
	"context"
	"testing"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

type fakeController struct {
	fakeMemory
	x, y       int
	pressLog   []gomeboy.Button
	held       gomeboy.Button
	holding    bool
	walkFrames int
	pressed    bool
}

func newFakeController() *fakeController {
	f := &fakeController{x: 1, y: 1}
	f.frame = 10
	fillFloor(&f.fakeMemory)
	setCell(&f.fakeMemory, 0, 0, tileWall)
	setCell(&f.fakeMemory, 0, 1, tileWall)
	setCell(&f.fakeMemory, 4, 1, tileWall)
	f.syncSprites()
	return f
}

func (f *fakeController) syncSprites() {
	f.data[playerOAMX] = byte(oamXOrigin + f.x*cellPixels)
	f.data[playerOAMY] = byte(oamYOrigin + f.y*cellPixels)
	if f.walkFrames > 0 {
		f.data[playerOAMTile] = 0x84
	} else {
		f.data[playerOAMTile] = 0x80
	}
}

func (f *fakeController) Press(button gomeboy.Button) {
	f.pressLog = append(f.pressLog, button)
	f.held = button
	f.holding = true
	f.pressed = true
}

func (f *fakeController) Release(gomeboy.Button) {
	f.holding = false
}

func (f *fakeController) StepFrame() {
	f.frame++
	if f.pressed && f.walkFrames == 0 {
		switch f.held {
		case gomeboy.ButtonRight:
			f.x++
		case gomeboy.ButtonLeft:
			f.x--
		case gomeboy.ButtonDown:
			f.y++
		case gomeboy.ButtonUp:
			f.y--
		}
		f.walkFrames = 3
		f.data[moveCountAddr]++
		f.pressed = false
	}
	if f.walkFrames > 0 {
		f.walkFrames--
	}
	f.syncSprites()
}

func TestExecuteMoveStepsOneCellRight(t *testing.T) {
	emu := newFakeController()
	obs, err := ExecuteMove(context.Background(), emu, Move{Direction: DirRight})
	if err != nil {
		t.Fatal(err)
	}
	if obs.PlayerX != 2 || obs.PlayerY != 1 {
		t.Fatalf("player = (%d,%d), want (2,1)", obs.PlayerX, obs.PlayerY)
	}
	if !obs.Ready {
		t.Fatal("expected settled observation")
	}
	if len(emu.pressLog) != 1 || emu.pressLog[0] != gomeboy.ButtonRight {
		t.Fatalf("pressLog = %v, want [Right]", emu.pressLog)
	}
}

func TestExecuteMoveObservedReportsControllerFrames(t *testing.T) {
	emu := newFakeController()
	frames := 0
	obs, err := ExecuteMoveObserved(context.Background(), emu, Move{Direction: DirRight}, func(Observation) error {
		frames++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if obs.PlayerX != 2 || obs.PlayerY != 1 {
		t.Fatalf("player = (%d,%d), want (2,1)", obs.PlayerX, obs.PlayerY)
	}
	if frames < 3 {
		t.Fatalf("observer frames = %d, want at least the walk animation", frames)
	}
}

func TestChooseHeuristicMoveWalksTowardABox(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 3, 1, tileBox)
	setCell(mem, 5, 1, tileGoal)
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80

	obs := Observe(mem)
	decision, err := ChooseHeuristicMove(obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Move.Direction != DirRight {
		t.Fatalf("direction = %s, want right", decision.Move.Direction)
	}
}
