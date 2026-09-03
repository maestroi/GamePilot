# OpenAI-compatible Tetris planner

The LLM planner is intentionally narrow. It does not control Gomeboy directly and it does not invent arbitrary button sequences. It receives one structured Tetris position plus a deterministic list of legal placement candidates, then returns one `Placement{Rotation, TargetColumn}`.

## Wire protocol

`planner/openai` uses the common OpenAI-compatible Chat Completions shape:

```text
POST {base_url}/chat/completions
```

The base URL normally ends in `/v1`, for example:

- `http://localhost:1234/v1`
- `http://localhost:11434/v1`
- `https://api.openai.com/v1`

The transport sends:

- `model`
- one system message
- one user message
- `temperature: 0`
- a bounded `max_tokens` completion budget
- optionally `chat_template_kwargs.enable_thinking` for servers that support Qwen/vLLM-style thinking control

It reads `choices[0].message.content` and returns that text to the Tetris planner. No provider SDK is required.

GamePilot does not currently require provider-side Structured Outputs. That is deliberate: local OpenAI-compatible servers differ in which response-format extensions they support. Instead, the Tetris profile strictly validates the returned text itself.

## Fast thinking control

GamePilot is a control loop, not a long-form reasoning workload. The model only needs to choose two integers from a deterministic legal candidate list, so the CLI defaults to:

```text
-llm-thinking off
-llm-max-tokens 64
```

For Qwen3 served through vLLM-compatible chat templates, `off` sends:

```json
{"chat_template_kwargs":{"enable_thinking":false}}
```

This is the Qwen/vLLM hard switch for non-thinking mode and can substantially reduce per-move latency for reasoning-capable models.

Thinking modes:

- `off` (default): send `enable_thinking=false`
- `on`: send `enable_thinking=true`
- `auto`: omit the provider-specific field and let the server/model default decide

Use `auto` if a nominally OpenAI-compatible server rejects `chat_template_kwargs`. `-llm-max-tokens` can also be raised if a particular model needs more output budget, but normal GamePilot responses should fit comfortably inside the default.

## Model input

Before each model request, GamePilot computes `LegalSimulations` for the current piece. Top-out placements are removed before prompting.

The prompt contains:

- current piece and raw ROM rotation
- next piece and raw ROM rotation
- score, lines, and level
- the full 18x10 settled board as `#` / `.` text
- every legal candidate's `rotation` and `target_column`
- deterministic one-piece metrics for each candidate: lines cleared, aggregate height, holes, and bumpiness

The candidate metrics are descriptive inputs, not controller commands. They are computed locally from the same exact ROM-derived tetromino geometry used by the heuristic planner.

## Output contract

The model must return exactly:

```json
{"rotation": 2, "target_column": 4}
```

GamePilot uses `json.Decoder.DisallowUnknownFields` and rejects:

- markdown/code fences or non-JSON text
- missing/malformed fields
- additional fields
- rotations outside `0..3`
- placements not present in the deterministic legal candidate set

Invalid model output is re-prompted with the validation error, up to three attempts. HTTP/provider errors are returned immediately rather than hidden behind planner retries.

Only a validated placement reaches `ExecutePlacement`, which still verifies every rotation and horizontal movement from ROM memory.

## CLI configuration

Flags:

```text
-planner llm
-pieces N
-llm-base-url URL
-llm-model MODEL
-llm-api-key-env ENV_NAME
-llm-timeout 60s
-llm-thinking off|auto|on
-llm-max-tokens 64
-replay-out FILE
```

Environment defaults:

- `OPENAI_BASE_URL` overrides the default local URL `http://localhost:1234/v1`
- `OPENAI_MODEL` supplies the default model ID
- `OPENAI_API_KEY` is read by default when an API key is needed

For keyless local servers, pass:

```bash
-llm-api-key-env ''
```

## Local example

Start any server that exposes an OpenAI-compatible `/v1/chat/completions` endpoint, then run a small test first:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 3 \
  -llm-base-url http://localhost:8002/v1 \
  -llm-model '<model-id>' \
  -llm-api-key-env '' \
  -llm-thinking off \
  -replay-out llm-3.json
```

Because `off` and `64` are the defaults, the `-llm-thinking` and `-llm-max-tokens` flags can normally be omitted.

A successful startup line includes the effective latency controls:

```text
LLM: model=<model-id> base_url=http://localhost:8002/v1 thinking=off max_tokens=64
```

If the model returns malformed or illegal output but fixes it on retry, `attempts` will be greater than 1.

## Hosted example

```bash
export OPENAI_BASE_URL='https://api.openai.com/v1'
export OPENAI_MODEL='<model-id>'
export OPENAI_API_KEY='<secret>'

go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 3 \
  -llm-thinking auto \
  -replay-out llm-3.json
```

`auto` is the safest choice for a hosted provider that may not implement the Qwen/vLLM-specific chat-template extension.

Do not commit API keys or ROM files.

## Replay verification

The important property is that the model is not required for reproduction. After an LLM run:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner replay \
  -replay-in llm-3.json
```

Replay verification fresh-boots Tetris, re-executes the model's recorded placements through the strict controller, and verifies every state/frame boundary. This makes model experiments comparable and debuggable even when the model itself is nondeterministic.
