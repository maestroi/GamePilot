package ramclassify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maestroi/GamePilot/emulator/ramdiscover"
)

func testReport() ramdiscover.Report {
	return ramdiscover.Report{
		SchemaVersion: 1,
		ROMHash:       "abc123",
		Profile:       "tetris",
		StartFrame:    42,
		Config:        ramdiscover.Config{Trials: 3},
		Actions: []ramdiscover.ActionResult{
			{
				Action:     ramdiscover.Action{Name: "left"},
				Candidates: []ramdiscover.Candidate{{Address: "0xC202", AddressDecimal: 0xC202, Region: "wram", DominantDelta: -8, DivergenceRate: 1, DeltaConsistency: 1, Score: 1}},
			},
			{
				Action:     ramdiscover.Action{Name: "right"},
				Candidates: []ramdiscover.Candidate{{Address: "0xC202", AddressDecimal: 0xC202, Region: "wram", DominantDelta: 8, DivergenceRate: 1, DeltaConsistency: 1, Score: 1}},
			},
			{
				Action:     ramdiscover.Action{Name: "a"},
				Candidates: []ramdiscover.Candidate{{Address: "0xC203", AddressDecimal: 0xC203, Region: "wram", DominantDelta: -1, DivergenceRate: 1, DeltaConsistency: 1, Score: .95}},
			},
			{
				Action:     ramdiscover.Action{Name: "b"},
				Candidates: []ramdiscover.Candidate{{Address: "0xC203", AddressDecimal: 0xC203, Region: "wram", DominantDelta: 1, DivergenceRate: 1, DeltaConsistency: 1, Score: .95}},
			},
		},
		Relations: []ramdiscover.Relation{
			{Kind: "opposite_delta", Actions: [2]string{"left", "right"}, Address: "0xC202", Region: "wram", FirstDelta: -8, SecondDelta: 8, Score: 1},
			{Kind: "opposite_delta", Actions: [2]string{"a", "b"}, Address: "0xC203", Region: "wram", FirstDelta: -1, SecondDelta: 1, Score: .95},
		},
	}
}

func TestCollectEvidencePreservesCausalRelations(t *testing.T) {
	evidence := collectEvidence(testReport(), 10)
	if len(evidence) != 2 {
		t.Fatalf("len(evidence) = %d, want 2", len(evidence))
	}
	if evidence[0].Address != "0xC202" || evidence[0].Relations[0].FirstDelta != -8 || evidence[0].Relations[0].SecondDelta != 8 {
		t.Fatalf("unexpected first evidence: %#v", evidence[0])
	}
	if evidence[1].Address != "0xC203" || len(evidence[1].Actions) != 2 {
		t.Fatalf("unexpected second evidence: %#v", evidence[1])
	}
}

func TestClassifyBuildsTetrisScalarSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Fatalf("request = %s %s, want POST /v1/systemone", r.Method, r.URL.Path)
		}
		var request systemOneRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "openjev-latest" {
			t.Fatalf("model = %q", request.Model)
		}
		state, ok := request.State.(map[string]any)
		if !ok || state["profile"] != "tetris" {
			t.Fatalf("unexpected state: %#v", request.State)
		}
		if len(request.Questions) != 2 {
			t.Fatalf("questions = %d, want 2", len(request.Questions))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"openjev-0.1",
			"answers":{
				"address_000":{"type":"choice","choice":"piece_x","probabilities":{"piece_x":0.94,"rotation":0.02,"insufficient_evidence":0.04},"confidence":0.72},
				"address_001":{"type":"choice","choice":"rotation","probabilities":{"rotation":0.91,"piece_x":0.03,"insufficient_evidence":0.06},"confidence":0.65}
			},
			"usage":{"input_tokens":100,"output_tokens":0}
		}`))
	}))
	defer server.Close()

	client := NewOpenJevClient(server.URL+"/v1", "")
	cfg := DefaultConfig()
	cfg.BatchSize = 8
	schema, err := Classify(context.Background(), client, testReport(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pieceX, ok := schema.State["piece_x"]
	if !ok || pieceX.Address != "0xC202" {
		t.Fatalf("piece_x = %#v, ok=%v", pieceX, ok)
	}
	rotation, ok := schema.State["rotation"]
	if !ok || rotation.Address != "0xC203" {
		t.Fatalf("rotation = %#v, ok=%v", rotation, ok)
	}
	if !schema.Classifications[0].Accepted || !schema.Classifications[1].Accepted {
		t.Fatalf("classifications not accepted: %#v", schema.Classifications)
	}
}

func TestClassifyRejectsWeakOrAbstainedAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"openjev-0.1",
			"answers":{
				"address_000":{"type":"choice","choice":"piece_x","probabilities":{"piece_x":0.51,"insufficient_evidence":0.49},"confidence":0.01},
				"address_001":{"type":"choice","choice":"insufficient_evidence","probabilities":{"rotation":0.2,"insufficient_evidence":0.8},"confidence":0.45}
			},
			"usage":{"input_tokens":80,"output_tokens":0}
		}`))
	}))
	defer server.Close()

	schema, err := Classify(context.Background(), NewOpenJevClient(server.URL+"/v1", ""), testReport(), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.State) != 0 {
		t.Fatalf("weak answers leaked into schema: %#v", schema.State)
	}
	if schema.Classifications[0].RejectionReason != "probability_below_threshold" {
		t.Fatalf("first rejection = %q", schema.Classifications[0].RejectionReason)
	}
	if schema.Classifications[1].RejectionReason != "insufficient_evidence" {
		t.Fatalf("second rejection = %q", schema.Classifications[1].RejectionReason)
	}
}

func TestClassifyKeepsBestDuplicateScalar(t *testing.T) {
	report := testReport()
	// Make the rotation byte look less convincing than C202 while the mocked model
	// intentionally labels both addresses piece_x.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"openjev-0.1",
			"answers":{
				"address_000":{"type":"choice","choice":"piece_x","probabilities":{"piece_x":0.9,"insufficient_evidence":0.1},"confidence":0.7},
				"address_001":{"type":"choice","choice":"piece_x","probabilities":{"piece_x":0.8,"insufficient_evidence":0.2},"confidence":0.4}
			},
			"usage":{"input_tokens":80,"output_tokens":0}
		}`))
	}))
	defer server.Close()

	schema, err := Classify(context.Background(), NewOpenJevClient(server.URL+"/v1", ""), report, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.State["piece_x"].Address; got != "0xC202" {
		t.Fatalf("piece_x winner = %s, want 0xC202", got)
	}
	if schema.Classifications[1].RejectionReason != "lower_ranked_duplicate" {
		t.Fatalf("duplicate rejection = %q", schema.Classifications[1].RejectionReason)
	}
}
