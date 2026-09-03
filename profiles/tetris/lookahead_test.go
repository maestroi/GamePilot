package tetris

import "testing"

func TestUniqueLegalSimulationsCollapsesSymmetricORotations(t *testing.T) {
	obs := readyPlannerObservation(PieceO)
	unique, err := UniqueLegalSimulations(obs)
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != 9 {
		t.Fatalf("unique O placements = %d, want 9", len(unique))
	}
	for _, candidate := range unique {
		if candidate.Placement.Rotation != 0 {
			t.Fatalf("equivalent O placement kept rotation %d, want cheapest rotation 0", candidate.Placement.Rotation)
		}
	}
}

func TestChooseLookaheadPlacementUsesPreviewPiece(t *testing.T) {
	obs := readyPlannerObservation(PieceI)
	obs.NextPiece = Piece{Kind: PieceJ, Rotation: 0}
	obs.Board[BoardRows-1][0] = Filled
	obs.Board[BoardRows-1][1] = Filled

	onePly, err := ChooseHeuristicPlacement(obs)
	if err != nil {
		t.Fatal(err)
	}
	if onePly.Placement != (Placement{Rotation: 0, TargetColumn: 2}) {
		t.Fatalf("one-ply placement = %+v, want rotation=0 target_column=2", onePly.Placement)
	}

	twoPly, err := ChooseLookaheadPlacement(obs)
	if err != nil {
		t.Fatal(err)
	}
	if twoPly.First.Placement != (Placement{Rotation: 0, TargetColumn: 3}) {
		t.Fatalf("two-ply placement = %+v, want rotation=0 target_column=3", twoPly.First.Placement)
	}
	if twoPly.Reply == nil {
		t.Fatal("two-ply decision has no preview-piece reply")
	}
	if twoPly.Reply.Placement != (Placement{Rotation: 0, TargetColumn: 0}) {
		t.Fatalf("preview reply = %+v, want rotation=0 target_column=0", twoPly.Reply.Placement)
	}
}

func TestShortlistLookaheadSimulationsBoundsPromptCandidates(t *testing.T) {
	obs := readyPlannerObservation(PieceT)
	obs.NextPiece = Piece{Kind: PieceI, Rotation: 0}

	all, err := LookaheadSimulations(obs)
	if err != nil {
		t.Fatal(err)
	}
	shortlist, total, err := ShortlistLookaheadSimulations(obs, 5)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(all) {
		t.Fatalf("total = %d, want %d", total, len(all))
	}
	if len(shortlist) != 5 {
		t.Fatalf("shortlist length = %d, want 5", len(shortlist))
	}
	for i := range shortlist {
		if shortlist[i].First.Placement != all[i].First.Placement {
			t.Fatalf("shortlist[%d] = %+v, want best-first %+v", i, shortlist[i].First.Placement, all[i].First.Placement)
		}
	}
}

func TestLookaheadRequiresKnownPreviewPiece(t *testing.T) {
	obs := readyPlannerObservation(PieceT)
	obs.NextPiece = Piece{Kind: PieceUnknown}
	if _, err := ChooseLookaheadPlacement(obs); err == nil {
		t.Fatal("expected unknown-next-piece error")
	}
}
