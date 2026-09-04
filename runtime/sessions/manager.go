// Package sessions owns long-lived GamePilot run lifecycle above emulator/profile code.
//
// A managed session has exactly one goroutine driving its Runner. Readers only
// receive copied snapshots, frames, and replay bytes; they never receive an
// emulator pointer or another mutable object owned by the runner goroutine.
package sessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued   Status = "queued"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
)

var (
	ErrSessionNotFound   = errors.New("sessions: session not found")
	ErrReplayUnavailable = errors.New("sessions: replay unavailable")
	ErrFrameUnavailable  = errors.New("sessions: frame unavailable")
)

// PlannerOptions contains safe planner-level launch knobs. Model is intended to
// be an operator-configured alias/label; credentials and raw provider endpoints
// belong outside LaunchConfig.
type PlannerOptions struct {
	Model         string `json:"model,omitempty"`
	ShortlistSize int    `json:"shortlist_size,omitempty"`
}

// LaunchConfig is immutable after Manager.Start returns. ROMPath is deliberately
// excluded from JSON so serializing a Snapshot cannot accidentally disclose a
// server filesystem path. A future private API will resolve public ROM aliases
// to this internal path before starting a session.
type LaunchConfig struct {
	ROMPath        string         `json:"-"`
	Profile        string         `json:"profile"`
	Planner        string         `json:"planner"`
	MoveLimit      int            `json:"move_limit,omitempty"` // 0 means run until terminal/cancelled.
	RecordReplay   bool           `json:"record_replay"`
	Pacing         PacingMode     `json:"pacing,omitempty"`
	PlannerOptions PlannerOptions `json:"planner_options,omitempty"`
}

// Frame is one immutable encoded framebuffer publication. Data is kept outside
// Snapshot JSON so browser/API layers can serve it as an image response rather
// than base64-expanding every state poll.
type Frame struct {
	Sequence      uint64    `json:"sequence"`
	EmulatorFrame uint64    `json:"emulator_frame"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	ContentType   string    `json:"content_type"`
	CapturedAt    time.Time `json:"captured_at"`
	Data          []byte    `json:"-"`
}

// Snapshot is the immutable read model exposed by Manager. Observation and
// Decision are profile/planner-specific JSON payloads so this lifecycle layer
// does not need to know Tetris board geometry or future games' action schemas.
type Snapshot struct {
	ID               string          `json:"id"`
	Status           Status          `json:"status"`
	Config           LaunchConfig    `json:"config"`
	Profile          string          `json:"profile,omitempty"`
	ROMSHA256        string          `json:"rom_sha256,omitempty"`
	CartridgeTitle   string          `json:"cartridge_title,omitempty"`
	Frame            uint64          `json:"frame"`
	Moves            int             `json:"moves"`
	Observation      json.RawMessage `json:"observation,omitempty"`
	Decision         json.RawMessage `json:"decision,omitempty"`
	PlannerActivity  string          `json:"planner_activity,omitempty"`
	PlannerStartedAt time.Time       `json:"planner_started_at,omitempty"`
	PlannerLatencyMS int64           `json:"planner_latency_ms,omitempty"`
	Sequence         uint64          `json:"sequence"`
	FrameSequence    uint64          `json:"frame_sequence,omitempty"`
	FrameAvailable   bool            `json:"frame_available"`
	Reason           string          `json:"reason,omitempty"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	StartedAt        time.Time       `json:"started_at,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
	EndedAt          time.Time       `json:"ended_at,omitempty"`
}

// Update is published by the single runner goroutine at meaningful boundaries.
// Manager copies JSON/image fields before making them visible to readers.
type Update struct {
	Profile          string
	ROMSHA256        string
	CartridgeTitle   string
	Frame            uint64
	Moves            int
	Observation      json.RawMessage
	Decision         json.RawMessage
	PlannerActivity  string
	PlannerStartedAt time.Time
	PlannerLatencyMS int64
	Image            *Frame
}

// Result is the terminal runner output. Replay is an encoded profile replay,
// kept as bytes so the generic lifecycle manager does not depend on one game's
// replay type.
type Result struct {
	Reason string
	Replay []byte
}

// Runner owns all emulator/profile calls for one session. Run is invoked once
// from exactly one goroutine.
type Runner interface {
	Run(ctx context.Context, publish func(Update)) (Result, error)
}

// RunnerFactory must be side-effect free with respect to emulator lifetime:
// New validates/configures a Runner, while the Runner opens/closes its emulator
// inside Run. That lets Manager reject invalid launch configs synchronously.
type RunnerFactory interface {
	New(config LaunchConfig) (Runner, error)
}

type RunnerFactoryFunc func(config LaunchConfig) (Runner, error)

func (f RunnerFactoryFunc) New(config LaunchConfig) (Runner, error) { return f(config) }

type Manager struct {
	mu       sync.RWMutex
	factory  RunnerFactory
	now      func() time.Time
	newID    func() (string, error)
	sessions map[string]*managedSession
}

type managedSession struct {
	mu     sync.RWMutex
	snap   Snapshot
	cancel context.CancelFunc
	done   chan struct{}
	replay []byte
	image  *Frame
}

func NewManager(factory RunnerFactory) *Manager {
	return newManager(factory, time.Now, randomID)
}

func newManager(factory RunnerFactory, now func() time.Time, newID func() (string, error)) *Manager {
	return &Manager{
		factory:  factory,
		now:      now,
		newID:    newID,
		sessions: make(map[string]*managedSession),
	}
}

func (m *Manager) Start(config LaunchConfig) (string, error) {
	if m == nil || m.factory == nil {
		return "", fmt.Errorf("sessions: runner factory is required")
	}
	pacing, err := normalizePacing(config.Pacing)
	if err != nil {
		return "", err
	}
	config.Pacing = pacing
	if err := validateLaunchConfig(config); err != nil {
		return "", err
	}

	runner, err := m.factory.New(config)
	if err != nil {
		return "", fmt.Errorf("sessions: configure runner: %w", err)
	}
	if runner == nil {
		return "", fmt.Errorf("sessions: runner factory returned nil runner")
	}

	id, err := m.newID()
	if err != nil {
		return "", fmt.Errorf("sessions: allocate session id: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("sessions: allocated empty session id")
	}

	ctx, cancel := context.WithCancel(context.Background())
	created := m.now()
	s := &managedSession{
		snap: Snapshot{
			ID:        id,
			Status:    StatusQueued,
			Config:    config,
			Profile:   config.Profile,
			Sequence:  1,
			CreatedAt: created,
			UpdatedAt: created,
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	m.mu.Lock()
	if _, exists := m.sessions[id]; exists {
		m.mu.Unlock()
		cancel()
		return "", fmt.Errorf("sessions: duplicate session id %q", id)
	}
	m.sessions[id] = s
	m.mu.Unlock()

	go m.run(ctx, s, runner)
	return id, nil
}

func validateLaunchConfig(config LaunchConfig) error {
	if config.ROMPath == "" {
		return fmt.Errorf("sessions: ROM path is required")
	}
	if config.Profile == "" {
		return fmt.Errorf("sessions: profile is required")
	}
	if config.Planner == "" {
		return fmt.Errorf("sessions: planner is required")
	}
	if config.MoveLimit < 0 {
		return fmt.Errorf("sessions: move limit cannot be negative")
	}
	if config.PlannerOptions.ShortlistSize < 0 {
		return fmt.Errorf("sessions: planner shortlist size cannot be negative")
	}
	return nil
}

func (m *Manager) run(ctx context.Context, s *managedSession, runner Runner) {
	now := m.now()
	s.mu.Lock()
	if s.snap.Status == StatusQueued {
		s.snap.Status = StatusStarting
		s.snap.StartedAt = now
	}
	if s.snap.StartedAt.IsZero() {
		s.snap.StartedAt = now
	}
	if s.snap.Status == StatusStarting {
		s.snap.Status = StatusRunning
	}
	s.snap.Sequence++
	s.snap.UpdatedAt = now
	s.mu.Unlock()

	result, err := runner.Run(ctx, func(update Update) {
		s.applyUpdate(update, m.now())
	})

	now = m.now()
	s.mu.Lock()
	if len(result.Replay) > 0 {
		s.replay = append([]byte(nil), result.Replay...)
	}
	s.snap.EndedAt = now
	s.snap.UpdatedAt = now
	s.snap.Sequence++
	s.snap.PlannerActivity = ""
	s.snap.PlannerStartedAt = time.Time{}
	if err == nil {
		s.snap.Status = StatusDone
		s.snap.Reason = result.Reason
		if s.snap.Reason == "" {
			s.snap.Reason = "completed"
		}
	} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		s.snap.Status = StatusDone
		s.snap.Reason = result.Reason
		if s.snap.Reason == "" {
			s.snap.Reason = "stopped"
		}
	} else {
		s.snap.Status = StatusFailed
		s.snap.Reason = result.Reason
		s.snap.Error = err.Error()
	}
	s.mu.Unlock()
	close(s.done)
}

func (s *managedSession) applyUpdate(update Update, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if terminal(s.snap.Status) {
		return
	}
	if update.Profile != "" {
		s.snap.Profile = update.Profile
	}
	if update.ROMSHA256 != "" {
		s.snap.ROMSHA256 = update.ROMSHA256
	}
	if update.CartridgeTitle != "" {
		s.snap.CartridgeTitle = update.CartridgeTitle
	}
	s.snap.Frame = update.Frame
	s.snap.Moves = update.Moves
	s.snap.Observation = cloneJSON(update.Observation)
	s.snap.Decision = cloneJSON(update.Decision)
	s.snap.PlannerActivity = update.PlannerActivity
	s.snap.PlannerStartedAt = update.PlannerStartedAt
	s.snap.PlannerLatencyMS = update.PlannerLatencyMS
	s.snap.Sequence++
	s.snap.UpdatedAt = now
	if update.Image != nil {
		image := cloneFrame(*update.Image)
		image.Sequence = s.snap.Sequence
		if image.CapturedAt.IsZero() {
			image.CapturedAt = now
		}
		s.image = &image
		s.snap.FrameSequence = image.Sequence
		s.snap.FrameAvailable = true
	}
}

func (m *Manager) Stop(id string) error {
	s, err := m.lookup(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if terminal(s.snap.Status) {
		s.mu.Unlock()
		return nil
	}
	s.snap.Status = StatusStopping
	s.snap.Sequence++
	s.snap.UpdatedAt = m.now()
	cancel := s.cancel
	s.mu.Unlock()
	cancel()
	return nil
}

func (m *Manager) Snapshot(id string) (Snapshot, error) {
	s, err := m.lookup(id)
	if err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(), nil
}

func (m *Manager) List() []Snapshot {
	m.mu.RLock()
	sessions := make([]*managedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	out := make([]Snapshot, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Frame returns a defensive copy of the most recently published encoded image.
// Only one image is retained per session, so slow readers cannot backpressure or
// cause unbounded buffering in the emulator owner goroutine.
func (m *Manager) Frame(id string) (Frame, error) {
	s, err := m.lookup(id)
	if err != nil {
		return Frame{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.image == nil || len(s.image.Data) == 0 {
		return Frame{}, ErrFrameUnavailable
	}
	return cloneFrame(*s.image), nil
}

func (m *Manager) Replay(id string) ([]byte, error) {
	s, err := m.lookup(id)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.replay) == 0 {
		return nil, ErrReplayUnavailable
	}
	return append([]byte(nil), s.replay...), nil
}

func (m *Manager) Wait(ctx context.Context, id string) (Snapshot, error) {
	s, err := m.lookup(id)
	if err != nil {
		return Snapshot{}, err
	}
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-s.done:
		return s.snapshot(), nil
	}
}

// Close requests cancellation for every non-terminal session and waits for all
// owned runner goroutines to finish or for ctx to expire.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		_ = m.Stop(id)
	}
	for _, id := range ids {
		if _, err := m.Wait(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) lookup(id string) (*managedSession, error) {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return s, nil
}

func (s *managedSession) snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := s.snap
	copy.Observation = cloneJSON(s.snap.Observation)
	copy.Decision = cloneJSON(s.snap.Decision)
	return copy
}

func terminal(status Status) bool {
	return status == StatusDone || status == StatusFailed
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneFrame(frame Frame) Frame {
	copy := frame
	if len(frame.Data) > 0 {
		copy.Data = append([]byte(nil), frame.Data...)
	}
	return copy
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
