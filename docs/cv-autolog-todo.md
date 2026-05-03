# CV Auto-Log: Outstanding Work

What's left to do across the whole CV auto-log + wall-view initiative,
as of `pump-v0.0.81` / `pump-cv-v0.2.0`. Companion to
[`cv-autolog-plan.md`](cv-autolog-plan.md).

## Hardware install (operator)

- [ ] Buy and wall-mount the touchscreen (recommended: Iiyama ProLite TF3239MSC-B1AG, 32" PCAP open-frame)
- [ ] Wire Cat6 from the K8s switch to the mini-PC behind the screen
- [ ] Install Debian/Ubuntu + Chromium kiosk on the mini-PC, point at `https://pump.<your-domain>/wall/`
- [ ] Mount + power 2× RTSP IP cameras in upper corners of the gym
- [ ] Print checkerboard target (e.g. 9×6 inner corners, 25 mm squares)
- [ ] Update `cv/configs/default.yaml` (or a ConfigMap) with the real RTSP URLs

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
- [ ] **Clip retention policy.** Clips accumulate at ~10 MB each × ~30 sets/day = ~10 GB/month. Add a cron sweep (or systemd timer) that deletes clips older than N days. Lives outside the app.
- [ ] Validate that the 8 s × 10 fps × 640×360 rolling buffer is the right shape in practice. Bigger buffer = clearer replay but more RAM; lower fps = jerkier playback.

## Phase 3 admin panel polish

- [ ] **Live HSV mask preview endpoint.** UI is wired in `admin.html` "Plate HSV" tab; pump-cv side currently a stub (`<div class="admin-stub">`). Need an endpoint that takes a captured frame + HSV ranges and returns the binary mask as PNG.
- [ ] **Per-camera FPS reporting.** `GET /api/v1/cameras` returns `[]` today. Pose source layer needs to track frames-per-second per camera and a connected/disconnected boolean.
- [ ] **Pose-overlay live frame endpoint.** No UI yet. Plan: pump-cv exposes `GET /api/v1/cameras/{name}/latest.jpg` returning the most recent frame with the YOLO skeleton drawn on it. Admin panel shows a refreshing `<img>` per camera.
- [ ] **Snapshot retention policy.** Like clips, snapshots will accumulate. Cron sweep recommended.

## Phase 4 stretch (from plan)

- [ ] Form feedback overlays (depth on squat, bar path on bench, tempo)
- [ ] Velocity-based training estimates from bar speed
- [ ] **Voltra integration spike.** Time-boxed 1–2 weeks. Updated approach based on follow-up research:
  - **No Android app yet** ([confirmed by Beyond Power](https://help.beyond-power.com/en/articles/9932264-android-version-of-beyond)). Means no easy HCI-snoop path; need a hardware BLE sniffer from the start (nRF52840 dongle ~$15, or Ubertooth).
  - **No community RE work** for the Voltra exists in 2026 (no GitHub, no HA integration, no Reddit thread). The voltraco/docs GitHub repo is a different company, not Beyond Power.
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
