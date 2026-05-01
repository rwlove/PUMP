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
