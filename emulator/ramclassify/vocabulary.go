package ramclassify

import "strings"

const insufficientEvidence = "insufficient_evidence"

func vocabulary(profile string) []SemanticOption {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "tetris":
		return []SemanticOption{
			{ID: "piece_x", Description: "Horizontal position or X anchor of the currently controlled falling tetromino; left/right should move it in opposite directions.", Scalar: true},
			{ID: "piece_y", Description: "Vertical position or Y anchor of the currently controlled falling tetromino; down or gravity should advance it.", Scalar: true},
			{ID: "rotation", Description: "Current tetromino orientation or rotation state; rotate buttons should change it, often modulo a small number.", Scalar: true},
			{ID: "piece_type", Description: "Identity or type of the current or next tetromino, changing mainly when a new piece is spawned rather than on ordinary movement.", Scalar: true},
			{ID: "score", Description: "Player score or a byte belonging to score; should correlate with scoring events, not directly with ordinary left/right movement.", Scalar: true},
			{ID: "lines", Description: "Cleared-line or progress counter; should change on line-clear events rather than ordinary movement.", Scalar: true},
			{ID: "level", Description: "Game level or difficulty value; changes rarely at progression thresholds.", Scalar: true},
			{ID: "game_state", Description: "High-level gameplay mode, phase, ready/game-over state, or lock/wipe phase rather than a geometric coordinate.", Scalar: true},
			{ID: "timer_or_counter", Description: "Low-level timer, animation counter, cooldown, or generic counter affected indirectly by input timing.", Scalar: false},
			{ID: "input_state", Description: "Joypad/input latch, debouncing state, or control-processing byte that reflects a button rather than game-world semantics.", Scalar: false},
			{ID: "board_or_tile", Description: "One byte belonging to board, tile, sprite, or rendered-map data rather than a scalar gameplay variable.", Scalar: false},
			{ID: insufficientEvidence, Description: "The causal evidence is insufficient or ambiguous; do not assign a gameplay meaning yet.", Scalar: false},
		}
	case "boxxle":
		return []SemanticOption{
			{ID: "player_x", Description: "Horizontal player position or X anchor; left/right should move it in opposite directions.", Scalar: true},
			{ID: "player_y", Description: "Vertical player position or Y anchor; up/down should move it in opposite directions.", Scalar: true},
			{ID: "facing_or_direction", Description: "Player facing direction or orientation changed by directional controls.", Scalar: true},
			{ID: "room_or_level", Description: "Current room, stage, set, or level identifier; should change only during progression or loading.", Scalar: true},
			{ID: "move_or_progress_counter", Description: "Move count or puzzle-progress counter that changes after accepted moves or pushes.", Scalar: true},
			{ID: "game_state", Description: "High-level gameplay mode, transition, completion, or menu state.", Scalar: true},
			{ID: "timer_or_counter", Description: "Low-level timer, animation counter, cooldown, or generic counter affected indirectly by input timing.", Scalar: false},
			{ID: "input_state", Description: "Joypad/input latch, debouncing state, or control-processing byte rather than a world-state variable.", Scalar: false},
			{ID: "board_or_tile", Description: "One byte belonging to room tiles, collision map, sprite, or rendered-map data rather than a scalar variable.", Scalar: false},
			{ID: insufficientEvidence, Description: "The causal evidence is insufficient or ambiguous; do not assign a gameplay meaning yet.", Scalar: false},
		}
	default:
		return []SemanticOption{
			{ID: "controlled_x", Description: "Horizontal position or X coordinate of the player or currently controlled object; left/right should move it in opposite directions.", Scalar: true},
			{ID: "controlled_y", Description: "Vertical position or Y coordinate of the player or currently controlled object; up/down or down/gravity should change it.", Scalar: true},
			{ID: "orientation", Description: "Rotation, facing direction, orientation, or other small cyclic state directly changed by controls.", Scalar: true},
			{ID: "object_id", Description: "Identity/type of the controlled or upcoming game object, changing on spawn or object transitions.", Scalar: true},
			{ID: "score", Description: "Player score or a byte belonging to score, expected to correlate with scoring events rather than direct movement.", Scalar: true},
			{ID: "progress", Description: "Progress counter such as lines, items, moves, objectives, or stage progress.", Scalar: true},
			{ID: "level", Description: "Level, room, stage, or difficulty identifier.", Scalar: true},
			{ID: "game_state", Description: "High-level game mode, phase, ready/completion/game-over state, or transition state.", Scalar: true},
			{ID: "timer_or_counter", Description: "Low-level timer, animation counter, cooldown, or generic counter.", Scalar: false},
			{ID: "input_state", Description: "Input latch, debouncing state, or button-processing state rather than game-world semantics.", Scalar: false},
			{ID: "board_or_tile", Description: "One byte belonging to board, tile, collision-map, sprite, or rendered-map data rather than a scalar variable.", Scalar: false},
			{ID: insufficientEvidence, Description: "The causal evidence is insufficient or ambiguous; do not assign a gameplay meaning yet.", Scalar: false},
		}
	}
}

func optionMap(options []SemanticOption) map[string]SemanticOption {
	out := make(map[string]SemanticOption, len(options))
	for _, option := range options {
		out[option.ID] = option
	}
	return out
}
