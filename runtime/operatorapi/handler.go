// Package operatorapi exposes the private authenticated control plane for
// GamePilot live sessions. It is intentionally separate from the public/read
// handlers so mutation routes cannot be inherited by the spectator surface.
package operatorapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maestroi/GamePilot/runtime/sessions"
)

const (
	defaultMaxBodyBytes       int64 = 16 << 10
	defaultMutationLimit            = 60
	defaultMutationWindow           = time.Minute
	defaultMutationTimeout          = 5 * time.Second
	defaultMaxMoveLimit             = 100000
	defaultMaxShortlistSize         = 100
)

// ROMOption maps a browser-visible alias to one server-local ROM path. Path is
// never serialized by this package.
type ROMOption struct {
	Alias   string `json:"alias"`
	Label   string `json:"label,omitempty"`
	Profile string `json:"profile"`
	Path    string `json:"-"`
}

// PlannerOption describes one safe planner choice for a profile. Model-backed
// planners require a separately allowlisted model alias.
type PlannerOption struct {
	ID            string `json:"id"`
	RequiresModel bool   `json:"requires_model,omitempty"`
}

// ProfileOption describes one profile and the planners the operator is allowed
// to launch for it.
type ProfileOption struct {
	ID       string          `json:"id"`
	Planners []PlannerOption `json:"planners"`
}

// ModelOption is a safe server-configured alias. Provider URL, credentials, and
// other secret configuration remain outside the HTTP contract.
type ModelOption struct {
	Alias string `json:"alias"`
	Label string `json:"label,omitempty"`
}

// Options configures one private operator handler.
type Options struct {
	Manager       *sessions.Manager
	OperatorToken string
	ROMs          []ROMOption
	Profiles      []ProfileOption
	Models        []ModelOption

	MaxMoveLimit     int
	MaxShortlistSize int
	MaxBodyBytes     int64
	MutationLimit    int
	MutationWindow   time.Duration
	MutationTimeout  time.Duration
}

type safeConfig struct {
	ROMs     []ROMOption     `json:"roms"`
	Profiles []ProfileOption `json:"profiles"`
	Models   []ModelOption   `json:"models"`
	Pacing   []sessions.PacingMode `json:"pacing"`
	Limits   safeLimits      `json:"limits"`
}

type safeLimits struct {
	MaxMoveLimit     int `json:"max_move_limit"`
	MaxShortlistSize int `json:"max_shortlist_size"`
	MaxBodyBytes     int64 `json:"max_body_bytes"`
}

type createSessionRequest struct {
	ROMAlias       string              `json:"rom_alias"`
	Profile        string              `json:"profile"`
	Planner        string              `json:"planner"`
	MoveLimit      int                 `json:"move_limit"`
	RecordReplay   bool                `json:"record_replay"`
	Pacing         sessions.PacingMode `json:"pacing,omitempty"`
	Model           string              `json:"model,omitempty"`
	ShortlistSize   int                 `json:"shortlist_size,omitempty"`
}

type sessionListResponse struct {
	Sessions []sessions.Snapshot `json:"sessions"`
}

type apiErrorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type handler struct {
	manager *sessions.Manager
	token   string
	roms    map[string]ROMOption
	profiles map[string]ProfileOption
	models  map[string]ModelOption
	config  safeConfig

	maxMoveLimit     int
	maxShortlistSize int
	maxBodyBytes     int64
	mutationTimeout  time.Duration
	limiter          *fixedWindowLimiter
}

// NewHandler builds the private operator API. When OperatorToken is empty, it
// returns a handler with no operator routes mounted (all requests receive 404).
// This is deliberate: missing auth configuration never downgrades mutations to
// anonymous access.
func NewHandler(opts Options) (http.Handler, error) {
	return newHandler(opts, time.Now)
}

func newHandler(opts Options, now func() time.Time) (http.Handler, error) {
	if opts.OperatorToken == "" {
		return http.NotFoundHandler(), nil
	}
	if opts.Manager == nil {
		return nil, fmt.Errorf("operatorapi: session manager is required")
	}
	if now == nil {
		now = time.Now
	}
	applyDefaults(&opts)

	h := &handler{
		manager:            opts.Manager,
		token:              opts.OperatorToken,
		maxMoveLimit:       opts.MaxMoveLimit,
		maxShortlistSize:   opts.MaxShortlistSize,
		maxBodyBytes:       opts.MaxBodyBytes,
		mutationTimeout:    opts.MutationTimeout,
		limiter:            newFixedWindowLimiter(opts.MutationLimit, opts.MutationWindow, now),
	}
	if err := h.buildCatalog(opts); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/config", h.requireAuth(h.getConfig))
	mux.HandleFunc("GET /v1/sessions", h.requireAuth(h.listSessions))
	mux.HandleFunc("GET /v1/sessions/{id}", h.requireAuth(h.getSession))
	mux.HandleFunc("GET /v1/sessions/{id}/frame", h.requireAuth(h.getFrame))
	mux.HandleFunc("POST /v1/sessions", h.requireAuth(h.mutation(h.createSession)))
	mux.HandleFunc("POST /v1/sessions/{id}/stop", h.requireAuth(h.mutation(h.stopSession)))
	mux.HandleFunc("DELETE /v1/sessions/{id}", h.requireAuth(h.mutation(h.deleteSession)))
	return mux, nil
}

func applyDefaults(opts *Options) {
	if opts.MaxMoveLimit <= 0 {
		opts.MaxMoveLimit = defaultMaxMoveLimit
	}
	if opts.MaxShortlistSize <= 0 {
		opts.MaxShortlistSize = defaultMaxShortlistSize
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = defaultMaxBodyBytes
	}
	if opts.MutationLimit <= 0 {
		opts.MutationLimit = defaultMutationLimit
	}
	if opts.MutationWindow <= 0 {
		opts.MutationWindow = defaultMutationWindow
	}
	if opts.MutationTimeout <= 0 {
		opts.MutationTimeout = defaultMutationTimeout
	}
}

func (h *handler) buildCatalog(opts Options) error {
	h.roms = make(map[string]ROMOption, len(opts.ROMs))
	roms := append([]ROMOption(nil), opts.ROMs...)
	for _, rom := range roms {
		if rom.Alias == "" || rom.Profile == "" || rom.Path == "" {
			return fmt.Errorf("operatorapi: ROM alias, profile, and path are required")
		}
		if _, exists := h.roms[rom.Alias]; exists {
			return fmt.Errorf("operatorapi: duplicate ROM alias %q", rom.Alias)
		}
		h.roms[rom.Alias] = rom
	}
	sort.Slice(roms, func(i, j int) bool { return roms[i].Alias < roms[j].Alias })

	h.profiles = make(map[string]ProfileOption, len(opts.Profiles))
	profiles := append([]ProfileOption(nil), opts.Profiles...)
	for i := range profiles {
		profile := profiles[i]
		if profile.ID == "" {
			return fmt.Errorf("operatorapi: profile id is required")
		}
		if _, exists := h.profiles[profile.ID]; exists {
			return fmt.Errorf("operatorapi: duplicate profile %q", profile.ID)
		}
		seenPlanner := make(map[string]struct{}, len(profile.Planners))
		for _, planner := range profile.Planners {
			if planner.ID == "" {
				return fmt.Errorf("operatorapi: empty planner id for profile %q", profile.ID)
			}
			if _, exists := seenPlanner[planner.ID]; exists {
				return fmt.Errorf("operatorapi: duplicate planner %q for profile %q", planner.ID, profile.ID)
			}
			seenPlanner[planner.ID] = struct{}{}
		}
		sort.Slice(profiles[i].Planners, func(a, b int) bool {
			return profiles[i].Planners[a].ID < profiles[i].Planners[b].ID
		})
		h.profiles[profile.ID] = profiles[i]
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })

	for _, rom := range roms {
		if _, ok := h.profiles[rom.Profile]; !ok {
			return fmt.Errorf("operatorapi: ROM alias %q references unknown profile %q", rom.Alias, rom.Profile)
		}
	}

	h.models = make(map[string]ModelOption, len(opts.Models))
	models := append([]ModelOption(nil), opts.Models...)
	for _, model := range models {
		if model.Alias == "" {
			return fmt.Errorf("operatorapi: model alias is required")
		}
		if _, exists := h.models[model.Alias]; exists {
			return fmt.Errorf("operatorapi: duplicate model alias %q", model.Alias)
		}
		h.models[model.Alias] = model
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Alias < models[j].Alias })

	h.config = safeConfig{
		ROMs:     roms,
		Profiles: profiles,
		Models:   models,
		Pacing:   []sessions.PacingMode{sessions.PacingFast, sessions.PacingRealtime},
		Limits: safeLimits{
			MaxMoveLimit:     h.maxMoveLimit,
			MaxShortlistSize: h.maxShortlistSize,
			MaxBodyBytes:     h.maxBodyBytes,
		},
	}
	return nil
}

func (h *handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		const prefix = "Bearer "
		auth := req.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) {
			unauthorized(res)
			return
		}
		presented := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		if subtle.ConstantTimeCompare([]byte(presented), []byte(h.token)) != 1 {
			unauthorized(res)
			return
		}
		next(res, req)
	}
}

func unauthorized(res http.ResponseWriter) {
	res.Header().Set("WWW-Authenticate", `Bearer realm="gamepilot-operator"`)
	writeError(res, http.StatusUnauthorized, "unauthorized", "operator authentication required")
}

func (h *handler) mutation(next http.HandlerFunc) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		allowed, retryAfter := h.limiter.Allow()
		if !allowed {
			if retryAfter > 0 {
				res.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second)/time.Second)))
			}
			writeError(res, http.StatusTooManyRequests, "rate_limited", "operator mutation rate limit exceeded")
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), h.mutationTimeout)
		defer cancel()
		next(res, req.WithContext(ctx))
	}
}

func (h *handler) getConfig(res http.ResponseWriter, _ *http.Request) {
	writeJSON(res, http.StatusOK, h.config)
}

func (h *handler) listSessions(res http.ResponseWriter, _ *http.Request) {
	writeJSON(res, http.StatusOK, sessionListResponse{Sessions: h.manager.List()})
}

func (h *handler) getSession(res http.ResponseWriter, req *http.Request) {
	snap, err := h.manager.Snapshot(req.PathValue("id"))
	if err != nil {
		writeManagerReadError(res, err)
		return
	}
	writeJSON(res, http.StatusOK, snap)
}

func (h *handler) getFrame(res http.ResponseWriter, req *http.Request) {
	frame, err := h.manager.Frame(req.PathValue("id"))
	if err != nil {
		if errors.Is(err, sessions.ErrSessionNotFound) || errors.Is(err, sessions.ErrFrameUnavailable) {
			writeError(res, http.StatusNotFound, "not_found", "frame not found")
			return
		}
		writeError(res, http.StatusInternalServerError, "read_failed", "frame read failed")
		return
	}
	res.Header().Set("Content-Type", frame.ContentType)
	res.Header().Set("Cache-Control", "no-store")
	res.Header().Set("X-GamePilot-Sequence", strconv.FormatUint(frame.Sequence, 10))
	res.Header().Set("X-GamePilot-Emulator-Frame", strconv.FormatUint(frame.EmulatorFrame, 10))
	res.WriteHeader(http.StatusOK)
	_, _ = res.Write(frame.Data)
}

func (h *handler) createSession(res http.ResponseWriter, req *http.Request) {
	var input createSessionRequest
	if err := decodeJSON(res, req, h.maxBodyBytes, &input); err != nil {
		writeError(res, http.StatusBadRequest, "invalid_request", "request body must be one JSON object using supported fields")
		return
	}
	launch, validationCode, validationMessage := h.resolveLaunch(input)
	if validationCode != "" {
		writeError(res, http.StatusBadRequest, validationCode, validationMessage)
		return
	}

	id, err := h.manager.Start(launch)
	if err != nil {
		// Catalog validation should make user-controlled configuration errors
		// impossible here. Keep factory/runtime internals out of the response.
		writeError(res, http.StatusInternalServerError, "start_failed", "session could not be started")
		return
	}
	snap, err := h.manager.Snapshot(id)
	if err != nil {
		writeError(res, http.StatusInternalServerError, "start_failed", "session started but could not be read")
		return
	}
	res.Header().Set("Location", "/v1/sessions/"+id)
	writeJSON(res, http.StatusCreated, snap)
}

func (h *handler) resolveLaunch(input createSessionRequest) (sessions.LaunchConfig, string, string) {
	rom, ok := h.roms[input.ROMAlias]
	if !ok {
		return sessions.LaunchConfig{}, "invalid_rom_alias", "ROM alias is not configured"
	}
	if input.Profile == "" || input.Profile != rom.Profile {
		return sessions.LaunchConfig{}, "invalid_profile", "profile does not match the selected ROM"
	}
	profile, ok := h.profiles[input.Profile]
	if !ok {
		return sessions.LaunchConfig{}, "invalid_profile", "profile is not configured"
	}
	var planner PlannerOption
	plannerOK := false
	for _, candidate := range profile.Planners {
		if candidate.ID == input.Planner {
			planner = candidate
			plannerOK = true
			break
		}
	}
	if !plannerOK {
		return sessions.LaunchConfig{}, "invalid_planner", "planner is not enabled for this profile"
	}
	if input.MoveLimit < 0 || input.MoveLimit > h.maxMoveLimit {
		return sessions.LaunchConfig{}, "invalid_move_limit", "move_limit is outside the configured range"
	}
	if input.ShortlistSize < 0 || input.ShortlistSize > h.maxShortlistSize {
		return sessions.LaunchConfig{}, "invalid_shortlist_size", "shortlist_size is outside the configured range"
	}
	if planner.RequiresModel {
		if input.Model == "" {
			return sessions.LaunchConfig{}, "invalid_model", "this planner requires a configured model alias"
		}
		if _, ok := h.models[input.Model]; !ok {
			return sessions.LaunchConfig{}, "invalid_model", "model alias is not configured"
		}
	} else {
		if input.Model != "" || input.ShortlistSize != 0 {
			return sessions.LaunchConfig{}, "invalid_planner_options", "model and shortlist_size are only valid for model-backed planners"
		}
	}

	pacing := input.Pacing
	if pacing == "" {
		// The operator surface exists for live observation, so its safe default is
		// realtime even though the lower-level Manager defaults to fast.
		pacing = sessions.PacingRealtime
	}
	if pacing != sessions.PacingFast && pacing != sessions.PacingRealtime {
		return sessions.LaunchConfig{}, "invalid_pacing", "pacing must be fast or realtime"
	}

	return sessions.LaunchConfig{
		ROMPath:      rom.Path,
		Profile:      input.Profile,
		Planner:      input.Planner,
		MoveLimit:    input.MoveLimit,
		RecordReplay: input.RecordReplay,
		Pacing:       pacing,
		PlannerOptions: sessions.PlannerOptions{
			Model:         input.Model,
			ShortlistSize: input.ShortlistSize,
		},
	}, "", ""
}

func (h *handler) stopSession(res http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := h.manager.Stop(id); err != nil {
		if errors.Is(err, sessions.ErrSessionNotFound) {
			writeError(res, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeError(res, http.StatusInternalServerError, "stop_failed", "session could not be stopped")
		return
	}
	snap, err := h.manager.Snapshot(id)
	if err != nil {
		writeManagerReadError(res, err)
		return
	}
	writeJSON(res, http.StatusOK, snap)
}

func (h *handler) deleteSession(res http.ResponseWriter, req *http.Request) {
	err := h.manager.Delete(req.PathValue("id"))
	switch {
	case err == nil:
		res.Header().Set("Cache-Control", "no-store")
		res.WriteHeader(http.StatusNoContent)
	case errors.Is(err, sessions.ErrSessionNotFound):
		writeError(res, http.StatusNotFound, "session_not_found", "session not found")
	case errors.Is(err, sessions.ErrSessionNotTerminal):
		writeError(res, http.StatusConflict, "session_not_terminal", "stop the session and wait for terminal state before deleting it")
	default:
		writeError(res, http.StatusInternalServerError, "delete_failed", "session could not be deleted")
	}
}

func writeManagerReadError(res http.ResponseWriter, err error) {
	if errors.Is(err, sessions.ErrSessionNotFound) {
		writeError(res, http.StatusNotFound, "session_not_found", "session not found")
		return
	}
	writeError(res, http.StatusInternalServerError, "read_failed", "session read failed")
}

func decodeJSON(res http.ResponseWriter, req *http.Request, limit int64, dst any) error {
	req.Body = http.MaxBytesReader(res, req.Body, limit)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(res http.ResponseWriter, status int, value any) {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("Cache-Control", "no-store")
	res.WriteHeader(status)
	_ = json.NewEncoder(res).Encode(value)
}

func writeError(res http.ResponseWriter, status int, code, message string) {
	writeJSON(res, status, apiErrorBody{Error: apiError{Code: code, Message: message}})
}

type fixedWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	start  time.Time
	count  int
}

func newFixedWindowLimiter(limit int, window time.Duration, now func() time.Time) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, window: window, now: now}
}

func (l *fixedWindowLimiter) Allow() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.start.IsZero() || !now.Before(l.start.Add(l.window)) {
		l.start = now
		l.count = 0
	}
	if l.count >= l.limit {
		return false, l.start.Add(l.window).Sub(now)
	}
	l.count++
	return true, 0
}
