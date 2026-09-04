package sessions

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

// frameCapturer is intentionally narrower than tetrisRuntime so existing fake
// runtimes/tests that do not care about video stay valid. Production observable
// runtimes implement it; runners publish a frame only when it is available.
type frameCapturer interface {
	CaptureFrame() (Frame, error)
}

func (r *gomeboyTetrisRuntime) CaptureFrame() (Frame, error) {
	emu := r.session.Emulator()
	rendered := emu.Frame()
	data, err := encodePNGFrame(rendered)
	if err != nil {
		return Frame{}, err
	}
	return Frame{
		EmulatorFrame: emu.FrameCount(),
		Width:         rendered.Width,
		Height:        rendered.Height,
		ContentType:   "image/png",
		Data:          data,
	}, nil
}

func captureFrame(runtime tetrisRuntime) (*Frame, error) {
	capturer, ok := runtime.(frameCapturer)
	if !ok {
		return nil, nil
	}
	frame, err := capturer.CaptureFrame()
	if err != nil {
		return nil, fmt.Errorf("sessions: capture framebuffer: %w", err)
	}
	return &frame, nil
}

func encodePNGFrame(frame gomeboy.Frame) ([]byte, error) {
	if frame.Width <= 0 || frame.Height <= 0 {
		return nil, fmt.Errorf("sessions: invalid framebuffer dimensions %dx%d", frame.Width, frame.Height)
	}
	want := frame.Width * frame.Height * 3
	if len(frame.RGB) != want {
		return nil, fmt.Errorf("sessions: invalid framebuffer length %d for %dx%d RGB frame", len(frame.RGB), frame.Width, frame.Height)
	}

	img := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	for src, dst := 0, 0; src < len(frame.RGB); src, dst = src+3, dst+4 {
		img.Pix[dst+0] = frame.RGB[src+0]
		img.Pix[dst+1] = frame.RGB[src+1]
		img.Pix[dst+2] = frame.RGB[src+2]
		img.Pix[dst+3] = 0xff
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("sessions: encode framebuffer PNG: %w", err)
	}
	return out.Bytes(), nil
}
