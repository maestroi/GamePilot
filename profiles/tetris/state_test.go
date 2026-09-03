package tetris

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

func TestObserveDecodesKnownMemoryLayout(t *testing.T) {
	mem := &fakeMemory{frame: 1234}
	fillEmptyBoard(mem)

	mem.data[playfieldBase+17*playfieldStride] = 0x80
	mem.data[playfieldBase+17*playfieldStride+9] = 0x86
	mem.data[playfieldBase+10*playfieldStride+4] = 0x8C

	mem.data[activeVisibleAddr] = 0x00
	mem.data[activeYAddr] = 0x18
	mem.data[activeXAddr] = 0x3F
	mem.data[activePieceAddr] = 0x1A // T, rotation 2
	mem.data[previewPieceAddr] = 0x09 // I, rotation 1

	mem.data[scoreAddr] = 0x00
	mem.data[scoreAddr+1] = 0x34
	mem.data[scoreAddr+2] = 0x00
	mem.data[linesAddr] = 0x12
	mem.data[linesAddr+1] = 0x00
	mem.data[levelAddr] = 2
	mem.data[gameStateAddr] = 0
	mem.data[lockPhaseAddr] = 0
	mem.data[wipeCounterAddr] = 0

	obs, err := Observe(mem)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	if obs.Frame != 1234 {
		t.Fatalf("Frame = %d, want 1234", obs.Frame)
	}
	if obs.Board[17][0] != Filled || obs.Board[17][9] != Filled || obs.Board[10][4] != Filled {
		t.Fatal("expected occupied playfield cells to decode as Filled")
	}
	if obs.Board[0][0] != Empty {
		t.Fatal("expected 0x2F playfield tile to decode as Empty")
	}
	if obs.CurrentPiece.Kind != PieceT || obs.CurrentPiece.Rotation != 2 {
		t.Fatalf("CurrentPiece = %+v, want T rotation 2", obs.CurrentPiece)
	}
	if obs.CurrentPiece.AnchorX != 0x3F || obs.CurrentPiece.AnchorY != 0x18 || !obs.CurrentPiece.Visible {
		t.Fatalf("CurrentPiece position/visibility = %+v", obs.CurrentPiece)
	}
	if obs.NextPiece.Kind != PieceI || obs.NextPiece.Rotation != 1 {
		t.Fatalf("NextPiece = %+v, want I rotation 1", obs.NextPiece)
	}
	if obs.Score != 3400 {
		t.Fatalf("Score = %d, want 3400", obs.Score)
	}
	if obs.Lines != 12 {
		t.Fatalf("Lines = %d, want 12", obs.Lines)
	}
	if obs.Level != 2 {
		t.Fatalf("Level = %d, want 2", obs.Level)
	}
	if !obs.Ready || obs.GameOver {
		t.Fatalf("Ready/GameOver = %v/%v, want true/false", obs.Ready, obs.GameOver)
	}
}

func TestObserveGameOverStates(t *testing.T) {
	for _, state := range []byte{0x01, 0x0D, 0x04, 0x34} {
		mem := &fakeMemory{}
		fillEmptyBoard(mem)
		mem.data[activeVisibleAddr] = 0x80
		mem.data[gameStateAddr] = state

		obs, err := Observe(mem)
		if err != nil {
			t.Fatalf("state 0x%02x: Observe() error = %v", state, err)
		}
		if !obs.GameOver {
			t.Fatalf("state 0x%02x: GameOver = false, want true", state)
		}
	}
}

func TestDecodePackedBCDRejectsInvalidDigits(t *testing.T) {
	if _, err := decodePackedBCD([]byte{0xFA}); err == nil {
		t.Fatal("expected invalid packed BCD to return an error")
	}
}

func fillEmptyBoard(mem *fakeMemory) {
	for row := 0; row < BoardRows; row++ {
		for column := 0; column < BoardColumns; column++ {
			mem.data[playfieldBase+uint16(row)*playfieldStride+uint16(column)] = emptyTile
		}
	}
}
