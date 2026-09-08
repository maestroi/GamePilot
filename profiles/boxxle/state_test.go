package boxxle

import "testing"

type fakeMemory struct {
	data  [1 << 16]byte
	frame uint64
}

func (m *fakeMemory) Peek8(addr uint16) byte { return m.data[addr] }
func (m *fakeMemory) PeekInto(addr uint16, dst []byte) {
	for i := range dst {
		dst[i] = m.data[addr+uint16(i)]
	}
}
func (m *fakeMemory) FrameCount() uint64 { return m.frame }

func fillFloor(mem *fakeMemory) {
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			addr := cellTileAddr(col, row)
			mem.data[addr] = tileFloor
			mem.data[addr+1] = tileFloor
		}
	}
}

func setCell(mem *fakeMemory, col, row int, tile byte) {
	addr := cellTileAddr(col, row)
	mem.data[addr] = tile
	mem.data[addr+1] = tile
}

func TestObserveDecodesWarehouseAndPlayer(t *testing.T) {
	mem := &fakeMemory{frame: 99}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 2, 2, tileBox)
	setCell(mem, 4, 3, tileGoal)

	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80
	mem.data[moveCountAddr] = 7
	mem.data[setNumberAddr] = 1

	obs := Observe(mem)
	if obs.Frame != 99 {
		t.Fatalf("Frame = %d, want 99", obs.Frame)
	}
	if obs.Grid[0][0] != Wall || obs.Grid[2][2] != Box || obs.Grid[3][4] != Goal {
		t.Fatalf("grid wall/box/goal = %v/%v/%v", obs.Grid[0][0], obs.Grid[2][2], obs.Grid[3][4])
	}
	if obs.PlayerX != 1 || obs.PlayerY != 1 || obs.Grid[1][1] != Player {
		t.Fatalf("player = (%d,%d) cell=%s, want (1,1) @", obs.PlayerX, obs.PlayerY, obs.Grid[1][1])
	}
	if obs.Moves != 7 || obs.Set != 1 {
		t.Fatalf("Moves/Set = %d/%d, want 7/1", obs.Moves, obs.Set)
	}
	if !obs.Playing || !obs.Ready || obs.Solved {
		t.Fatalf("Playing/Ready/Solved = %v/%v/%v", obs.Playing, obs.Ready, obs.Solved)
	}
}

func TestObserveNotReadyWhileWalking(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels + 8
	mem.data[playerOAMTile] = 0x84

	obs := Observe(mem)
	if !obs.Playing {
		t.Fatal("expected still playing while walking")
	}
	if obs.Ready {
		t.Fatal("expected not ready while the walker is between cells")
	}
}

func TestObservePlayingWithPushPose(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	mem.data[playerOAMY] = oamYOrigin + 2*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 2*cellPixels
	mem.data[playerOAMTile] = 0x94

	obs := Observe(mem)
	if !obs.Playing || !obs.Ready {
		t.Fatalf("Playing/Ready = %v/%v, want true/true for push pose 0x94", obs.Playing, obs.Ready)
	}
}

func TestObserveOverlaysPushedBoxFromOAM(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 3, 1, tileGoal)
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80
	mem.data[0xFE10] = byte(oamYOrigin + 1*cellPixels)
	mem.data[0xFE11] = byte(oamXOrigin + 3*cellPixels)
	mem.data[0xFE12] = tileBox

	obs := Observe(mem)
	if obs.Grid[1][3] != BoxOnGoal {
		t.Fatalf("OAM box on goal = %s, want *", obs.Grid[1][3])
	}
}

func TestObserveOverlaysBoxOnGoalSprite(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80
	mem.data[0xFE10] = byte(oamYOrigin + 1*cellPixels)
	mem.data[0xFE11] = byte(oamXOrigin + 3*cellPixels)
	mem.data[0xFE12] = tileBoxOnGoal

	obs := Observe(mem)
	if obs.Grid[1][3] != BoxOnGoal {
		t.Fatalf("OAM box-on-goal sprite = %s, want *", obs.Grid[1][3])
	}
}

func TestObserveDecodesBoxOnGoalTile(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 7, 5, tileBoxOnGoal)
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80

	obs := Observe(mem)
	if obs.Grid[5][7] != BoxOnGoal {
		t.Fatalf("tile $AC cell = %s, want box-on-goal", obs.Grid[5][7])
	}
}

func TestObserveTreatsUnknownSolidTilesAsWalls(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 4, 4, 0x00)
	mem.data[playerOAMY] = oamYOrigin + 1*cellPixels
	mem.data[playerOAMX] = oamXOrigin + 1*cellPixels
	mem.data[playerOAMTile] = 0x80

	obs := Observe(mem)
	if obs.Grid[4][4] != Wall {
		t.Fatalf("unknown tile cell = %s, want wall", obs.Grid[4][4])
	}
}

func TestRenderGrid(t *testing.T) {
	mem := &fakeMemory{}
	fillFloor(mem)
	setCell(mem, 0, 0, tileWall)
	setCell(mem, 1, 0, tileWall)
	mem.data[playerOAMY] = oamYOrigin
	mem.data[playerOAMX] = oamXOrigin
	mem.data[playerOAMTile] = 0x80
	// Player overlays (0,0) which is a wall in this fixture; still renders the grid.

	got := RenderGrid(Observe(mem))
	if got[0] != '#' {
		t.Fatalf("RenderGrid starts with %q, want wall", got[:1])
	}
}
