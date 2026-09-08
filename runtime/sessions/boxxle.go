package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	emulatorsession "github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/boxxle"
)

// BoxxlePlanner returns one already-validated warehouse step.
type BoxxlePlanner interface {
	Plan(ctx context.Context, obs boxxle.Observation) (BoxxlePlan, error)
}

type BoxxlePlan struct {
	Move     boxxle.Move
	Decision json.RawMessage
}

type BoxxlePlannerFactory func(config LaunchConfig) (BoxxlePlanner, error)

// NewBoxxleRunnerFactory builds the production Boxxle runtime factory.
func NewBoxxleRunnerFactory() RunnerFactory {
	return &boxxleRunnerFactory{
		open:     openGomeboyBoxxleRuntime,
		planners: map[string]BoxxlePlannerFactory{"heuristic": heuristicBoxxleFactory},
		now:      time.Now,
		pacer:    defaultPacerFactory,
	}
}

func heuristicBoxxleFactory(LaunchConfig) (BoxxlePlanner, error) {
	return heuristicBoxxlePlanner{}, nil
}

type boxxleRunnerFactory struct {
	open     func(string) (boxxleRuntime, error)
	planners map[string]BoxxlePlannerFactory
	now      func() time.Time
	pacer    pacerFactory
}

func (f *boxxleRunnerFactory) New(config LaunchConfig) (Runner, error) {
	if config.Profile != boxxle.ProfileID {
		return nil, fmt.Errorf("sessions: unsupported profile %q; Boxxle runtime supports %q", config.Profile, boxxle.ProfileID)
	}
	factory := f.planners[config.Planner]
	if factory == nil {
		return nil, fmt.Errorf("sessions: unsupported Boxxle planner %q", config.Planner)
	}
	planner, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("sessions: configure Boxxle planner %q: %w", config.Planner, err)
	}
	if planner == nil {
		return nil, fmt.Errorf("sessions: Boxxle planner %q factory returned nil", config.Planner)
	}
	open := f.open
	if open == nil {
		open = openGomeboyBoxxleRuntime
	}
	now := f.now
	if now == nil {
		now = time.Now
	}
	newPacer := f.pacer
	if newPacer == nil {
		newPacer = defaultPacerFactory
	}
	return &boxxleRunner{config: config, open: open, planner: planner, now: now, pacer: newPacer}, nil
}

type boxxleRunner struct {
	config  LaunchConfig
	open    func(string) (boxxleRuntime, error)
	planner BoxxlePlanner
	now     func() time.Time
	pacer   pacerFactory
}

type boxxleRuntime interface {
	ROMHash() string
	CartridgeTitle() string
	Start(ctx context.Context) error
	Observe() (boxxle.Observation, error)
	Execute(ctx context.Context, move boxxle.Move) (boxxle.Observation, error)
	Close() error
}

type observedBoxxleRuntime interface {
	ExecuteObserved(ctx context.Context, move boxxle.Move, observer boxxle.MoveFrameObserver) (boxxle.Observation, error)
}

type gomeboyBoxxleRuntime struct {
	session *emulatorsession.Session
}

func openGomeboyBoxxleRuntime(path string) (boxxleRuntime, error) {
	sess, err := emulatorsession.OpenROMWithVideo(path)
	if err != nil {
		return nil, err
	}
	return &gomeboyBoxxleRuntime{session: sess}, nil
}

func (r *gomeboyBoxxleRuntime) ROMHash() string        { return r.session.ROMHash() }
func (r *gomeboyBoxxleRuntime) CartridgeTitle() string { return r.session.Cartridge().Title }
func (r *gomeboyBoxxleRuntime) Start(ctx context.Context) error {
	return boxxle.StartRoom1(ctx, r.session.Emulator())
}
func (r *gomeboyBoxxleRuntime) Observe() (boxxle.Observation, error) {
	return boxxle.Observe(r.session.Emulator()), nil
}
func (r *gomeboyBoxxleRuntime) Execute(ctx context.Context, move boxxle.Move) (boxxle.Observation, error) {
	return boxxle.ExecuteMove(ctx, r.session.Emulator(), move)
}
func (r *gomeboyBoxxleRuntime) ExecuteObserved(ctx context.Context, move boxxle.Move, observer boxxle.MoveFrameObserver) (boxxle.Observation, error) {
	return boxxle.ExecuteMoveObserved(ctx, r.session.Emulator(), move, observer)
}
func (r *gomeboyBoxxleRuntime) CaptureFrame() (Frame, error) {
	return captureSessionFrame(r.session)
}
func (r *gomeboyBoxxleRuntime) Close() error { return r.session.Close() }

func (r *boxxleRunner) Run(ctx context.Context, publish func(Update)) (result Result, retErr error) {
	if err := ctx.Err(); err != nil {
		return Result{Reason: "stopped"}, err
	}
	runtime, err := r.open(r.config.ROMPath)
	if err != nil {
		return Result{}, fmt.Errorf("sessions: open Boxxle runtime: %w", err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("sessions: close emulator: %w", err))
		}
	}()

	hash := runtime.ROMHash()
	if err := (boxxle.Profile{}).RequireROM(hash); err != nil {
		return Result{}, err
	}
	if err := runtime.Start(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return Result{Reason: "stopped"}, err
		}
		return Result{}, fmt.Errorf("sessions: Boxxle startup: %w", err)
	}
	obs, err := runtime.Observe()
	if err != nil {
		return Result{}, fmt.Errorf("sessions: initial Boxxle observation: %w", err)
	}
	if !obs.Ready && !obs.Solved {
		return Result{}, fmt.Errorf("sessions: Boxxle startup returned non-ready frame %d", obs.Frame)
	}

	image, err := captureFrame(runtime)
	if err != nil {
		return Result{}, err
	}
	var lastPlannerLatencyMS int64
	planningStarted := r.now()
	if err := publishBoxxle(publish, r.config.Planner, runtime.CartridgeTitle(), hash, 0, obs, nil, "planning", image, planningStarted, lastPlannerLatencyMS); err != nil {
		return Result{}, err
	}

	moves := 0
	for r.config.MoveLimit == 0 || moves < r.config.MoveLimit {
		if err := ctx.Err(); err != nil {
			return Result{Reason: "stopped"}, err
		}
		if obs.Solved {
			return Result{Reason: "solved"}, nil
		}

		before := obs
		plan, err := r.planner.Plan(ctx, before)
		plannerLatency := r.now().Sub(planningStarted)
		if plannerLatency < 0 {
			plannerLatency = 0
		}
		plannerLatencyMS := plannerLatency.Milliseconds()
		lastPlannerLatencyMS = plannerLatencyMS
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Result{Reason: "stopped"}, err
			}
			if errors.Is(err, boxxle.ErrNoMove) {
				return Result{Reason: "no_move"}, nil
			}
			return Result{}, fmt.Errorf("sessions: Boxxle move %d plan: %w", moves+1, err)
		}
		if err := publishBoxxle(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, before, plan.Decision, "executing", nil, time.Time{}, plannerLatencyMS); err != nil {
			return Result{}, err
		}

		after, err := r.execute(ctx, runtime, hash, moves, before, plan, plannerLatencyMS, publish)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Result{Reason: "stopped"}, err
			}
			return Result{}, fmt.Errorf("sessions: Boxxle move %d execute: %w", moves+1, err)
		}
		moves++
		obs = after
		image, err = captureFrame(runtime)
		if err != nil {
			return Result{}, err
		}
		planningStarted = r.now()
		if err := publishBoxxle(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, obs, plan.Decision, "planning", image, planningStarted, lastPlannerLatencyMS); err != nil {
			return Result{}, err
		}
	}
	return Result{Reason: "move_limit"}, nil
}

func (r *boxxleRunner) execute(ctx context.Context, runtime boxxleRuntime, hash string, moves int, before boxxle.Observation, plan BoxxlePlan, plannerLatencyMS int64, publish func(Update)) (boxxle.Observation, error) {
	if r.config.Pacing != PacingRealtime {
		return runtime.Execute(ctx, plan.Move)
	}
	observed, ok := runtime.(observedBoxxleRuntime)
	if !ok {
		return runtime.Execute(ctx, plan.Move)
	}

	pacer := r.pacer(PacingRealtime)
	if pacer == nil {
		pacer = fastFramePacer{}
	}
	lastPublished := before
	sampled := 0
	observer := func(mid boxxle.Observation) error {
		if err := pacer.Wait(ctx, mid.Frame); err != nil {
			return err
		}
		sampled++
		important := lastPublished.PlayerX != mid.PlayerX || lastPublished.PlayerY != mid.PlayerY || lastPublished.Ready != mid.Ready || lastPublished.Solved != mid.Solved
		if sampled%presentationSampleEveryFrames != 0 && !important {
			return nil
		}
		image, err := captureFrame(runtime)
		if err != nil {
			return err
		}
		if err := publishBoxxle(publish, r.config.Planner, runtime.CartridgeTitle(), hash, moves, mid, plan.Decision, "executing", image, time.Time{}, plannerLatencyMS); err != nil {
			return err
		}
		lastPublished = mid
		return nil
	}
	return observed.ExecuteObserved(ctx, plan.Move, observer)
}

func publishBoxxle(publish func(Update), planner, title, hash string, moves int, obs boxxle.Observation, decision json.RawMessage, activity string, image *Frame, plannerStartedAt time.Time, plannerLatencyMS int64) error {
	payload, err := json.Marshal(obs)
	if err != nil {
		return fmt.Errorf("sessions: encode Boxxle observation: %w", err)
	}
	publish(Update{
		Profile:          boxxle.ProfileID,
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

type heuristicBoxxlePlanner struct{}

func (heuristicBoxxlePlanner) Plan(_ context.Context, obs boxxle.Observation) (BoxxlePlan, error) {
	decision, err := boxxle.ChooseHeuristicMove(obs)
	if err != nil {
		return BoxxlePlan{}, err
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return BoxxlePlan{}, err
	}
	return BoxxlePlan{Move: decision.Move, Decision: payload}, nil
}
