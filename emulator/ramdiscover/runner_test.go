package ramdiscover

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeState struct {
	Memory  [65536]byte
	Frame   uint64
	Pressed map[Input]bool
}

type fakeMachine struct {
	state fakeState
}

func newFakeMachine() *fakeMachine {
	m := &fakeMachine{}
	m.state.Memory[0xC202] = 80
	m.state.Memory[0xC203] = 12
	m.state.Pressed = map[Input]bool{}
	return m
}

func (m *fakeMachine) SaveCheckpoint() ([]byte, error)  { return json.Marshal(m.state) }
func (m *fakeMachine) LoadCheckpoint(data []byte) error { return json.Unmarshal(data, &m.state) }
func (m *fakeMachine) PeekInto(addr uint16, dst []byte) {
	for i := range dst {
		dst[i] = m.state.Memory[int(addr)+i]
	}
}
func (m *fakeMachine) FrameCount() uint64        { return m.state.Frame }
func (m *fakeMachine) Press(input Input) error   { m.state.Pressed[input] = true; return nil }
func (m *fakeMachine) Release(input Input) error { delete(m.state.Pressed, input); return nil }
func (m *fakeMachine) StepFrame() {
	m.state.Frame++
	m.state.Memory[0xC100]++ // unrelated timer: should cancel against idle control.
	if m.state.Pressed[InputLeft] {
		m.state.Memory[0xC202] -= 8
	}
	if m.state.Pressed[InputRight] {
		m.state.Memory[0xC202] += 8
	}
	if m.state.Pressed[InputA] {
		m.state.Memory[0xC203]--
	}
	if m.state.Pressed[InputB] {
		m.state.Memory[0xC203]++
	}
}

func TestRunFiltersIdleNoiseAndFindsOppositeDeltas(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ranges = []MemoryRange{{Name: "test", Start: 0xC100, End: 0xC203}}
	cfg.Actions = []Action{
		{Name: "left", Input: InputLeft, HoldFrames: 1, ReleaseFrames: 2},
		{Name: "right", Input: InputRight, HoldFrames: 1, ReleaseFrames: 2},
		{Name: "a", Input: InputA, HoldFrames: 1, ReleaseFrames: 2},
		{Name: "b", Input: InputB, HoldFrames: 1, ReleaseFrames: 2},
	}
	cfg.Pairs = []ActionPair{{First: "left", Second: "right"}, {First: "a", Second: "b"}}
	cfg.Trials = 2
	cfg.TrialAdvanceFrames = 1

	report, err := Run(context.Background(), newFakeMachine(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if hasAddress(report.Actions[0].Candidates, 0xC100) {
		t.Fatalf("idle timer leaked into candidates")
	}
	if !hasAddress(report.Actions[0].Candidates, 0xC202) {
		t.Fatalf("left did not discover x address")
	}
	if !hasAddress(report.Actions[2].Candidates, 0xC203) {
		t.Fatalf("A did not discover rotation address")
	}
	if !hasRelation(report.Relations, 0xC202, "left", "right", -8, 8) {
		t.Fatalf("missing left/right relation: %#v", report.Relations)
	}
	if !hasRelation(report.Relations, 0xC203, "a", "b", -1, 1) {
		t.Fatalf("missing a/b relation: %#v", report.Relations)
	}
}

func TestRunRestoresRootCheckpoint(t *testing.T) {
	m := newFakeMachine()
	before, _ := m.SaveCheckpoint()
	cfg := DefaultConfig()
	cfg.Ranges = []MemoryRange{{Name: "test", Start: 0xC100, End: 0xC203}}
	cfg.Trials = 2
	if _, err := Run(context.Background(), m, cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := m.SaveCheckpoint()
	if string(before) != string(after) {
		t.Fatalf("machine state changed after discovery")
	}
}

func hasAddress(candidates []Candidate, addr uint16) bool {
	for _, c := range candidates {
		if c.AddressDecimal == addr {
			return true
		}
	}
	return false
}
func hasRelation(relations []Relation, addr uint16, first, second string, d1, d2 int) bool {
	needle := "0x" + hex4(addr)
	for _, r := range relations {
		if r.Address == needle && r.Actions == [2]string{first, second} && r.FirstDelta == d1 && r.SecondDelta == d2 {
			return true
		}
	}
	return false
}
func hex4(v uint16) string {
	const chars = "0123456789ABCDEF"
	b := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		b[i] = chars[v&0xf]
		v >>= 4
	}
	return string(b)
}
