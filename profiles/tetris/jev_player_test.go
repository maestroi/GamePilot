package tetris

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"testing"

	"github.com/maestroi/GamePilot/emulator/ramclassify"
	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

func TestChooseJevPlacementWithShadowRestoresAndSelectsBoundedOutcome(t *testing.T) {
	emu := newControllerFake()
	beforeMem := emu.mem
	beforeFrame := emu.frame
	beforeDownFrames := emu.downFrames
	beforeLockCountdown := emu.lockCountdown
	beforePressLog := append([]gomeboy.Button(nil), emu.pressLog...)

	obs, err := Observe(emu)
	if err != nil {
		t.Fatal(err)
	}
	engine := &testJevDecisionEngine{choice: "placement_01"}

	decision, err := ChooseJevPlacementWithShadow(
		context.Background(),
		emu,
		engine,
		obs,
		JevPlacementConfig{Model: "test-jev", MaxCandidates: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	if engine.calls != 1 {
		t.Fatalf("engine calls = %d, want 1", engine.calls)
	}
	if got := len(engine.criteria); got != 3 {
		t.Fatalf("bounded criteria = %d, want 3", got)
	}
	if decision.Choice != "placement_01" {
		t.Fatalf("choice = %q, want placement_01", decision.Choice)
	}
	if decision.Candidates != 3 {
		t.Fatalf("candidates = %d, want 3", decision.Candidates)
	}
	if decision.TotalCandidates < decision.Candidates {
		t.Fatalf("total candidates = %d, want >= %d", decision.TotalCandidates, decision.Candidates)
	}
	if decision.Placement != decision.Outcomes[1].Placement {
		t.Fatalf("placement = %+v, want second verified outcome %+v", decision.Placement, decision.Outcomes[1].Placement)
	}
	if decision.Probability != 0.8 {
		t.Fatalf("probability = %v, want 0.8", decision.Probability)
	}
	if decision.Confidence != 0.6 {
		t.Fatalf("confidence = %v, want 0.6", decision.Confidence)
	}

	if emu.frame != beforeFrame {
		t.Fatalf("live frame = %d after shadow search, want restored %d", emu.frame, beforeFrame)
	}
	if emu.mem != beforeMem {
		t.Fatal("live memory changed after shadow search")
	}
	if emu.downFrames != beforeDownFrames || emu.lockCountdown != beforeLockCountdown {
		t.Fatal("controller timing state changed after shadow search")
	}
	if len(emu.pressLog) != len(beforePressLog) {
		t.Fatalf("press log length = %d after shadow search, want %d", len(emu.pressLog), len(beforePressLog))
	}
	for button, pressed := range emu.pressed {
		if pressed {
			t.Fatalf("button %v remained pressed after checkpoint restore", button)
		}
	}
}

func TestChooseJevPlacementWithShadowRejectsInventedChoiceAndRestores(t *testing.T) {
	emu := newControllerFake()
	beforeMem := emu.mem
	beforeFrame := emu.frame

	obs, err := Observe(emu)
	if err != nil {
		t.Fatal(err)
	}
	engine := &testJevDecisionEngine{choice: "invented_placement"}

	_, err = ChooseJevPlacementWithShadow(
		context.Background(),
		emu,
		engine,
		obs,
		JevPlacementConfig{Model: "test-jev", MaxCandidates: 2},
	)
	if err == nil {
		t.Fatal("expected invented Jev choice to be rejected")
	}
	if emu.frame != beforeFrame || emu.mem != beforeMem {
		t.Fatal("live emulator was not restored after rejected Jev choice")
	}
}

type testJevDecisionEngine struct {
	choice   string
	calls    int
	criteria map[string]string
}

func (e *testJevDecisionEngine) Decide(
	_ context.Context,
	_ string,
	_ any,
	questions map[string]ramclassify.Question,
) (ramclassify.DecisionResponse, error) {
	e.calls++
	question, ok := questions["placement"]
	if !ok {
		return ramclassify.DecisionResponse{}, fmt.Errorf("missing placement question")
	}
	e.criteria = question.Criteria
	probabilities := make(map[string]float64, len(question.Criteria)+1)
	for option := range question.Criteria {
		probabilities[option] = 0.05
	}
	probabilities[e.choice] = 0.8
	return ramclassify.DecisionResponse{
		Model: "test-jev",
		Answers: map[string]ramclassify.Answer{
			"placement": {
				Type:          "choice",
				Choice:        e.choice,
				Probabilities: probabilities,
				Confidence:    0.6,
			},
		},
	}, nil
}

type controllerFakeCheckpoint struct {
	Mem           []byte
	Frame         uint64
	Pressed       map[gomeboy.Button]bool
	PressLog      []gomeboy.Button
	BlockRight    bool
	MaxAnchorX    byte
	DownFrames    int
	LockCountdown int
}

func (f *controllerFake) SaveCheckpoint() ([]byte, error) {
	snapshot := controllerFakeCheckpoint{
		Mem:           append([]byte(nil), f.mem[:]...),
		Frame:         f.frame,
		Pressed:       make(map[gomeboy.Button]bool, len(f.pressed)),
		PressLog:      append([]gomeboy.Button(nil), f.pressLog...),
		BlockRight:    f.blockRight,
		MaxAnchorX:    f.maxAnchorX,
		DownFrames:    f.downFrames,
		LockCountdown: f.lockCountdown,
	}
	for button, pressed := range f.pressed {
		snapshot.Pressed[button] = pressed
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snapshot); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *controllerFake) LoadCheckpoint(data []byte) error {
	var snapshot controllerFakeCheckpoint
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return err
	}
	if len(snapshot.Mem) != len(f.mem) {
		return fmt.Errorf("checkpoint memory length = %d, want %d", len(snapshot.Mem), len(f.mem))
	}
	copy(f.mem[:], snapshot.Mem)
	f.frame = snapshot.Frame
	f.pressed = make(map[gomeboy.Button]bool, len(snapshot.Pressed))
	for button, pressed := range snapshot.Pressed {
		f.pressed[button] = pressed
	}
	f.pressLog = append([]gomeboy.Button(nil), snapshot.PressLog...)
	f.blockRight = snapshot.BlockRight
	f.maxAnchorX = snapshot.MaxAnchorX
	f.downFrames = snapshot.DownFrames
	f.lockCountdown = snapshot.LockCountdown
	return nil
}
