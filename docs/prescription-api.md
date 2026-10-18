# Prescription layer — authoring & read API

PUMP's prescription layer is **"coach authors, PUMP presents"**: a coach (a
person, or an external agent) authors a training block through the REST API;
the workout page presents the next session in a rotation **queue** and the
athlete logs sets and reports feedback as usual. PUMP does the presenting — it
does not do the programming, and there is no in-app plan editor.

Data model: a **program** (block/mesocycle) holds **prescribed sessions**; each
committed session holds **prescribed items** (one planned exercise: rep range,
RIR, optional load, rationale, flags, optional substitution). The athlete's
post-session **feedback** (session RPE + a next-day 0–3 soreness/irritation
rating) is keyed by date.

**Queue / rotation model (schema v22):** the athlete trains autoregulated — by
feel, no schedule — so sessions are a **repeating queue in `position` order,
not calendar-dated**. Session `date` is optional (NULL for queue sessions); the
program carries a **`cursor_position`** marking the next-due session. The
workout page serves the committed session at the cursor; completing a session
advances the cursor to the next committed position (wrapping). The committed
sessions of the most-recently-authored program form the rotation; `provisional`
sessions are drafts and never enter the queue.

## Auth

- **Reads are open** — the workout UI and any check-in reader need them.
- **Writes are gated by `PLAN_INGEST_KEY`** (sent as `X-Api-Key`). Empty key =
  open, the in-cluster posture (same as `WEIGHT_INGEST_KEY` / `HEALTH_INGEST_KEY`).
- The **athlete feedback form** (`POST /feedback/`) is a same-origin web form
  and is **not** key-gated — exactly like `/set/` and `/weight/`. Its
  key-gated API twin is `POST /api/session-feedback`.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET`  | `/api/programs` | open | All programs, each with nested sessions + items |
| `POST` | `/api/programs` | key | Create a program → `{"id": N}` |
| `DELETE` | `/api/programs/:id` | key | Delete a program (cascades sessions/items) |
| `POST` | `/api/programs/:id/sessions` | key | Add a prescribed session → `{"id": N}` |
| `PUT`  | `/api/prescribed-sessions/:id/status` | key | Set status `provisional`\|`committed` |
| `PUT`  | `/api/prescribed-sessions/:id/items` | key | Replace a session's items (ordered) |
| `GET`  | `/api/plan/current` | open | The queue view: cursor session (`Current`), the rotation (`Sessions`), and `NextLabel`; **204** when there is no queue |
| `GET`  | `/api/prescription?date=YYYY-MM-DD` | open | Committed session + items for a specific date (default today); **204** when none. Authoring/by-date read; the page uses `/api/plan/current` |
| `GET`  | `/api/session-feedback?since=YYYY-MM-DD` | open | Feedback rows, optional `since` window |
| `POST` | `/api/session-feedback` | key | Append a feedback row |
| `POST` | `/plan/advance/` | same-origin | Advance the queue past the completed session (`session_id` form field) → `{"next_position": N}`. Browser/athlete route, like `/feedback/` |

Only **committed** sessions surface and drive the UI; `provisional` sessions are
still being authored (a draft not yet in the rotation). The page selects the
session at the program's `cursor_position`, not one dated to today; `POST
/plan/advance/` moves the cursor on when a session is finished.

## Worked example — author a block

```bash
BASE=https://pump.example        # your PUMP base URL
KEY=...                          # X-Api-Key; omit the header when PLAN_INGEST_KEY is empty

# 1. Create the program (block). → {"id": 12}
curl -s -XPOST "$BASE/api/programs" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d '{
  "Name": "Block A", "StartDate": "2026-10-20", "LengthWeeks": 4,
  "DeloadWeek": 4, "CreatedBy": "coach", "Notes": "preserve phase"
}'

# 2. Add committed sessions — one per rotation slot, in creation (position)
#    order: Legs, Chest, Back, ...  Date is OPTIONAL in the queue model (omit
#    it). Each → {"id": N}.
curl -s -XPOST "$BASE/api/programs/12/sessions" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d '{
  "Label": "Back", "Status": "committed"
}'

# 3. Set its items. ExerciseID references the exercises catalog. TargetSets is
#    how many working-set rows the "Arnold's Routine" button scaffolds for this
#    item (omit → defaults to 3). TargetWeight / TargetRIR are optional (omit →
#    provisional load, shown as "load TBD", and the button pre-fills that row
#    from the exercise's own default). Flags is a comma-separated tag list;
#    AltExerciseID is a substitution.
curl -s -XPUT "$BASE/api/prescribed-sessions/34/items" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d '[
  {"ExerciseID": 47, "TargetSets": 4, "TargetRepsLow": 10, "TargetRepsHigh": 12, "TargetRIR": 2,
   "TargetWeight": "120", "Rationale": "hold load", "Flags": "grip-offload,gated"},
  {"ExerciseID": 48, "TargetSets": 3, "TargetRepsLow": 10, "TargetRepsHigh": 12,
   "Rationale": "load decided at the session", "Flags": "gated"}
]'

# 4. What the workout page reads: the queue's cursor session + rotation
#    (200 + QueuePlan JSON, or 204 when there is no queue).
curl -s "$BASE/api/plan/current"

# 5. When the athlete finishes a session, the page advances the queue:
curl -s -XPOST "$BASE/plan/advance/" -d "session_id=34"   # → {"next_position": N}
```

A draft session can be added as `"Status": "provisional"` (it stays out of the
rotation) and promoted into the queue with `PUT
/api/prescribed-sessions/:id/status` once its load is decided.

## Feedback

The athlete submits via the workout page's **Feedback** button (`POST
/feedback/`, form fields `rpe` 6–10 and `soreness` 0–3, each with an optional
`*_date`). RPE defaults to the viewed day; the next-day soreness rating is
keyed to the **prior** day, since that signal typically shows up ~24 h later.
A check-in reader pulls the window with `GET /api/session-feedback?since=…`.
