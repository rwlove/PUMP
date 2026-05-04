# CV Auto-Log: Outstanding Work

What's left to do across the whole CV auto-log + wall-view initiative,
as of `pump-v0.0.82` / `pump-cv-v0.3.0`. Companion to
[`cv-autolog-plan.md`](cv-autolog-plan.md).

---

## ★ Resume here ★

**State of the world (last touched 2026-05-04):**

- Software is fully shipped through phases 0, 1, 2, and a phase-3 admin scaffold. Latest tags: `pump-v0.0.82` and `pump-cv-v0.3.0`, both published to GHCR.
- Wall view (`/wall/`) and admin panel (`/admin/`) are live and verified at the wire level. They render in any browser; touchscreen is convenience, not a dependency.
- Demo CLI (`python -m pump_cv.scripts.demo`) lets you exercise the wall view + admin panel against synthetic CV writes today, without cameras.
- Camera install plan is finalized (right-wall placement, both upper corners, side-on profile views).
- **Cameras have been purchased: 2× Reolink CX810 (4K ColorX bullet, F1.0, PoE).** Awaiting delivery.
- **Touchscreen NOT yet ordered:** Iiyama ProLite TF3239MSC-B1AG (32") plus a VESA full-motion mount (e.g. ECHOGEAR EGMF2 or VideoSecu MW365B2H). ~$1100 + $40.
- **Mini-PC, PoE switch, Cat6 supply already on hand.**
- **Foamcore + chessboard PDF for calibration:** ~$5; print at home from Mark Hedley Jones's [free PDF generator](https://markhedleyjones.com/projects/calibration-checkerboard-collection).

**Next physical steps when cameras arrive:**

1. Power up each camera one at a time on the PoE switch; find IP via DHCP table; web UI to disable P2P/cloud/auto-update; set strong admin password.
2. Verify RTSP streams: `ffplay rtsp://USER:PASS@IP:554/h264Preview_01_main` from any machine.
3. Mount cameras temporarily with **3M VHB tape** in the front-right and back-right upper corners of the right wall (the long wall opposite the touchscreen). Both angled at room center, tilted down ~25-35°.
4. Run Cat6 from each camera to the PoE switch.
5. Aim cameras while watching live feed: stand at squat rack and bench positions, verify head-to-toe coverage in side profile.
6. Print + assemble chessboard, capture ~20 photos per camera (intrinsics) and ~20 paired shots (stereo extrinsics). Run `python -m pump_cv.calibration intrinsics …` and `… stereo …`.
7. Update `cv/configs/default.yaml` (or K8s ConfigMap) with the RTSP URLs + calibration paths.
8. Deploy `pump-cv` to the K8s cluster (Flux will roll the latest tag).
9. Toggle `CVAUTOLOG=true` in PUMP config UI.
10. Tune via `/admin/` → Thresholds tab.
11. After a week of confirmed-good placement, swap VHB tape for drywall screws.

**Room layout (10 × 12 ft, 8 ft ceiling):**

- Front wall: 10 ft (unused TV mounted high, 30 % from left edge — off, doesn't matter)
- Back wall: 10 ft (squat rack against this wall)
- Left wall: 12 ft (touchscreen will mount here)
- Right wall: 12 ft (cameras: front-right + back-right upper corners)
- Athlete faces back wall during squats; faces front wall during bench

**Settled design decisions** (ratified through conversation, no changes pending):

| Decision | Choice |
|---|---|
| Camera count | 2 |
| Camera model | Reolink CX810 (4K ColorX bullet PoE) |
| Camera placement | Right wall, both upper corners, ~62-78° angular separation, side-on profile views |
| Touchscreen size | 32" (TF3239MSC-B1AG); 4K not needed at 3-4 ft viewing distance |
| Touchscreen mount | VESA 200×200 full-motion (ECHOGEAR or VideoSecu) |
| Wall view layout | Data left ~40%, looping clip right ~60%, current set highlighted |
| Per-set camera selection | Highest mean keypoint confidence (deferred — Phase 2 polish) |
| Idle behavior | Motion-trigger sleep/wake from pump-cv |
| Notifications | Pushover (env-only credentials in PUMP, never in pump-cv) |
| Image distribution | `pump-v*` and `pump-cv-v*` separate tag lines, `linux/amd64` only |
| Local-only ops | Cameras on no-WAN VLAN once final-mounted |
| Admin panel scope | Feature-rich (pose overlay, HSV sliders, prototype mgr — staged across phases) |
| Form critique | Aspirational long-term goal driving display + camera selection |

---

## Hardware install (operator)

- [x] Decide on touchscreen model and size (Iiyama TF3239MSC-B1AG, 32")
- [x] Decide on camera model and count (2× Reolink CX810)
- [x] Decide on camera placement (right wall upper corners)
- [x] Purchase cameras
- [ ] Order touchscreen (Iiyama TF3239MSC-B1AG, ~$1100) + VESA mount
- [ ] Print + assemble chessboard target on rigid foamcore
- [ ] Mount cameras (start with 3M VHB tape, swap to drywall screws once placement confirmed)
- [ ] Run Cat6 from each camera to existing PoE switch
- [ ] Configure cameras via web UI: change passwords, disable P2P/cloud, set RTSP
- [ ] Set up mini-PC (Debian/Ubuntu + Chromium kiosk pointed at `https://pump.<your-domain>/wall/`)
- [ ] Wall-mount the touchscreen + connect mini-PC via HDMI + USB
- [ ] Network-isolate cameras on a no-WAN VLAN (firewall rule or separate VLAN)

## Phase 1 follow-through (needs cameras to verify)

- [ ] Smoke-test the YOLOv8 wrapper against a real RTSP frame on the P40
- [ ] Run `python -m pump_cv.calibration intrinsics --images …` against actual chessboard photos for each camera
- [ ] Run `python -m pump_cv.calibration stereo --left … --right …` for the camera-pair extrinsics
- [ ] Capture intrinsics/extrinsics .npz files; reference them from `cv/configs/default.yaml`
- [ ] Run `python -m pump_cv.scripts.benchmark` against a sample clip to confirm achieved FPS on the P40
- [ ] Tune `rep.min_amplitude_deg`, `rep.min_period_s`, `set_boundary.quiet_seconds`, and `confidence_threshold` against your actual gym lighting + form. Use the admin-panel sliders.
- [ ] Record reference clips through the `/exercise/:id/reference` UI for each PUMP exercise you do regularly
- [ ] Verify wake/sleep signal in practice (athlete enters → wall wakes; absent for 10 min → wall sleeps)

## Phase 2 polish (clip capture)

- [ ] **Best-camera-per-set selection.** Currently `FusedPoseSource` always yields cam-A's frame, so the clip is always cam-A. Pick the camera with the higher mean per-keypoint confidence over the captured set. Requires the source to keep both per-camera frame buffers and expose the confidence summary at set close.
- [x] **Clip retention policy.** Implemented as `pump_cv.retention` (background asyncio task, default 30 d). Validate after a few weeks of real use.
- [ ] Validate that the 8 s × 10 fps × 640×360 rolling buffer is the right shape in practice. Bigger buffer = clearer replay but more RAM; lower fps = jerkier playback.

## Phase 3 admin panel polish

- [x] **Live HSV mask preview endpoint** — implemented; admin "Plate HSV" tab is functional
- [x] **Per-camera FPS reporting** — `GET /api/v1/cameras` returns real data; admin "Cameras" tab live-polls
- [x] **Snapshot retention** — covered by `pump_cv.retention`
- [x] **Snapshot filters** — `GET /api/v1/snapshots?since=YYYY-MM-DD&limit=N`
- [ ] **Pose-overlay live frame endpoint.** No UI yet. Plan: pump-cv exposes `GET /api/v1/cameras/{name}/latest.jpg` returning the most recent frame with the YOLO skeleton drawn on it. Admin panel shows a refreshing `<img>` per camera.
- [ ] Hot-reload prototypes on disk change (currently they load only at sidecar startup; a new upload requires a rolling restart to be picked up)

## Phase 4 stretch (from plan)

- [ ] Form feedback overlays (depth on squat, bar path on bench, tempo)
- [ ] Velocity-based training estimates from bar speed
- [ ] **Voltra integration spike.** Time-boxed 1–2 weeks. Approach:
  - **No Android app yet** ([confirmed by Beyond Power](https://help.beyond-power.com/en/articles/9932264-android-version-of-beyond)). Means no easy HCI-snoop path; need a hardware BLE sniffer from the start (nRF52840 dongle ~$15, or Ubertooth).
  - **No community RE work** for the Voltra exists in 2026 (no GitHub, no HA integration, no Reddit thread). The voltraco/docs GitHub repo is a different company.
  - **Voltra has WiFi for firmware updates** — worth scanning the local network for an mDNS service or HTTP endpoint *before* doing BLE work; might be a quick win.
  - **Recommended order**: (a) `nmap` and `avahi-browse` the device's IP for HTTP/mDNS first; (b) email Beyond Power support to ask about a BLE protocol document for non-commercial integration; (c) only if both fail, do hardware BLE sniffing with the iOS app paired to the device.
  - Decode at minimum: current resistance value + rep events.
  - If successful, swap the Voltra branch from "opaque + manual confirm" to "BLE/HTTP subscriber + automatic logging."

## Operations + docs

- [ ] Write Kubernetes manifests for `pump-cv` Deployment + ConfigMap + Secret + PVC (snapshots/clips/prototypes share volume). User-managed per agreement.
- [ ] Set up Flux `ImageRepository` + `ImagePolicy` + `ImageUpdateAutomation` for `ghcr.io/rwlove/pump-cv` so it auto-rolls on tag.
- [ ] Refresh README screenshots once `/wall/` and `/admin/` are live in the running cluster (CLAUDE.md step 1)
- [ ] Move RTSP credentials out of the yaml config and into a `Secret`-derived env var when cameras start using auth

## Decisions not yet made

- Which exercise-name prefix triggers Voltra-opaque mode? Currently any name containing "voltra" (case-insensitive). Could add an explicit `equipment_type: voltra` field on the `exercises` table if naming gets unwieldy.
- Should there be an emergency "pause CV writes" button on the admin panel? Useful when CV is misbehaving badly mid-workout. Toggling `CVAutoLog` off in config achieves the same end via /config/.
- Should the wall view show ONLY today's sets, or also the most recent 1–2 sets from the previous workout day for context?

## Reference: full purchase list

| # | Item | Source | Approx | Status |
|---|---|---|---|---|
| 1 | Iiyama ProLite TF3239MSC-B1AG (32" PCAP) | [Assured Systems](https://www.assured-systems.com/iiyama-prolite-tf3239msc-b1ag-32-12pt-open-frame-pcap-interactive-large-format-display/) | ~$1100 | Not ordered |
| 2 | 2× Reolink CX810 (4K ColorX PoE) | [Amazon](https://www.amazon.com/REOLINK-CX810-Technology-Detection-Spotlight/dp/B0CX1QCQNX) | ~$200 | **Purchased** |
| 3 | VESA 200×200 full-motion mount | [VideoSecu MW365B2H](https://www.videosecu.com/mw365b2/) or [ECHOGEAR EGMF2](https://www.echogear.com/tv-mounts/full-motion-tv-wall-mount-for-32-60-tvs-egmf2/) | $30-80 | Not ordered |
| 4 | Cat6 patch cables | Any | $10-25 | On hand |
| 5 | Foamcore + chessboard PDF | Craft store + [free generator](https://markhedleyjones.com/projects/calibration-checkerboard-collection) | $5 | Not bought |
| 6 | 3M VHB heavy-duty mounting tape (temporary camera mount) | [Amazon](https://www.amazon.com/3M-VHB-Heavy-Duty-Mounting/dp/B07KSRZWP1) | $8 | Not bought |
| 7 | (have) Mini-PC for kiosk | — | — | On hand |
| 8 | (have) PoE switch | — | — | On hand |
