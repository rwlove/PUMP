# AGENTS.md — PUMP

Instructions for AI agents working in this repository.

**Why this file exists.** opencode reads `AGENTS.md` from the repository root
and nothing else — it does not read `CLAUDE.md`, and it does not expand the
`@`-import syntax Claude Code uses. Without this file an agent working here has
no repository context at all.

`CLAUDE.md` imports this file, so both tools share one source of truth.


---

## Repository guide

### Role / framing

You are a contributor to PUMP, a self-hosted workout tracker (Go server +
Python CV sidecar) deployed as OCI images to the HomeLab cluster. The primary
user is Rob; the household user may use the tracking UI. Changes here ship directly to the
personal deployment via image tags.

Within that role, the user makes the final call on every tag and schema
migration. Claude advises and drafts; the user steers.

### Prime directive

**Schema migrations are irreversible once deployed.** Never edit a past entry
in `pgMigrations`. Always append a new migration. The live database is the
authoritative runtime state; Git defines the intended schema evolution.

Any PR that adds, removes, or renames a table or column requires a patch
version bump on the `pump-vX.Y.Z` tag line.


### Logo changes

The logo lives in two canonical locations:

- `internal/web/public/logo.svg` — served by the web UI at `/fs/public/logo.svg`
- `assets/logo.svg` — used by the README

Whenever the logo SVG is changed, **all three of the following must be updated in the same commit**:

1. **`internal/web/public/logo.svg`** and **`assets/logo.svg`** — keep them identical.
2. **`internal/web/public/favicon.svg`** — rebuild as a compact square (64×64 viewBox) version
   of the new logo: same shapes, same colors, dark rounded background, "PUMP" text at the
   bottom with the raised-P style. Update `favicon.png` too if tooling is available.
3. **`README.md`** — the `<img src="assets/logo.svg">` tag already references the file; verify
   it still renders correctly after any viewBox or size change.

### Release tag conventions

Three independent tag lines, each driving its own CI:

- `pump-vX.Y.Z` → publishes `ghcr.io/rwlove/pump:vX.Y.Z` (the Go server image)
- `pump-cv-vA.B.C` → publishes `ghcr.io/rwlove/pump-cv:vA.B.C` (the camera sidecar image)
- `pump-voltra-vA.B.C` → publishes `ghcr.io/rwlove/pump-voltra:vA.B.C` (the Voltra trainer sidecar image)

The `pump-` / `pump-cv-` / `pump-voltra-` prefixes are stripped before tagging
the image in GHCR, so users still pull `pump:v0.0.80` even though the git tag is
`pump-v0.0.80`. Legacy `v0.0.X` tags (≤ v0.0.79) pre-date the split and
remain as historical markers — they no longer trigger any CI.

**Adding a fourth tag line?** Check its prefix against the existing trigger
globs first. `pump-v*` matches `pump-voltra-v0.1.0` — `*` absorbs the
`oltra-v0.1.0` — so tagging the sidecar on 2026-08-04 also fired the server
build and published a spurious `pump:voltra-v0.1.0`. The server trigger is now
`pump-v[0-9]*`. Any new `pump-<name>-v*` line must not be glob-swallowed by an
existing one.

### Before applying any new pump-v* tag

Complete all steps in order before running `git tag`:

#### 1. Regenerate screenshots

Take fresh screenshots of every page and tab at 1280×900 using the running preview server's CDP endpoint (see the Node.js CDP helper pattern established in this repo). Update all images in `assets/` — the authoritative set is whatever the README's screenshot table embeds; currently:

- `screenshot-workout.png` — main workout page (`/`)
- `screenshot-health.png` — Overall Health dashboard (`/health/`)
- `screenshot-config.png` — Settings page (`/config/`)
- `screenshot-stats-overview.png` — Stats › Exercise Distribution tab (`/stats/`)
- `screenshot-stats-activity.png` — Stats › Weight Moved tab (click `#tab-activity-btn`)
- `screenshot-stats-weight.png` — Stats › Body Weight tab (click `#tab-weight-btn`)
- `screenshot-stats-prs.png` — Stats › Personal Records tab (click `#tab-prs-btn`)
- `screenshot-stats-overload.png` — Stats › Progressive Overload tab (click `#tab-overload-btn`)
- `screenshot-stats-balance.png` — Stats › Muscle Balance tab (click `#tab-balance-btn`)
- `screenshot-stats-consistency.png` — Stats › Consistency tab (click `#tab-consistency-btn`)
- `screenshot-stats-recovery.png` — Stats › Recovery tab (click `#tab-recovery-btn`)
- `screenshot-stats-steps.png` — Stats › Steps tab (click `#tab-steps-btn`)
- `screenshot-stats-hr.png` — Stats › Heart Rate tab (click `#tab-hr-btn`)
- `screenshot-stats-sleep.png` — Stats › Sleep tab (click `#tab-sleep-btn`)
- `screenshot-stats-cardio.png` — Stats › Cardio tab (click `#tab-cardio-btn`)

Shoot against a clean restore of the production dump — never against a database that test writes have touched. If a tab is added or removed, update this list and the README table to match.

**Verify the list against the UI, don't trust it.** Enumerate the tabs from the
template rather than assuming this list is current:

```
grep -o 'id="tab-[a-z]*-btn"' internal/web/templates/stats.html | sort -u
```

Four tabs (Personal Records, Progressive Overload, Consistency, Recovery)
shipped in May 2026 and went three releases without a screenshot because the
list was treated as authoritative.

**Shoot the workout page on a day that has sets.** It defaults to today, which
is usually empty and makes a poor first impression in the README. Set
`sessionStorage["today"]` to a recent training day before capturing, and pick
the day from the data:

```sql
SELECT date, count(*) FROM sets GROUP BY date ORDER BY date DESC LIMIT 5;
```

#### 2. Audit and update the README

Read `README.md` top to bottom and verify every claim matches the actual codebase:

- Screenshot table rows/labels match the current tabs
- Environment variable table (`DISPLAY_DAYS`, `PAGESTEP`, `AUTOFILL`, etc.) lists every variable with the correct default. Diff it against the code rather than reading it — `grep -rohE 'Getenv\("[A-Z_]+"\)|env[A-Za-z]*\("[A-Z_]+"' --include=*.go internal/ cmd/` for the server, and `config.py` for each sidecar. `VOLTRA_MAX_LOAD_LB` shipped undocumented this way.
- Architecture diagram reflects the current service layout
- No references to removed features (e.g. deleted tabs, dropped config options)

Fix any stale content before tagging.

#### 3. Remove unused code and assets

Before tagging, scan for cruft introduced since the last tag and delete it:

- Unused JavaScript functions in `internal/web/public/js/`
- Unused CSS rules in `internal/web/public/css/`
- Orphaned template files in `internal/web/templates/`
- Stale screenshot files in `assets/` that no longer appear in the README
- Dead Go code (unreachable handlers, unused struct fields, removed config keys)

Commit the cleanup in the same PR as the screenshot/README updates, before pushing the tag.

### Database schema rules

#### Schema is always the source of truth

Every table, column, and index that the Go code reads or writes must be defined in a migration in `internal/db/postgres.go`. There must be no ad-hoc `CREATE TABLE` or `ALTER TABLE` calls outside of `pgMigrations`. If a query references a column that no migration creates, add a migration before merging.

#### How to add or change the schema

1. Append a new entry to `pgMigrations` in `internal/db/postgres.go`. **Never edit or delete an existing entry** — the migration runner skips versions already recorded in `schema_version`, so modifying a past entry has no effect on existing databases and will cause drift.
2. Give the new entry the next sequential `Version` integer.
3. Write the migration as idempotent SQL where practical (`IF EXISTS`, `IF NOT EXISTS`, `DO $$ … IF NOT EXISTS $$`).
4. Update the Go model in `internal/models/models.go` and any store methods in `internal/store/` to match the new schema.
5. Verify the migration runs cleanly from a fresh database and from an existing v(N-1) database.

#### Schema changes require a version bump

Any PR that adds, removes, or renames a table or column must also increment the patch version on the pump tag line (e.g. `pump-v0.0.80` → `pump-v0.0.81`). Document the schema change in the commit message.

#### Migration checklist before tagging

- [ ] All columns referenced in `internal/store/postgres.go` exist in a migration
- [ ] No migration entry has been modified after it was merged
- [ ] `schema_version` table will contain a row for every migration after a fresh run
- [ ] `models.Conf` and `models.Set` / `models.Exercise` / `models.BodyWeight` match the current schema

#### Multi-version upgrade safety

The migration runner (`internal/db/postgres.go` — `MigratePostgres`) iterates the full `pgMigrations` slice in ascending version order and applies every version not yet recorded in `schema_version`. This means a database at **any past version** (e.g. v1) is automatically and safely brought to the latest version (e.g. v5) by running the server once — no manual intervention required.

**Before every PR that introduces schema changes you must verify:**

1. **Gap-free sequence.** Version numbers in `pgMigrations` must be consecutive integers starting at 1. Never skip a number. A gap causes the runner to silently skip all versions above the gap.

2. **Fresh-database path.** Start with a completely empty PostgreSQL database and run the server. Confirm all migrations apply cleanly, in order, with no errors. Check `schema_version` contains one row per migration.

3. **Multi-hop upgrade path.** Test every likely "cold start" scenario a real user might hit:
   - **v1 → latest**: user who deployed at launch and never updated.
   - **v(latest-1) → latest**: user on the previous release.
   - Any intermediate version you know is widely deployed.

   Procedure for each:
   ```
   # Restore a dump from that version, then:
   docker run --rm -e POSTGRES_DSN=... ghcr.io/rwlove/pump:<new-image-tag>
   # (image tag is the prefix-stripped form, e.g. v0.0.80, NOT pump-v0.0.80)
   # Inspect logs: every pending migration should appear as "applying vN"
   # Verify the app works normally; verify no data was lost
   ```

4. **Rollback awareness.** PUMP does not support automatic rollbacks. If a migration fails mid-run, the database is left at the last successfully committed version and the server exits non-zero. The fix is to correct the migration SQL and redeploy — never delete or modify the failed entry; append a corrective migration instead.

5. **Idempotency.** Every DDL statement in a migration must be safe to re-run (use `IF EXISTS` / `IF NOT EXISTS`). This protects against partially-applied states caused by transient network failures.

6. **No data loss.** Any migration that drops a column or table must either be preceded by a migration that migrates the data elsewhere, or there must be explicit sign-off in the PR that the data is intentionally discarded (e.g. removing a feature entirely).
