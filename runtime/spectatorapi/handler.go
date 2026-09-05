// Package spectatorapi exposes the public, read-only GamePilot watch surface.
//
// It deliberately maps internal session snapshots into explicit public DTOs.
// Internal structs are never serialized directly, so adding a private field to
// sessions.Snapshot cannot accidentally expand the public contract.
package spectatorapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

const recentLimit = 8

// SessionReader is the narrow read capability required by the public surface.
// sessions.Manager satisfies this interface; mutation methods are intentionally
// absent from the contract.
type SessionReader interface {
	List() []sessions.Snapshot
	Snapshot(id string) (sessions.Snapshot, error)
	Frame(id string) (sessions.Frame, error)
}

// WatchResponse is the complete public JSON payload used by the spectator UI.
type WatchResponse struct {
	Session     *PublicSession  `json:"session,omitempty"`
	Recent      []PublicSession `json:"recent"`
	GeneratedAt time.Time       `json:"generated_at"`
}

// PublicSession is an allowlisted projection of sessions.Snapshot. Do not add
// fields here merely because they exist on the internal snapshot: every field
// is part of the internet-facing contract.
type PublicSession struct {
	ID               string           `json:"id"`
	Status           string           `json:"status"`
	Profile          string           `json:"profile,omitempty"`
	Planner          string           `json:"planner,omitempty"`
	ModelLabel       string           `json:"model_label,omitempty"`
	Moves            int              `json:"moves"`
	Frame            uint64           `json:"frame"`
	FrameSequence    uint64           `json:"frame_sequence,omitempty"`
	FrameAvailable   bool             `json:"frame_available"`
	PlannerActivity  string           `json:"planner_activity,omitempty"`
	PlannerLatencyMS int64            `json:"planner_latency_ms,omitempty"`
	Tetris           *PublicTetris    `json:"tetris,omitempty"`
	LatestPlacement  *PublicPlacement `json:"latest_placement,omitempty"`
	ElapsedSeconds   int64            `json:"elapsed_seconds"`
	CreatedAt        time.Time        `json:"created_at"`
	StartedAt        time.Time        `json:"started_at,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
	EndedAt          time.Time        `json:"ended_at,omitempty"`
}

type PublicTetris struct {
	Board        [18][10]uint8 `json:"board"`
	CurrentPiece PublicPiece   `json:"current_piece"`
	NextPiece    PublicPiece   `json:"next_piece"`
	Score        int           `json:"score"`
	Lines        int           `json:"lines"`
	Level        int           `json:"level"`
	Ready        bool          `json:"ready"`
	GameOver     bool          `json:"game_over"`
}

type PublicPiece struct {
	Kind     string `json:"kind"`
	Rotation int    `json:"rotation"`
}

type PublicPlacement struct {
	Rotation     int `json:"rotation"`
	TargetColumn int `json:"target_column"`
}

type tetrisObservation struct {
	Board        [18][10]uint8 `json:"board"`
	CurrentPiece piecePayload  `json:"current_piece"`
	NextPiece    piecePayload  `json:"next_piece"`
	Score        int           `json:"score"`
	Lines        int           `json:"lines"`
	Level        int           `json:"level"`
	Ready        bool          `json:"ready"`
	GameOver     bool          `json:"game_over"`
}

type piecePayload struct {
	Kind     string `json:"kind"`
	Rotation int    `json:"rotation"`
}

type placementPayload struct {
	Rotation     int `json:"rotation"`
	TargetColumn int `json:"target_column"`
}

type decisionPayload struct {
	Placement *placementPayload `json:"placement"`
	First     *struct {
		Placement *placementPayload `json:"placement"`
	} `json:"first"`
	Best *struct {
		Placement *placementPayload `json:"placement"`
	} `json:"best"`
}

type handler struct {
	reader SessionReader
	now    func() time.Time
}

// NewHandler builds the public API. A nil reader fails closed: the public route
// exists but returns a generic 503 without backend details.
func NewHandler(reader SessionReader) http.Handler {
	h := &handler{reader: reader, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/watch", h.getWatch)
	mux.HandleFunc("GET /v1/frame/{id}", h.getFrame)
	return securityHeaders(mux)
}

func (h *handler) getWatch(res http.ResponseWriter, req *http.Request) {
	if h.reader == nil {
		writeUnavailable(res)
		return
	}

	now := h.now().UTC()
	all := h.reader.List()
	public := make([]PublicSession, 0, len(all))
	byID := make(map[string]PublicSession, len(all))
	for _, snap := range all {
		item, ok := projectSession(snap, now)
		if !ok {
			continue
		}
		public = append(public, item)
		byID[item.ID] = item
	}

	sort.Slice(public, func(i, j int) bool {
		ai, aj := isActive(public[i].Status), isActive(public[j].Status)
		if ai != aj {
			return ai
		}
		if public[i].UpdatedAt.Equal(public[j].UpdatedAt) {
			return public[i].ID < public[j].ID
		}
		return public[i].UpdatedAt.After(public[j].UpdatedAt)
	})

	var selected *PublicSession
	requested := strings.TrimSpace(req.URL.Query().Get("session"))
	if requested != "" {
		item, ok := byID[requested]
		if !ok {
			writePublicError(res, http.StatusNotFound, "not_found")
			return
		}
		selected = &item
	} else if len(public) > 0 {
		item := public[0]
		selected = &item
	}

	recent := make([]PublicSession, 0, recentLimit)
	for _, item := range public {
		if !isTerminal(item.Status) {
			continue
		}
		recent = append(recent, item)
		if len(recent) == recentLimit {
			break
		}
	}

	writeJSON(res, http.StatusOK, WatchResponse{
		Session:     selected,
		Recent:      recent,
		GeneratedAt: now,
	})
}

func (h *handler) getFrame(res http.ResponseWriter, req *http.Request) {
	if h.reader == nil {
		writeUnavailable(res)
		return
	}
	id := req.PathValue("id")
	if !safeID(id) {
		writePublicError(res, http.StatusNotFound, "not_found")
		return
	}
	frame, err := h.reader.Frame(id)
	if err != nil {
		if errors.Is(err, sessions.ErrSessionNotFound) || errors.Is(err, sessions.ErrFrameUnavailable) {
			writePublicError(res, http.StatusNotFound, "not_found")
			return
		}
		writeUnavailable(res)
		return
	}
	// The current runtime publishes PNG frames. Refuse to reflect arbitrary
	// backend content types onto the public origin.
	if frame.ContentType != "image/png" || len(frame.Data) == 0 {
		writePublicError(res, http.StatusNotFound, "not_found")
		return
	}
	res.Header().Set("Content-Type", "image/png")
	res.Header().Set("X-GamePilot-Sequence", strconv.FormatUint(frame.Sequence, 10))
	res.WriteHeader(http.StatusOK)
	_, _ = res.Write(frame.Data)
}

func projectSession(snap sessions.Snapshot, now time.Time) (PublicSession, bool) {
	if !safeID(snap.ID) {
		return PublicSession{}, false
	}
	status := publicStatus(snap.Status)
	if status == "" {
		return PublicSession{}, false
	}

	profile := sanitizeLabel(snap.Profile, "profile")
	if profile == "" {
		profile = sanitizeLabel(snap.Config.Profile, "profile")
	}
	item := PublicSession{
		ID:               snap.ID,
		Status:           status,
		Profile:          profile,
		Planner:          sanitizeLabel(snap.Config.Planner, "planner"),
		ModelLabel:       sanitizeLabel(snap.Config.PlannerOptions.Model, "configured"),
		Moves:            maxInt(snap.Moves, 0),
		Frame:            snap.Frame,
		FrameSequence:    snap.FrameSequence,
		FrameAvailable:   snap.FrameAvailable,
		PlannerActivity:  publicPlannerActivity(snap.PlannerActivity),
		PlannerLatencyMS: maxInt64(snap.PlannerLatencyMS, 0),
		LatestPlacement:  publicPlacement(snap.Decision),
		CreatedAt:        snap.CreatedAt.UTC(),
		StartedAt:        utcOrZero(snap.StartedAt),
		UpdatedAt:        snap.UpdatedAt.UTC(),
		EndedAt:          utcOrZero(snap.EndedAt),
	}
	item.ElapsedSeconds = elapsedSeconds(snap, now)
	if profile == "tetris" {
		item.Tetris = publicTetris(snap.Observation)
	}
	return item, true
}

func publicTetris(raw json.RawMessage) *PublicTetris {
	if len(raw) == 0 {
		return nil
	}
	var input tetrisObservation
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil
	}
	return &PublicTetris{
		Board: input.Board,
		CurrentPiece: PublicPiece{
			Kind:     sanitizePiece(input.CurrentPiece.Kind),
			Rotation: clampRotation(input.CurrentPiece.Rotation),
		},
		NextPiece: PublicPiece{
			Kind:     sanitizePiece(input.NextPiece.Kind),
			Rotation: clampRotation(input.NextPiece.Rotation),
		},
		Score:    maxInt(input.Score, 0),
		Lines:    maxInt(input.Lines, 0),
		Level:    maxInt(input.Level, 0),
		Ready:    input.Ready,
		GameOver: input.GameOver,
	}
}

func publicPlacement(raw json.RawMessage) *PublicPlacement {
	if len(raw) == 0 {
		return nil
	}
	var input decisionPayload
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil
	}
	placement := input.Placement
	if placement == nil && input.First != nil {
		placement = input.First.Placement
	}
	if placement == nil && input.Best != nil {
		placement = input.Best.Placement
	}
	if placement == nil || placement.Rotation < 0 || placement.Rotation > 3 || placement.TargetColumn < 0 || placement.TargetColumn > 9 {
		return nil
	}
	return &PublicPlacement{Rotation: placement.Rotation, TargetColumn: placement.TargetColumn}
}

func elapsedSeconds(snap sessions.Snapshot, now time.Time) int64 {
	start := snap.StartedAt
	if start.IsZero() {
		start = snap.CreatedAt
	}
	if start.IsZero() {
		return 0
	}
	end := snap.EndedAt
	if end.IsZero() || end.Before(start) {
		end = now
	}
	if end.Before(start) {
		return 0
	}
	return int64(end.Sub(start) / time.Second)
}

func publicStatus(status sessions.Status) string {
	switch status {
	case sessions.StatusQueued, sessions.StatusStarting, sessions.StatusRunning, sessions.StatusStopping, sessions.StatusDone, sessions.StatusFailed:
		return string(status)
	default:
		return ""
	}
}

func publicPlannerActivity(value string) string {
	if value == "planning" {
		return value
	}
	return ""
}

func sanitizePiece(value string) string {
	switch value {
	case "I", "J", "L", "O", "S", "T", "Z", "unknown":
		return value
	default:
		return "unknown"
	}
}

func clampRotation(value int) int {
	if value < 0 || value > 3 {
		return 0
	}
	return value
}

func sanitizeLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 80 || strings.Contains(value, "://") || strings.ContainsAny(value, "/\\@:=") {
		return fallback
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.' || r == '+' {
			continue
		}
		return fallback
	}
	return value
}

func safeID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isActive(status string) bool {
	return status == "queued" || status == "starting" || status == "running" || status == "stopping"
}

func isTerminal(status string) bool { return status == "done" || status == "failed" }

func utcOrZero(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func maxInt(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Cache-Control", "no-store")
		res.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		res.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		res.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		res.Header().Set("Referrer-Policy", "no-referrer")
		res.Header().Set("X-Content-Type-Options", "nosniff")
		res.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(res, req)
	})
}

func writeUnavailable(res http.ResponseWriter) {
	writePublicError(res, http.StatusServiceUnavailable, "spectator_unavailable")
}

func writePublicError(res http.ResponseWriter, status int, code string) {
	writeJSON(res, status, struct {
		Error string `json:"error"`
	}{Error: code})
}

func writeJSON(res http.ResponseWriter, status int, value any) {
	res.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.WriteHeader(status)
	_ = json.NewEncoder(res).Encode(value)
}
