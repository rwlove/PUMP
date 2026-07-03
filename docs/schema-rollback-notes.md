# Schema Rollback Notes

Reference for operators who need to roll a PUMP deploy back to an earlier
image tag. PUMP's migration runner (`internal/db/postgres.go`,
`MigratePostgres`) is **forward-only** — there is no reverse-migration
tooling and no test coverage for downgrades. Every migration must be
authored so that "downgrade the image, don't touch the DB" is a safe
operation, and every rollback decision must know which migrations have
already applied and whether the older image tolerates them.

## Rule of thumb

- **Additive changes** (new tables, new columns with defaults, new
  indexes): safe. Older code ignores what it doesn't know about.
- **Destructive changes** (`DROP COLUMN`, `ALTER TYPE` narrowing, `DROP
  TABLE`, `NOT NULL` without default, data migrations that mangle
  existing rows): **not rollback-safe**. Downgrading the image will fail
  reads/writes because the schema no longer matches what older code
  expects.

If a future PR adds a destructive migration, it must include a note in
this file explaining the downgrade impact so an operator considering
rollback knows.

## Per-migration safety

| v | Description | Rollback-safe? | Notes |
|--:|---|:---:|---|
| 1 | Create `exercises`, `sets`, `weight` | n/a | Nothing to roll back to. |
| 2 | Drop `intensity` columns from `exercises` and `sets` | ❌ | An image predating v2 expects those columns. Rolling back requires re-adding the columns manually. |
| 3 | Add `sets.note` (default '') | ✅ | Additive. Older code ignores it. |
| 4 | Add `sets.source`, `sets.confidence`, `sets.pending` (defaults) | ✅ | Additive. |
| 5 | Widen `sets.confidence` REAL → DOUBLE PRECISION | ✅ | Older Go code reads it as `float64` either way; a REAL fits in DOUBLE PRECISION. |
| 6 | Add `sets.clip_path` (default '') | ✅ | Additive. |
| 7 | Add `weight.recorded_at` and `weight_date_recorded_at_idx` | ⚠️ | Column is now `NOT NULL DEFAULT NOW()`. Older code (pre-v0.0.72) doesn't set it on INSERT — but the default makes writes succeed. Older code that did `SELECT *` may not tolerate the extra column. Prefer downgrading to v0.0.72 or later. |
| 8 | Create `health_record` table + `health_record_type_time_idx` | ✅ | Additive table; older code ignores it. Health ingest breaks (endpoint disappears in older code) but the table just accumulates nothing. |
| 9 | Add `sets_date_idx`, `sets_name_date_idx`, `health_record_start_time_idx` | ✅ | Additive indexes; older code just doesn't use them. Storage cost only. |
| 10 | Create `app_config` table (single-row settings persistence) | ✅ | Additive table; older code (v0.0.102 and earlier) never reads it. See "Behavior surprise" below. |

## Rollback playbook

1. **Decide the target image tag.** Consult the table above. If the
   target predates any ❌ migration currently applied on the DB, do not
   proceed without a manual schema fix — you will crash-loop the older
   image.
2. **Bump the home-ops helmrelease** back to the previous digest (edit
   `kubernetes/apps/collab/pump/app/helmrelease.yaml`, change `tag:` to
   the older `v0.0.NN@sha256:...` pair, commit, PR, Flux reconciles).
3. **Do NOT try to reverse-run migrations.** Leave the DB at its current
   `schema_version`. The extra tables/columns/indexes are inert to the
   older code.
4. **Verify the rollback:** the pod's startup log will show
   `db migrations: schema is up to date` (the older image sees no
   pending migrations because its `pgMigrations` slice is shorter than
   what's in `schema_version`).

## Behavior surprise on v10→v0.0.102 rollback

Rolling back from v0.0.103+ to v0.0.102 or earlier is schema-safe but
carries one visible behavior change: settings you saved via `/config/`
(color, page-step, frequency days, display days, autofill, CV auto-log)
will silently revert to the env-var defaults on the older image, because
pre-v0.0.103 code doesn't know about the `app_config` table. When you
re-deploy v0.0.103+ the persisted values come back — the DB row is not
lost, just ignored by the older image.

If you need those settings honored during a rollback window, set them via
env vars on the helmrelease (`COLOR`, `PAGESTEP`, `FREQUENCY_DAYS`,
`DISPLAY_DAYS`, `AUTOFILL`, `CVAUTOLOG`) for the duration.

## Verified against these PRs

- v9 (indexes): PR #51, tagged pump-v0.0.102 — index-only, tested v8→v9
  against a prod dump, row counts unchanged, `EXPLAIN` shows the indexes
  in use.
- v10 (app_config): PR #63, tagged pump-v0.0.103 — additive table, tested
  v9→v10 against a prod dump, row counts unchanged, config round-trip
  verified.
