# PUMP — Claude Code Instructions

## Before applying any new version tag

Complete all three steps in order before running `git tag`:

### 1. Regenerate screenshots

Take fresh screenshots of every page and tab at 1280×900 using the running preview server's CDP endpoint (see the Node.js CDP helper pattern established in this repo). Update all images in `assets/`:

- `screenshot-workout.png` — main workout page (`/`)
- `screenshot-stats-overview.png` — Stats › Overview tab (`/stats/`)
- `screenshot-stats-activity.png` — Stats › Weight Moved tab (click `#tab-activity-btn`)
- `screenshot-stats-weight.png` — Stats › Body Weight tab (click `#tab-weight-btn`)
- `screenshot-config.png` — Settings page (`/config/`)

If a tab is added or removed, update this list and the README table to match.

### 2. Audit and update the README

Read `README.md` top to bottom and verify every claim matches the actual codebase:

- Screenshot table rows/labels match the current tabs
- Environment variable table (`HEATCOLOR`, `DISPLAY_DAYS`, `PAGESTEP`, etc.) lists every variable with the correct default
- Architecture diagram reflects the current service layout
- No references to removed features (e.g. deleted tabs, dropped config options)

Fix any stale content before tagging.

### 3. Remove unused code and assets

Before tagging, scan for cruft introduced since the last tag and delete it:

- Unused JavaScript functions in `internal/web/public/js/`
- Unused CSS rules in `internal/web/public/css/`
- Orphaned template files in `internal/web/templates/`
- Stale screenshot files in `assets/` that no longer appear in the README
- Dead Go code (unreachable handlers, unused struct fields, removed config keys)

Commit the cleanup in the same PR as the screenshot/README updates, before pushing the tag.

## Database schema rules

### Schema is always the source of truth

Every table, column, and index that the Go code reads or writes must be defined in a migration in `internal/db/postgres.go`. There must be no ad-hoc `CREATE TABLE` or `ALTER TABLE` calls outside of `pgMigrations`. If a query references a column that no migration creates, add a migration before merging.

### How to add or change the schema

1. Append a new entry to `pgMigrations` in `internal/db/postgres.go`. **Never edit or delete an existing entry** — the migration runner skips versions already recorded in `schema_version`, so modifying a past entry has no effect on existing databases and will cause drift.
2. Give the new entry the next sequential `Version` integer.
3. Write the migration as idempotent SQL where practical (`IF EXISTS`, `IF NOT EXISTS`, `DO $$ … IF NOT EXISTS $$`).
4. Update the Go model in `internal/models/models.go` and any store methods in `internal/store/` to match the new schema.
5. Verify the migration runs cleanly from a fresh database and from an existing v(N-1) database.

### Schema changes require a version bump

Any PR that adds, removes, or renames a table or column must also increment the patch version (e.g. `v0.0.9` → `v0.0.10`). Document the schema change in the commit message.

### Migration checklist before tagging

- [ ] All columns referenced in `internal/store/postgres.go` and `internal/store/apiclient.go` exist in a migration
- [ ] No migration entry has been modified after it was merged
- [ ] `schema_version` table will contain a row for every migration after a fresh run
- [ ] `models.Conf` and `models.Set` / `models.Exercise` / `models.BodyWeight` match the current schema
