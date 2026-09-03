# OpenAI-compatible Tetris planner

The LLM planner is intentionally narrow. It does not control Gomeboy directly and it does not invent arbitrary button sequences. It receives one structured Tetris position plus a bounded deterministic shortlist of current-piece placements, then returns one `Placement{Rotation, TargetColumn}`.

## Wire protocol

`planner/openai` uses the common OpenAI-compatible Chat Completions shape:

```text
POST {base_url}/chat/completions
```

The base URL normally ends in `/v1`, for example:

- `http://localhost:1234/v1`
- `http://localhost:11434/v1`
- `https://api.openai.com/v1`

The transport sends `model`, one system message, one user message, `temperature: 0`, a bounded `max_tokens` budget, and optionally `chat_template_kwargs.enable_thinking` for Qwen/vLLM-style thinking control. It reads `choices[0].message.content`; no provider SDK is required.

GamePilot performs schema/legal-action validation itself rather than requiring provider-side Structured Outputs, because local OpenAI-compatible servers vary in extension support.

## Fast thinking control

GamePilot is a control loop, not a long-form reasoning workload. Defaults are:

```text
-llm-thinking off
-llm-max-tokens 64
-llm-candidates 10
```

For Qwen3 served through vLLM-compatible chat templates, `off` sends:

```json
{"chat_template_kwargs":{"enable_thinking":false}}
```

Thinking modes:

- `off` (default): send `enable_thinking=false`
- `on`: send `enable_thinking=true`
- `auto`: omit the provider-specific field

Use `auto` if a compatible server rejects `chat_template_kwargs`.

## Two-ply candidate shortlist

Before each model request, GamePilot uses the known ROM preview piece to compute deterministic two-ply candidates:

1. enumerate legal current-piece placements;
2. collapse placements that produce the exact same settled board, keeping the normal cheaper/stabler raw input representation;
3. for each distinct current outcome, enumerate legal placements for the preview piece;
4. keep the best preview reply;
5. score the leaf board using the normal height/holes/bumpiness weights and total line clears across both plies;
6. sort candidates deterministically;
7. send only the best `-llm-candidates N` entries.

The default shortlist size is 10. Lower values reduce prompt prefill; higher values expose more alternatives to the model.

The preview reply is advisory simulation context only. It is never queued for execution. After the real current placement, GamePilot reads the ROM again and replans from the new truth state.

## Model input

The prompt contains:

- current piece and raw ROM rotation
- next piece and raw ROM rotation
- score, lines, and level
- the full 18x10 settled board as `#` / `.` text
- the best N current placements
- immediate line clears and board metrics for each placement
- the best deterministic preview-piece reply for each placement
- total two-ply line clears
- final leaf-board height, holes, and bumpiness
- deterministic two-ply score and rank

Move logs expose prompt compression as `candidates=shown/total`.

## Output contract

The model must return exactly:

```json
{"rotation": 2, "target_column": 4}
```

GamePilot uses `json.Decoder.DisallowUnknownFields` and rejects markdown/non-JSON text, malformed or additional fields, rotations outside `0..3`, and placements not present in the current shortlist.

Invalid model output is re-prompted with the validation error up to three attempts. HTTP/provider errors return immediately. Only a validated current-piece placement reaches `ExecutePlacement`.

## CLI configuration

```text
-planner llm
-pieces N
-llm-base-url URL
-llm-model MODEL
-llm-api-key-env ENV_NAME
-llm-timeout 60s
-llm-thinking off|auto|on
-llm-max-tokens 64
-llm-candidates 10
-replay-out FILE
```

Environment defaults:

- `OPENAI_BASE_URL` overrides `http://localhost:1234/v1`
- `OPENAI_MODEL` supplies the model ID
- `OPENAI_API_KEY` is read by default when needed

For keyless local servers, pass `-llm-api-key-env ''`.

## Local example

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 3 \
  -llm-base-url http://localhost:8002/v1 \
  -llm-model '<model-id>' \
  -llm-api-key-env '' \
  -llm-candidates 10 \
  -replay-out llm-3.json
```

A startup line includes the effective controls:

```text
LLM: model=<model-id> base_url=http://localhost:8002/v1 thinking=off max_tokens=64 candidates=10
```

A move line includes actual shortlist compression, for example:

```text
Move 1: piece=T rotation=2 target_column=0 model=<model-id> attempts=1 candidates=10/34
```

## Replay verification

The model is not required for reproduction. After an LLM run:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner replay \
  -replay-in llm-3.json
```

Replay verification fresh-boots Tetris, re-executes the recorded placements through the strict controller, and verifies every state/frame boundary.
