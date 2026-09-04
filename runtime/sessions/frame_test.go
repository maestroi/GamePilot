package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

func TestEncodePNGFrame(t *testing.T) {
	encoded, err := encodePNGFrame(gomeboy.Frame{
		Width:  2,
		Height: 1,
		RGB: []byte{
			0xff, 0x00, 0x00,
			0x00, 0xff, 0x00,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode encoded PNG: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 1 {
		t.Fatalf("PNG bounds = %v, want 2x1", got)
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); r != 0xffff || g != 0 || b != 0 {
		t.Fatalf("first pixel = (%04x,%04x,%04x), want red", r, g, b)
	}
}

func TestManagerRetainsOnlyLatestFrameAndCopiesData(t *testing.T) {
	first := mustTestPNG(t, 1)
	second := mustTestPNG(t, 2)
	factory := RunnerFactoryFunc(func(LaunchConfig) (Runner, error) {
		return runnerFunc(func(_ context.Context, publish func(Update)) (Result, error) {
			publish(Update{
				Profile:     "tetris",
				Frame:       100,
				Observation: json.RawMessage(`{"step":1}`),
				Image: &Frame{EmulatorFrame: 100, Width: 1, Height: 1, ContentType: "image/png", Data: first},
			})
			publish(Update{
				Profile:     "tetris",
				Frame:       101,
				Observation: json.RawMessage(`{"step":2}`),
				Image: &Frame{EmulatorFrame: 101, Width: 1, Height: 1, ContentType: "image/png", Data: second},
			})
			return Result{Reason: "completed"}, nil
		}), nil
	})
	m := newManager(factory, time.Now, func() (string, error) { return "frames", nil })
	id, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, err := m.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.FrameAvailable || snap.FrameSequence == 0 || snap.Sequence <= snap.FrameSequence {
		t.Fatalf("unexpected freshness metadata: sequence=%d frame_sequence=%d available=%v", snap.Sequence, snap.FrameSequence, snap.FrameAvailable)
	}
	frame, err := m.Frame(id)
	if err != nil {
		t.Fatal(err)
	}
	if frame.EmulatorFrame != 101 || !bytes.Equal(frame.Data, second) {
		t.Fatalf("latest frame = emulator %d bytes=%d, want emulator 101 latest image", frame.EmulatorFrame, len(frame.Data))
	}
	frame.Data[0] ^= 0xff
	again, err := m.Frame(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Data, second) {
		t.Fatal("manager frame mutated through caller-owned byte slice")
	}
}

func TestReadHandlerServesSnapshotAndPNG(t *testing.T) {
	image := mustTestPNG(t, 3)
	factory := RunnerFactoryFunc(func(LaunchConfig) (Runner, error) {
		return runnerFunc(func(_ context.Context, publish func(Update)) (Result, error) {
			publish(Update{
				Profile:         "tetris",
				Frame:           77,
				Moves:           2,
				Observation:     json.RawMessage(`{"score":123}`),
				PlannerActivity: "planning",
				Image: &Frame{EmulatorFrame: 77, Width: 1, Height: 1, ContentType: "image/png", Data: image},
			})
			return Result{Reason: "completed"}, nil
		}), nil
	})
	m := newManager(factory, time.Now, func() (string, error) { return "web", nil })
	id, err := m.Start(LaunchConfig{ROMPath: "/private/tetris.gb", Profile: "tetris", Planner: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Wait(ctx, id); err != nil {
		t.Fatal(err)
	}

	handler := NewReadHandler(m)
	stateReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/web", nil)
	stateRes := httptest.NewRecorder()
	handler.ServeHTTP(stateRes, stateReq)
	if stateRes.Code != http.StatusOK {
		t.Fatalf("state status = %d: %s", stateRes.Code, stateRes.Body.String())
	}
	if stateRes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("state cache control = %q", stateRes.Header().Get("Cache-Control"))
	}
	var snap Snapshot
	if err := json.NewDecoder(stateRes.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.ID != "web" || snap.Frame != 77 || !snap.FrameAvailable || snap.FrameSequence == 0 {
		t.Fatalf("unexpected HTTP snapshot: %+v", snap)
	}
	if bytes.Contains(stateRes.Body.Bytes(), []byte("/private/tetris.gb")) {
		t.Fatal("snapshot HTTP response leaked private ROM path")
	}

	frameReq := httptest.NewRequest(http.MethodGet, "/v1/sessions/web/frame", nil)
	frameRes := httptest.NewRecorder()
	handler.ServeHTTP(frameRes, frameReq)
	if frameRes.Code != http.StatusOK {
		t.Fatalf("frame status = %d: %s", frameRes.Code, frameRes.Body.String())
	}
	if frameRes.Header().Get("Content-Type") != "image/png" || frameRes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected frame headers: %v", frameRes.Header())
	}
	if frameRes.Header().Get("X-GamePilot-Sequence") == "" || frameRes.Header().Get("X-GamePilot-Emulator-Frame") != "77" {
		t.Fatalf("missing frame freshness headers: %v", frameRes.Header())
	}
	body, err := io.ReadAll(frameRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, image) {
		t.Fatal("HTTP frame body differs from published PNG")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/v1/sessions/web", nil)
	postRes := httptest.NewRecorder()
	handler.ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", postRes.Code)
	}
}

func TestFrameUnavailable(t *testing.T) {
	factory := RunnerFactoryFunc(func(LaunchConfig) (Runner, error) {
		return runnerFunc(func(context.Context, func(Update)) (Result, error) {
			return Result{Reason: "completed"}, nil
		}), nil
	})
	m := newManager(factory, time.Now, func() (string, error) { return "no-frame", nil })
	id, err := m.Start(LaunchConfig{ROMPath: "x", Profile: "tetris", Planner: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Wait(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Frame(id); !errors.Is(err, ErrFrameUnavailable) {
		t.Fatalf("Frame error = %v, want ErrFrameUnavailable", err)
	}
}

func mustTestPNG(t *testing.T, value byte) []byte {
	t.Helper()
	data, err := encodePNGFrame(gomeboy.Frame{Width: 1, Height: 1, RGB: []byte{value, value, value}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
