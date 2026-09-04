package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	emulatorsession "github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/tetris"
)

// TetrisPlanner returns one already-validated game-level placement decision.
// It never receives emulator access.
type TetrisPlanner interface {
	Plan(ctx context.Context, obs tetris.Observation) (TetrisPlan, error)
}

type TetrisPlan struct {
	Placement tetris.Placement
	Decision  json.RawMessage
}

type TetrisPlannerFactory func(config LaunchConfig) (TetrisPlanner, error)

// LLMResolver lets server code map a safe launch config/model alias to a
// configured completer while keeping credentials and provider URLs outside the
// session launch object.
type LLMResolver func(config LaunchConfig) (tetris.JSONCompleter, error)

// LLMPlannerFactory adapts the existing strict Tetris LLM planner to the live
// session planner interface.
func LLMPlannerFactory(resolve LLMResolver) TetrisPlannerFactory {
	return func(config LaunchConfig) (TetrisPlanner, error) {
		if resolve == nil {
			return nil, fmt.Errorf("sessions: LLM resolver is required")
		}
		model, err := resolve(config)
		if err != nil {
			return nil, err
		}
		shortlist := config.PlannerOptions.ShortlistSize
		if shortlist == 0 {
			shortlist = tetris.DefaultLLMShortlistSize
		}
		return llmTetrisPlanner{model: model, shortlist: shortlist}, nil
	}
}

// NewTetrisRunnerFactory builds the production Tetris runtime factory. Extra
// planner factories may add/override planner names (for example "llm").
func NewTetrisRunnerFactory(extra map[string]TetrisPlannerFactory) RunnerFactory {
	planners := map[string]TetrisPlannerFactory{
		"heuristic": func(LaunchConfig) (TetrisPlanner, error) { return heuristicTetrisPlanner{}, nil },
		"lookahead": func(LaunchConfig) (TetrisPlanner, error) { return lookaheadTetrisPlanner{}, nil },
	}
	for name, factory := range extra {
		planners[name] = factory
	}
	return &tetrisRunnerFactory{
		open:     openGomeboyTetrisRuntime,
		planners: planners,
		now:      time.Now,
		pacer:    defaultPacerFactory,
	}
}

// NewTetrisManager is the simplest programmatic live runtime for current
// built-in deterministic planners. Pass an LLM planner factory to
// NewTetrisRunnerFactory when a server has configured model aliases/credentials.
func NewTetrisManager(extra map[string]TetrisPlannerFactory) *Manager {
	return NewManager(NewTetrisRunnerFactory(extra))
}

type tetrisRunnerFactory struct {
	open     func(string) (tetrisRuntime, error)
	planners map[string]TetrisPlannerFactory
	now      func() time.Time
	pacer    pacerFactory
}

func (f *tetrisRunnerFactory) New(config LaunchConfig) (Runner, error) {
	if config.Profile != tetris.ProfileID {
		return nil, fmt.Errorf("sessions: unsupported profile %q; current live runtime supports %q", config.Profile, tetris.ProfileID)
	}
	factory := f.planners[config.Planner]
	if factory == nil {
		return nil, fmt.Errorf("sessions: unsupported Tetris planner %q", config.Planner)
	}
	planner, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("sessions: configure Tetris planner %q: %w", config.Planner, err)
	}
	if planner == nil {
		return nil, fmt.Errorf("sessions: Tetris planner %q factory returned nil", config.Planner)
	}
	open := f.open
	if open == nil {
		open = openGomeboyTetrisRuntime
	}
	now := f.now
	if now == nil {
		now = time.Now
	}
	newPacer := f.pacer
	if newPacer == nil {
		newPacer = defaultPacerFactory
	}
	return &tetrisRunner{config: config, open: open, planner: planner, now: now, pacer: newPacer}, nil
}

type tetrisRunner struct {
	config  LaunchConfig
	open    func(string) (tetrisRuntime, error)
	planner TetrisPlanner
	now     func() time.Time
	pacer   pacerFactory
}

type tetrisRuntime interface {
	ROMHash() string
	CartridgeTitle() string
	Start(ctx context.Context) error
	Observe() (tetris.Observation, error)
	Execute(ctx context.Context, placement tetris.Placement) (tetris.Observation, error)
	Close() error
}

// observedTetrisRuntime is optional so existing fake runtimes and fast-mode
// execution stay simple. Production Gomeboy sessions implement it to expose the
// exact controller frames already being stepped.
type observedTetrisRuntime interface {
	ExecuteObserved(ctx context.Context, placement tetris.Placement, observer tetris.PlacementFrameObserver) (tetris.Observation, error)
}

type gomeboyTetrisRuntime struct {
	session *emulatorsession.Session
}

func openGomeboyTetrisRuntime(path string) (tetrisRuntime, error) {
	sess, err := emulatorsession.OpenROMWithVideo(path)
	if err != nil {
		return nil, err
	}
	return &gomeboyTetrisRuntime{session: sess}, nil
}

func (r *gomeboyTetrisRuntime) ROMHash() string { return r.session.ROMHash() }
func (r *gomeboyTetrisRuntime) CartridgeTitle() string { return r.session.Cartridge().Title }
func (r *gomeboyTetrisRuntime) Start(ctx context.Context) error {
	return tetris.StartTypeAZero(ctx, r.session.Emulator())
}
func (r *gomeboyTetrisRuntime) Observe() (tetris.Observation, error) {
	return tetris.Observe(r.session.Emulator())
}
func (r *gomeboyTetrisRuntime) Execute(ctx context.Context, placement tetris.Placement) (tetris.Observation, error) {
	return tetris.ExecutePlacement(ctx, r.session.Emulator(), placement)
}
func (r *gomeboyTetrisRuntime) ExecuteObserved(ctx context.Context, placement tetris.Placement, observer tetris.PlacementFrameObserver) (tetris.Observation, error) {
	return tetris.ExecutePlacementObserved(ctx, r.session.Emulator(), placement, observer)
}
func (r *gomeboyTetrisRuntime) Close() error { return r.session.Close() }

func (r *tetrisRunner) Run(ctx context.Context, publish func(Update)) (result Result, retErr error) {
	if err := ctx.Err(); err != nil {
		return Result{Reason: "stopped"}, err
	}
	runtime, err := r.open(r.config.ROMPath)
	if err != nil {
		return Result{}, fmt.Errorf("sessions: open Tetris runtime: %w", err)
	}

	var replay *tetris.Replay
	defer func() {
		if replay != nil && r.config.RecordReplay {
			var buf bytes.Buffer
			if err := tetris.WriteReplay(&buf, *replay); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("sessions: finalize replay: %w", err))
			} else {
				result.Replay = append([]byte(nil), buf.Bytes()...)
			}
		}
		if err := runtime.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("sessions: close emulator: %w", err))
		}
	}()

	hash := runtime.ROMHash()
	if err := (tetris.Profile{}).RequireROM(hash); err != nil {
		return Result{}, err
	}
	if err := runtime.Start(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return Result{Reason: "stopped"}, err
		}
		return Result{}, fmt.Errorf("sessions: Tetris startup: %w", err)
	}
	obs, err := runtime.Observe()
	if err != nil {
		return Result{}, fmt.Errorf("sessions: initial Tetris observation: %w", err)
	}
	if !obs.Ready && !obs.GameOver {
		return Result{}, fmt.Errorf("sessions: Tetris startup returned non-ready frame %d", obs.Frame)
	}

	if r.config.RecordReplay {
		recorded := tetris.NewReplay(hash, obs)
		replay = &recorded
	}
	image, err := captureFrame(runtime)
	if err != nil {
		return Result{}, err
	}
	planningStarted := r.now()
	if err := publishTetris(publish, r.config.Planner, runtime.CartridgeTitle(), hash, 0, obs, nil, "planning", image, planningStarted, 0); err != nil {
		return Result{}, err
	}

	moves := 0
	for r.config.MoveLimit == 0 || moves < r.config.MoveLimit {
		if err := ctx.Err(); err != nil {
			return Result{Reason: "stopped"}, err
		}
		if obs.GameOver {
			return Result{Reason: "game_over"}, nil
		}

		before := obs
		plan, err := r.planner.Plan(ctx, before)
		plannerLatency := r.now().Sub(planningStarted)
		if plannerLatency < 0 {
			plannerLatency = 0
		}
		plannerLatencyMS := plannerLatency.Milliseconds()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Result{Reason: "stopped"}, err
			}
			if errors.Is(err, tetris.ErrNoPlacement) {
				return Result{Reason: "no_placement"}, nil
			}
			return Result{}, fmt.Errorf("sessions: Tetris move %d plan: %w", moves+1, err)
		}
		if err := publishTetris(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, before, plan.Decision, "executing", nil, time.Time{}, plannerLatencyMS); err != nil {
			return Result{}, err
		}

		after, err := r.execute(ctx, runtime, hash, moves, before, plan, plannerLatencyMS, publish)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Result{Reason: "stopped"}, err
			}
			return Result{}, fmt.Errorf("sessions: Tetris move %d execute: %w", moves+1, err)
		}
		if replay != nil {
			if err := replay.Append(before, plan.Placement, after); err != nil {
				return Result{}, fmt.Errorf("sessions: Tetris move %d replay: %w", moves+1, err)
			}
		}
		moves++
		obs = after
		image, err = captureFrame(runtime)
		if err != nil {
			return Result{}, err
		}
		planningStarted = r.now()
		if err := publishTetris(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, obs, plan.Decision, "planning", image, planningStarted, 0); err != nil {
			return Result{}, err
		}
	}
	return Result{Reason: "move_limit"}, nil
}

func (r *tetrisRunner) execute(ctx context.Context, runtime tetrisRuntime, hash string, moves int, before tetris.Observation, plan TetrisPlan, plannerLatencyMS int64, publish func(Update)) (tetris.Observation, error) {
	if r.config.Pacing != PacingRealtime {
		return runtime.Execute(ctx, plan.Placement)
	}
	observed, ok := runtime.(observedTetrisRuntime)
	if !ok {
		return runtime.Execute(ctx, plan.Placement)
	}

	pacer := r.pacer(PacingRealtime)
	if pacer == nil {
		pacer = fastFramePacer{}
	}
	lastPublished := before
	sampled := 0
	observer := func(mid tetris.Observation) error {
		if err := pacer.Wait(ctx, mid.Frame); err != nil {
			return err
		}
		sampled++
		important := presentationStateChanged(lastPublished, mid)
		if sampled%presentationSampleEveryFrames != 0 && !important {
			return nil
		}
		image, err := captureFrame(runtime)
		if err != nil {
			return err
		}
		if err := publishTetris(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, mid, plan.Decision, "executing", image, time.Time{}, plannerLatencyMS); err != nil {
			return err
		}
		lastPublished = mid
		return nil
	}
	return observed.ExecuteObserved(ctx, plan.Placement, observer)
}

func presentationStateChanged(previous, current tetris.Observation) bool {
	return previous.CurrentPiece.Rotation != current.CurrentPiece.Rotation ||
		previous.CurrentPiece.AnchorX != current.CurrentPiece.AnchorX ||
		previous.CurrentPiece.Visible != current.CurrentPiece.Visible ||
		previous.Ready != current.Ready ||
		previous.GameOver != current.GameOver ||
		previous.GameState != current.GameState
}

func publishTetris(publish func(Update), planner, title, hash string, moves int, obs tetris.Observation, decision json.RawMessage, activity string, image *Frame, plannerStartedAt time.Time, plannerLatencyMS int64) error {
	payload, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("sessions: encode Tetris observation: %w", err)
	}
	publish(Update{
		Profile:          tetris.ProfileID,
		ROMSHA256:        hash,
		CartridgeTitle:   title,
		Frame:            obs.Frame,
		Moves:            moves,
		Observation:      payload,
		Decision:         decision,
		PlannerActivity:  activity,
		PlannerStartedAt: plannerStartedAt,
		PlannerLatencyMS: plannerLatencyMS,
		Image:            image,
	})
	return nil
}

type heuristicTetrisPlanner struct{}

func (heuristicTetrisPlanner) Plan(_ context.Context, obs tetris.Observation) (TetrisPlan, error) {
	decision, err := tetris.ChooseHeuristicPlacement(obs)
	if err != nil {
		return TetrisPlan{}, err
	}
	payload, err := json.Marshal(struct {
		Placement       tetris.Placement `json:"placement"`
		Score           float64          `json:"score"`
		LinesCleared    int              `json:"lines_cleared"`
		AggregateHeight int              `json:"aggregate_height"`
		Holes           int              `json:"holes"`
		Bumpiness       int              `json:"bumpiness"`
	}{
		Placement:       decision.Placement,
		Score:           decision.Score,
		LinesCleared:    decision.LinesCleared,
		AggregateHeight: decision.AggregateHeight,
		Holes:           decision.Holes,
		Bumpiness:       decision.Bumpiness,
	})
	if err != nil {
		return TetrisPlan{}, err
	}
	return TetrisPlan{Placement: decision.Placement, Decision: payload}, nil
}

type lookaheadTetrisPlanner struct{}

func (lookaheadTetrisPlanner) Plan(_ context.Context, obs tetris.Observation) (TetrisPlan, error) {
	decision, err := tetris.ChooseLookaheadPlacement(obs)
	if err != nil {
		return TetrisPlan{}, err
	}
	payload, err := json.Marshal(struct {
		Placement  tetris.Placement   `json:"placement"`
		Score      float64            `json:"score"`
		TotalLines int                `json:"total_lines"`
		Reply      *tetris.Simulation `json:"reply,omitempty"`
	}{
		Placement:  decision.First.Placement,
		Score:      decision.Score,
		TotalLines: decision.TotalLines,
		Reply:      decision.Reply,
	})
	if err != nil {
		return TetrisPlan{}, err
	}
	return TetrisPlan{Placement: decision.First.Placement, Decision: payload}, nil
}

type llmTetrisPlanner struct {
	model     tetris.JSONCompleter
	shortlist int
}

func (p llmTetrisPlanner) Plan(ctx context.Context, obs tetris.Observation) (TetrisPlan, error) {
	decision, err := tetris.ChooseLLMPlacementWithShortlist(ctx, p.model, obs, p.shortlist)
	if err != nil {
		return TetrisPlan{}, err
	}
	payload, err := json.Marshal(struct {
		Placement       tetris.Placement `json:"placement"`
		Attempts        int              `json:"attempts"`
		Candidates      int              `json:"candidates"`
		TotalCandidates int              `json:"total_candidates"`
	}{
		Placement:       decision.Placement,
		Attempts:        decision.Attempts,
		Candidates:      decision.Candidates,
		TotalCandidates: decision.TotalCandidates,
	})
	if err != nil {
		return TetrisPlan{}, err
	}
	return TetrisPlan{Placement: decision.Placement, Decision: payload}, nil
}
