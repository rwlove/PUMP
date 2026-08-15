# CV Auto-Log: Planning Document

## Goal

Camera-based system that watches the athlete in a personal home gym, detects
exercises being performed (which exercise, weight, reps, sets), and writes
the detections into PUMP for the current day. The athlete only intervenes
when a detection is wrong or when the system asks for confirmation.

## Scope and constraints

- **Setting:** personal gym, single athlete, single user account.
- **Cameras:** two fixed RTSP IP cameras, mounted in upper corners of the
  room. Positioning to be determined together for maximum coverage and
  minimum occlusion.
- **Equipment in scope:** dumbbells, barbells (loaded plates), and the
  Beyond Power Voltra 1.
- **Exercise vocabulary:** exercises defined in PUMP. New exercises will be
  added over time; the system must support enrollment of new exercises by
  recording short reference clips.
- **Latency target:** real-time (sets visible in PUMP within seconds of
  completing them).
- **Failure mode:** when confidence is low, write a pending set immediately
  and notify the athlete (Pushover) for confirmation.
- **Manual workflow preserved:** a config-page toggle (`CVAutoLog`)
  controls whether the system writes anything. When off, manual entry
  works exactly as it does today.
- **Deployment:** Kubernetes cluster. Kubernetes work is out of scope for
  this plan; the plan assumes `pump-cv` is scheduled with access to the
  GPU.

## Architecture

```
┌──────────┐  ┌──────────┐
│ Cam A    │  │ Cam B    │   RTSP, 1080p H.264, fixed
└────┬─────┘  └────┬─────┘
     └────────┬────┘
              ▼
   ┌──────────────────────┐
   │ pump-cv (Python)     │   single process; asyncio
   │  ┌────────────────┐  │
   │  │ ffmpeg/NVDEC   │  │   per-cam decode (HW)
   │  │ pose extract   │  │   YOLO-Pose / RTMPose
   │  │ athlete picker │  │   single-user → trivial
   │  │ rep + set FSM  │  │   per-cam state machines
   │  │ exercise clf   │  │   prototype matching v1, learned later
   │  │ weight modules │  │   plate-color, dumbbell OCR, Voltra (opaque)
   │  │ multi-cam fuse │  │   3D triangulation (cameras calibrated)
   │  └────────────────┘  │
   └──────────┬───────────┘
              │ JSON over HTTP, X-Api-Key
              ▼
   ┌──────────────────────┐
   │ PUMP API (Go)        │   existing process
   │  + new endpoints     │   POST /api/sets, PATCH, confirm
   │  + new Set fields    │   Source, Confidence, Pending
   │  + SSE stream        │   live UI updates
   │  + Pushover sender   │   notifications for low-confidence sets
   └──────────────────────┘
```

Two processes, two languages, one JSON API contract. PUMP remains the
source of truth for sets. `pump-cv` is a producer that writes through the
API. Pushover credentials live in PUMP only — `pump-cv` does not need
public-internet egress.

## Hardware

- **GPU:** nVidia P40 (Pascal, 24 GB GDDR5, ~12 TFLOPS FP32, ~47 TOPS INT8,
  NVDEC for H.264/HEVC, no tensor cores). Sufficient for this workload
  with significant headroom; TensorRT INT8 path exploits Pascal's strong
  INT8 performance.
- **Cameras:** two RTSP IP cameras (model TBD), 1080p, 30 fps target.
  Bandwidth ~4–8 Mbps each.
- **Compute node:** runs `pump-cv` container with GPU passthrough via
  NVIDIA device plugin. Modest CPU/RAM (decode is on NVDEC).
- **One-time camera calibration:** ~10 minutes with a printed
  checkerboard, both intrinsics (per camera) and extrinsics
  (camera-to-camera transform) for 3D fusion.

## PUMP-side changes

### Schema migration v4

Append to `pgMigrations` in `internal/db/postgres.go`:

```sql
ALTER TABLE sets
  ADD COLUMN IF NOT EXISTS source     TEXT             NOT NULL DEFAULT 'manual',
  ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 1.0,
  ADD COLUMN IF NOT EXISTS pending    BOOLEAN          NOT NULL DEFAULT FALSE;
```

Idempotent and backfills existing rows as `manual`, confidence 1.0,
not pending — matching their actual provenance.

### Model

`models.Set` gains:

```go
Source     string  `db:"SOURCE"     json:"Source"`     // "manual" | "cv"
Confidence float64 `db:"CONFIDENCE" json:"Confidence"` // 0.0–1.0
Pending    bool    `db:"PENDING"    json:"Pending"`
```

`models.Conf` gains:

```go
CVAutoLog       bool   // master toggle
PushoverUserKey string // for low-confidence notifications
PushoverAppToken string
```

### API endpoints

Existing endpoints preserved unchanged. New endpoints:

- `POST   /api/sets`                  — append a single set; returns the new ID
- `PATCH  /api/sets/:id`              — update fields on a single set (used by both UI corrections and CV refinements)
- `POST   /api/sets/:id/confirm`      — clear `pending`; optionally accept edits in the body
- `DELETE /api/sets/:id`              — delete one set
- `GET    /api/sets/stream`           — SSE: server-sent events for set add/update/delete

The legacy `PUT /api/sets/date/:date` (bulk-replace) stays for the
manual-only path. When `CVAutoLog` is on, the today-view UI switches to
per-set ops so manual edits and CV writes do not clobber each other.

### Web UI

- **Config page:** new toggle for `CVAutoLog` and fields for Pushover
  credentials. Off by default.
- **Today view:** when `CVAutoLog` is on, opens an SSE connection to
  `/api/sets/stream` so CV-written sets appear live. CV-detected sets
  render with a distinct visual marker (small camera icon, faded color).
  Pending (low-confidence) sets render greyed with a tap-to-confirm
  affordance and a small note on what's uncertain (e.g. "weight: 95 lb
  with 62% confidence").
- **Confirmation flow:** tapping a pending set opens a quick edit panel
  (weight / reps / exercise) with confirm and reject buttons.

### Pushover integration

When a pending set is written, PUMP sends a Pushover message containing
exercise name, detected weight × reps, and a deeplink to the today view.
PUMP holds Pushover credentials; `pump-cv` does not.

## pump-cv design

### Stack

- Python 3.12, asyncio, FastAPI for control surface
- ffmpeg with NVDEC for RTSP decode
- PyTorch + Ultralytics (YOLOv8) and/or MMPose (RTMPose) for pose
- TensorRT for inference acceleration (INT8 where viable)
- OpenCV for calibration math, drawing, basic CV utilities
- PaddleOCR or EasyOCR for dumbbell OCR (episodic, not per-frame)
- PUMP API client (small, hand-written; `httpx` or `aiohttp`)

### Repo layout

Lives in the PUMP monorepo under `cv/`. Considered a standalone repo
(different language, different image, different secrets) but for a
single-developer project the monorepo wins on atomic cross-component
changes (an API field plus its CV consumer in one commit) and one place
to find anything. Two Containerfiles in the same repo: `Containerfile`
for the Go service, `cv/Containerfile` for the Python sidecar; CI scopes
each build to its respective path. Tags continue on the same `v0.0.N`
sequence regardless of which side changed.

Structure:

```
PUMP/
  cv/
    pump_cv/
      pose/           # types + YOLOv8 source + synthetic mock
      tracking/       # single-athlete picker
      fsm/            # rep counter + set-boundary state machine
      pipeline/       # runner that wires source → API
      classify/       # (later) exercise classifier
      weight/         # (later) plate-color, dumbbell-OCR, voltra modules
      fusion/         # (later) multi-cam fusion
      config.py
      log.py
      pump_client.py
      main.py
    models/           # downloaded weights (gitignored; or pulled at startup)
    fixtures/         # video clips for development (gitignored)
    calibration/      # checkerboard data, computed intrinsics/extrinsics
    configs/          # yaml configs
    tests/
    Containerfile
    README.md
    pyproject.toml
```

### Subsystem design notes

- **Pose extraction:** start with YOLOv8-Pose-medium @ 640px. Migrate to
  RTMPose if accuracy demands it.
- **Athlete picker:** single-user gym → take the largest/most-central
  person bounding box per frame and track across frames with a simple
  IoU tracker.
- **Rep counter:** for each exercise, a config maps to the relevant joint
  angle(s) (e.g. squat → knee flexion; bench → elbow flexion). Smooth the
  signal, peak-detect, count reps. Confidence comes from amplitude
  consistency and period regularity.
- **Set boundaries:** 25–30 s with no rep-pattern motion ends the set.
  Tunable per exercise.
- **Exercise classifier (v1):** prototype matching with DTW on keypoint
  sequences. Each PUMP exercise has one or more reference clips recorded
  by the athlete. Classify the active rep window against all prototypes
  and pick the closest. Confidence = (gap between best and second-best
  match) / best match distance.
- **Exercise classifier (v2, later):** train a small temporal CNN or
  Transformer on accumulated confirmed sets (sets that were `pending`
  and got confirmed without edits become high-quality labels).
- **Weight detection — barbell:** detect bar with YOLOv8; for each bar
  end, segment plates and color-classify. Standard color code (lb): red
  45, blue 35, yellow 25, green 10, white 5. Two cameras solve the
  back-side-of-bar occlusion problem when angles cooperate. Output: per
  side plate count + colors → total load. Confidence drops sharply when
  same-color stacks exceed 1, when collars hide plate edges, or when
  a side is fully occluded in both cameras.
- **Weight detection — dumbbell:** OCR the head when visible (good
  light, head-on angle). Fallback: rack-slot heuristic (calibrate "slot
  N = X lb" once, infer weight from where the dumbbell came from in
  frame).
- **Weight detection — Voltra (v1):** **opaque.** CV does pose-based
  reps + ROM. The system prompts for resistance once when it detects a
  Voltra set has started. Future work: BLE GATT reverse-engineering
  spike (see below).
- **Multi-cam fusion:** with calibrated cameras, lift 2D keypoints to 3D
  by triangulation. Pose, rep counting, and classifier operate on 3D
  keypoints — more robust to camera-specific occlusion. Weight detection
  remains per-cam with an arbiter (highest-confidence wins).
- **Confirmation queue:** when set confidence below threshold, write
  `pending=true` via API; PUMP fans out the Pushover notification.

### Configuration

`pump-cv` config is a single yaml file (mounted as a Kubernetes
ConfigMap) with sections for cameras (RTSP URLs, calibration paths),
PUMP (base URL, API key — Secret), and tunables (rep threshold, set
gap, confidence cutoff).

## Phased plan

### Phase 0 — Foundation (PUMP-only, no CV)

Deliverables:

- Schema migration v4 (`source`, `confidence`, `pending` on `sets`)
- `models.Set` and `models.Conf` updates
- New endpoints: `POST /api/sets`, `PATCH /api/sets/:id`,
  `POST /api/sets/:id/confirm`, `DELETE /api/sets/:id`,
  `GET /api/sets/stream`
- Config page: `CVAutoLog` toggle, Pushover credentials fields
- Today view: SSE subscription, distinct rendering for CV vs manual
  sets, pending-set tap-to-confirm UI
- Pushover sender (PUMP-side)

Outcome: PUMP can receive and display CV-written sets *if* we had a CV
producer. Manual UX unchanged when toggle is off.

### Phase 1 — Pipeline + reps + barbell weight

Deliverables:

- `pump-cv` repo bootstrapped (Containerfile, asyncio main loop)
- RTSP capture with NVDEC decode for both cameras
- YOLOv8-Pose inference per camera
- Athlete picker (single user)
- Rep counter for 3 hand-coded exercises (squat, bench, deadlift)
- Set-boundary FSM
- Plate-color barbell weight detector
- PUMP API client; writes detected sets via `POST /api/sets`
- One-time camera calibration script + persisted intrinsics/extrinsics
- 2D-to-3D triangulation for fused pose

User-facing requirement for Phase 1: athlete tells the system which
exercise is starting via the PUMP UI ("Start: Bench Press"). CV does
reps + sets + weight from there.

Outcome: walk in, tap "starting Bench Press", do your sets — PUMP fills
in reps and weight in real time.

### Phase 2 — Auto exercise classification

Deliverables:

- Reference-clip recording flow: PUMP UI lets the athlete record a short
  clip per exercise; clip is sent to `pump-cv` for prototype extraction
- Prototype-matching classifier (DTW on keypoint sequences)
- Confidence scoring; low-confidence → `pending=true` + Pushover

Outcome: walk in and lift; the system identifies the exercise, reps,
sets, and (where possible) weight without manual hints.

### Phase 3 — Robustness

Deliverables:

- Dumbbell OCR + rack-slot heuristic
- Snippet capture per detected set (short video clip) for review on
  wrong detections
- Learned classifier trained on accumulated confirmed sets (replaces or
  augments prototype matching)
- Tuning passes for the real exercise vocabulary

### Phase 4 — Stretch (opt-in features)

- Form feedback (depth, bar path, tempo overlays)
- Velocity-based training estimates from bar speed
- Voltra BLE GATT spike (see Risks below)

## Risks and open questions

- **Plate-color detection accuracy** is the biggest unknown for weight.
  Same-color stacks, collars hiding plate edges, mixed iron-and-bumper
  sets, and back-side occlusion all hurt. Two-camera coverage helps but
  doesn't eliminate the problem. Mitigation: aggressive use of the
  `pending` path so the athlete corrects rather than the system writing
  silently wrong values.
- **Lighting and angle dependence.** Pose models tolerate a lot but
  back-lit shots, deep shadows, and extreme oblique angles all degrade
  keypoint quality. Camera positioning and gym lighting will matter.
- **New-exercise enrollment quality.** Single-clip prototypes can be
  brittle. Encouraging multiple reference clips per exercise (different
  weights, slight angle variation) is cheap insurance.
- **Voltra opacity.** Phase 1–3 plan treats it as opaque (CV reps + ROM,
  prompt for resistance). A future spike (time-boxed ~1 week) could
  reverse-engineer the BLE GATT protocol the Beyond+ app uses; if
  successful, the Voltra branch becomes a clean BLE subscriber. If not,
  the fallback is acceptable.
- **Idle GPU power.** P40 idles at ~50 W. For a personal gym used
  ~1–2 h/day, consider scaling the `pump-cv` Deployment to zero outside
  gym hours (cron HPA or motion-triggered scale-up).
- **Driver path on Pascal.** P40 is supported by current NVIDIA drivers
  and CUDA 12.x, but verify the K8s node image and device plugin
  combination at the start of Phase 1.
- **API auth surface widening.** `pump-cv` as a server-to-server API
  client widens the blast radius of a key leak. Consider a dedicated
  key for `pump-cv` rather than reusing the server's primary `API_KEY`.

## Decisions log

| Topic | Decision |
|---|---|
| API write strategy | Append via new endpoints; keep legacy bulk-replace for manual-only |
| Pending set UI | Appear immediately, greyed, tap-to-confirm |
| Equipment in scope | Dumbbells, barbells, Voltra |
| Exercise enrollment | Reference clips recorded by athlete, prototype matching v1 |
| Voltra integration | Opaque (CV reps + manual resistance prompt) for now |
| Notifications | Pushover; credentials in PUMP, not in pump-cv |
| Camera calibration | One-time checkerboard pass; 3D triangulation fusion |
| Deployment | Kubernetes; pump-cv is its own service with GPU access |
| GPU | nVidia P40 (sufficient; not Coral) |
