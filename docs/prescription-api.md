# Prescription layer — authoring & read API

PUMP's prescription layer is **"coach authors, PUMP presents"**: a coach (a
person, or an external agent) authors a training block through the REST API;
the workout page presents today's committed session and the athlete logs sets
and reports feedback as usual. PUMP does the presenting — it does not do the
programming, and there is no in-app plan editor.

Data model (schema v20): a **program** (block/mesocycle) holds dated
**prescribed sessions**; each committed session holds **prescribed items**
(one planned exercise: rep range, RIR, optional load, rationale, flags,
optional substitution). The athlete's post-session **feedback** (session RPE +
a next-day 0–3 soreness/irritation rating) is keyed by date.

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
| `GET`  | `/api/prescription?date=YYYY-MM-DD` | open | Committed session + items for a date (default today); **204** on a freestyle day |
| `GET`  | `/api/session-feedback?since=YYYY-MM-DD` | open | Feedback rows, optional `since` window |
| `POST` | `/api/session-feedback` | key | Append a feedback row |

Only **committed** sessions surface in `GET /api/prescription` and drive the
UI; `provisional` sessions are still being authored (the one-exposure-ahead
leash: materialize the next session concretely, leave later ones provisional).

## Worked example — author a block

```bash
BASE=https://pump.example        # your PUMP base URL
KEY=...                          # X-Api-Key; omit the header when PLAN_INGEST_KEY is empty

# 1. Create the program (block). → {"id": 12}
curl -s -XPOST "$BASE/api/programs" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d '{
  "Name": "Block A", "StartDate": "2026-10-20", "LengthWeeks": 4,
  "DeloadWeek": 4, "CreatedBy": "coach", "Notes": "preserve phase"
}'

# 2. Add a committed session for a training day. → {"id": 34}
curl -s -XPOST "$BASE/api/programs/12/sessions" -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' -d '{
  "Date": "2026-10-21", "Label": "Back", "Status": "committed"
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

# 4. What the workout page reads for today (200 + session JSON, or 204 freestyle).
curl -s "$BASE/api/prescription?date=2026-10-21"
```

A later session can be added as `"Status": "provisional"` and promoted with
`PUT /api/prescribed-sessions/:id/status` once its load is decided.

## Feedback

The athlete submits via the workout page's **Feedback** button (`POST
/feedback/`, form fields `rpe` 6–10 and `soreness` 0–3, each with an optional
`*_date`). RPE defaults to the viewed day; the next-day soreness rating is
keyed to the **prior** day, since that signal typically shows up ~24 h later.
A check-in reader pulls the window with `GET /api/session-feedback?since=…`.
