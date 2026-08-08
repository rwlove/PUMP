// PUMP wall view — kiosk dashboard JS.
//
// Subscribes to two SSE streams:
//   /api/sets/stream  — set lifecycle (add/update/delete/bulk)
//   /api/wall/stream  — wake / sleep / build_changed
//
// Renders today's sets in a big-typography list, current set on top.
// Pending sets get tap-to-confirm and tap-to-reject buttons.
// On build_changed mismatch, self-reload to pick up the new code.

(function() {
  'use strict';

  // Today, in the page's local timezone. Deliberately not toISOString(),
  // which is UTC: in Eastern that rolls over at 20:00 and the kiosk spends
  // every evening session filtering on tomorrow's date, showing "No sets yet
  // today". todayStr() also prefers the server's date over the browser clock.
  //
  // Called at render time rather than captured at load, so a kiosk left up
  // overnight rolls over instead of sticking on yesterday's date forever.

  const els = {
    date:     document.getElementById('wallDate'),
    clock:    document.getElementById('wallClock'),
    sets:     document.getElementById('wallSets'),
    cams:     document.getElementById('wallCams'),
    status:   document.getElementById('wallStatus'),
    statusLabel: document.getElementById('wallStatusLabel'),
    sleep:    document.querySelector('.wall-sleep'),
    clipModal: document.getElementById('wallClipModal'),
    clipBody:  document.getElementById('wallClipBody'),
    clipClose: document.getElementById('wallClipClose'),
  };

  // ─── Navbar height → CSS var ────────────────────────────────────────
  // The shared navbar (header.html) sits above the wall grid. Its real
  // height depends on the logo size and font metrics, so measure it and
  // publish it as --wall-navbar-h. Both the grid's height calc and the
  // calibration overlay's top offset read this var; without it they fall
  // back to a 56px guess that's ~25px short of the actual navbar.
  function measureNavbar() {
    const nav = document.querySelector('.navbar');
    if (!nav) return;
    const h = Math.round(nav.getBoundingClientRect().height);
    if (h > 0) document.documentElement.style.setProperty('--wall-navbar-h', h + 'px');
  }
  measureNavbar();
  window.addEventListener('resize', measureNavbar, { passive: true });

  // ─── Header date + clock ────────────────────────────────────────────
  function renderHeader() {
    const now = new Date();
    els.date.textContent = now.toLocaleDateString(undefined, {
      weekday: 'long', month: 'long', day: 'numeric',
    });
    els.clock.textContent = now.toLocaleTimeString(undefined, {
      hour: 'numeric', minute: '2-digit',
    });
  }
  renderHeader();
  setInterval(renderHeader, 30 * 1000);

  // ─── Sets state + render ────────────────────────────────────────────
  // Keep a local Map<id, Set> so SSE updates can patch in place.
  const sets = new Map();

  function renderSets() {
    const todays = Array.from(sets.values())
      .filter(s => s.Date === todayStr())
      .sort((a, b) => b.ID - a.ID); // newest first

    if (todays.length === 0) {
      els.sets.innerHTML = '<div class="wall-empty">No sets yet today.</div>';
      return;
    }

    // Wrap contiguous same-exercise runs in a .wall-set-group container
    // so the group owns the per-exercise color stripe (one continuous
    // ::before painted along its full height) instead of each row
    // painting its own. Single-row groups still get wrapped — keeps
    // visuals uniform.
    let html = '';
    let i = 0;
    while (i < todays.length) {
      const groupName = todays[i].Name;
      const groupColor = todays[i].WorkoutColor || todays[i].Color || '#6c757d';
      let j = i + 1;
      while (j < todays.length && todays[j].Name === groupName) j++;
      const rows = todays.slice(i, j)
        .map((s, k) => renderSetCard(s, i + k === 0))
        .join('');
      html += `<div class="wall-set-group" style="--group-color: ${groupColor}">${rows}</div>`;
      i = j;
    }
    els.sets.innerHTML = html;

    // Wire row taps → open clip modal. Pending-set confirm/reject
    // buttons stop propagation below so they don't double-trigger.
    els.sets.querySelectorAll('.wall-set').forEach(node => {
      node.addEventListener('click', (ev) => {
        // Skip if the user tapped one of the action buttons.
        if (ev.target.closest('.wall-set-btn')) return;
        const id = parseInt(node.dataset.setId, 10);
        const s = sets.get(id);
        if (s) openClipModal(s);
      });
    });

    // Wire confirm/reject buttons.
    els.sets.querySelectorAll('.wall-set').forEach(node => {
      const id = parseInt(node.dataset.setId, 10);
      const confirmBtn = node.querySelector('.wall-set-btn.confirm');
      const rejectBtn  = node.querySelector('.wall-set-btn.reject');
      if (confirmBtn) confirmBtn.addEventListener('click', () => confirmSet(id));
      if (rejectBtn)  rejectBtn.addEventListener('click',  () => rejectSet(id));
    });
  }

  // ─── Live camera previews ───────────────────────────────────────────
  // Fetches the camera list from pump-cv (via pump's /api/cv/* proxy)
  // and renders one tile per camera with a per-camera on/off toggle.
  // When on, the tile polls the snapshot endpoint every CAM_POLL_MS
  // and swaps the <img> src. Toggle state persists in localStorage so
  // the kiosk remembers across reloads. The capture loop in pump-cv
  // runs regardless of toggle state — this is purely a viewer toggle.
  const CAM_POLL_MS = 1000;
  const CAM_LS_KEY  = 'pump.wall.cams.enabled';
  const camTimers   = new Map();   // name → setInterval handle
  let camsLoaded    = false;

  function loadCamPrefs() {
    try {
      return new Set(JSON.parse(localStorage.getItem(CAM_LS_KEY) || '[]'));
    } catch (_) { return new Set(); }
  }
  function saveCamPrefs(set) {
    try { localStorage.setItem(CAM_LS_KEY, JSON.stringify(Array.from(set))); }
    catch (_) { /* private mode etc — ignore */ }
  }

  async function loadCameras() {
    if (camsLoaded) return;
    try {
      const r = await fetch('/api/cv/api/v1/cameras');
      if (!r.ok) throw new Error('HTTP ' + r.status);
      const cams = await r.json();
      renderCams(cams || []);
      camsLoaded = true;
    } catch (e) {
      console.warn('wall: camera list unavailable', e);
      els.cams.innerHTML =
        '<div class="wall-video-placeholder">' +
        '<div class="wall-video-msg">Cameras unavailable<br><small>pump-cv unreachable</small></div>' +
        '</div>';
    }
  }

  function renderCams(cams) {
    if (!cams.length) {
      els.cams.innerHTML =
        '<div class="wall-video-placeholder">' +
        '<div class="wall-video-msg">No cameras configured</div>' +
        '</div>';
      return;
    }
    const enabled = loadCamPrefs();
    els.cams.innerHTML = cams.map(c => `
      <div class="wall-cam" data-cam="${escapeHtml(c.name)}">
        <div class="wall-cam-header">
          <span class="wall-cam-name">${escapeHtml(c.name)}</span>
          <label class="wall-cam-toggle">
            <input type="checkbox" aria-label="Show ${escapeHtml(c.name)} camera" ${enabled.has(c.name) ? 'checked' : ''}>
            <span class="wall-cam-toggle-slider"></span>
          </label>
        </div>
        <div class="wall-cam-frame">
          <img class="wall-cam-img" alt="${escapeHtml(c.name)} preview" hidden>
          <div class="wall-cam-wait" hidden>waiting for camera…</div>
          <div class="wall-cam-off">off</div>
        </div>
      </div>
    `).join('');

    cams.forEach(c => {
      const tile  = els.cams.querySelector(`.wall-cam[data-cam="${cssEscape(c.name)}"]`);
      const cbox  = tile.querySelector('input[type=checkbox]');
      cbox.addEventListener('change', () => {
        if (cbox.checked) startCam(c.name); else stopCam(c.name);
        const cur = loadCamPrefs();
        if (cbox.checked) cur.add(c.name); else cur.delete(c.name);
        saveCamPrefs(cur);
      });
      if (enabled.has(c.name)) startCam(c.name);
    });
  }

  function startCam(name) {
    if (camTimers.has(name)) return;
    const tile = els.cams.querySelector(`.wall-cam[data-cam="${cssEscape(name)}"]`);
    if (!tile) return;
    const img = tile.querySelector('.wall-cam-img');
    const off = tile.querySelector('.wall-cam-off');
    const wait = tile.querySelector('.wall-cam-wait');
    off.hidden = true;
    // Until the first frame decodes, show a "waiting" tile rather than a
    // broken-image icon. pump-cv 404s the snapshot endpoint while a camera
    // has no decoded frame yet (cold start / RTSP disconnected), so the
    // <img> error fires every poll until the stream comes up.
    img.hidden = true;
    if (wait) wait.hidden = false;
    img.onload = () => { img.hidden = false; if (wait) wait.hidden = true; };
    img.onerror = () => { img.hidden = true; if (wait) wait.hidden = false; };
    const tick = () => {
      // Nobody can see the frame while the tab is hidden; skip the fetch
      // and resume on the next interval after the tab is visible again.
      if (document.hidden) return;
      // Cache-buster — the snapshot endpoint sends Cache-Control: no-store
      // anyway, but `?t=` defends against any intermediate caching layer.
      img.src = `/api/cv/api/v1/cameras/${encodeURIComponent(name)}/snapshot?t=${Date.now()}`;
    };
    tick();
    camTimers.set(name, setInterval(tick, CAM_POLL_MS));
  }

  function stopCam(name) {
    const handle = camTimers.get(name);
    if (handle) { clearInterval(handle); camTimers.delete(name); }
    const tile = els.cams.querySelector(`.wall-cam[data-cam="${cssEscape(name)}"]`);
    if (!tile) return;
    const img = tile.querySelector('.wall-cam-img');
    const off = tile.querySelector('.wall-cam-off');
    const wait = tile.querySelector('.wall-cam-wait');
    img.onload = img.onerror = null;
    img.hidden = true;
    img.removeAttribute('src');
    if (wait) wait.hidden = true;
    off.hidden = false;
  }

  // CSS.escape isn't on every kiosk browser; trivial escape covers
  // our camera-name charset (alphanumeric + dash + underscore).
  function cssEscape(s) {
    return String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&');
  }

  function renderSetCard(s, isCurrent) {
    const cv      = s.Source === 'cv';
    const pending = !!s.Pending;

    const cvBadge = cv ? '<span class="wall-set-cvbadge" title="Detected by camera">CV</span>' : '';
    const note    = s.Note ? `<div class="wall-set-meta">${escapeHtml(s.Note)}</div>` : '';
    const actions = pending
      ? `<div class="wall-set-actions">
           <button class="wall-set-btn confirm" title="Confirm">✓</button>
           <button class="wall-set-btn reject"  title="Reject">✗</button>
         </div>`
      : '';
    const cls = ['wall-set'];
    if (isCurrent) cls.push('is-current');

    return `
      <article class="${cls.join(' ')}"
               data-set-id="${s.ID}" data-pending="${pending}">
        <div class="wall-set-body">
          <div class="wall-set-name">${escapeHtml(s.Name)} ${cvBadge}</div>
          <div class="wall-set-numbers">
            ${formatWeight(s.Weight)}<span class="unit">lb</span>
            &nbsp;×&nbsp;
            ${s.Reps}<span class="unit">reps</span>
          </div>
          ${note}
        </div>
        ${actions}
      </article>
    `;
  }

  function formatWeight(w) {
    const n = parseFloat(w);
    if (isNaN(n)) return w;
    return Number.isInteger(n) ? n.toString() : n.toFixed(1);
  }

  // ─── Set-recording modal ────────────────────────────────────────────
  function openClipModal(s) {
    if (s && s.ClipPath) {
      els.clipBody.innerHTML =
        `<video class="wall-clip-video" src="/clips/${escapeHtml(s.ClipPath)}" ` +
        `autoplay loop muted playsinline controls></video>` +
        `<div class="wall-clip-caption">${escapeHtml(s.Name)} — ` +
        `${formatWeight(s.Weight)} lb × ${s.Reps} reps</div>`;
    } else {
      els.clipBody.innerHTML =
        `<div class="wall-clip-empty">` +
          `<div class="wall-clip-empty-icon">⊘</div>` +
          `<div class="wall-clip-empty-msg">No recording captured for this set.</div>` +
        `</div>` +
        `<div class="wall-clip-caption">${escapeHtml(s.Name)} — ` +
        `${formatWeight(s.Weight)} lb × ${s.Reps} reps</div>`;
    }
    els.clipModal.hidden = false;
  }
  function closeClipModal() {
    els.clipModal.hidden = true;
    // Stop the video so it doesn't keep playing audio/network in the
    // background; clearing innerHTML detaches the <video>.
    els.clipBody.innerHTML = '';
  }
  // Click outside the box closes; click on the close button closes.
  els.clipModal.addEventListener('click', (ev) => {
    if (ev.target === els.clipModal) closeClipModal();
  });
  els.clipClose.addEventListener('click', closeClipModal);
  // Escape key also closes (laptop convenience).
  document.addEventListener('keydown', (ev) => {
    if (ev.key === 'Escape' && !els.clipModal.hidden) closeClipModal();
  });
  // ─── Tap-to-confirm / tap-to-reject ─────────────────────────────────
  async function confirmSet(id) {
    try {
      const r = await fetch(`/api/sets/${id}/confirm`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: '{}',
      });
      if (!r.ok) throw new Error('HTTP ' + r.status);
    } catch (e) {
      console.error('confirm failed', e);
    }
  }

  async function rejectSet(id) {
    try {
      const r = await fetch(`/api/sets/${id}`, { method: 'DELETE' });
      if (!r.ok && r.status !== 204) throw new Error('HTTP ' + r.status);
    } catch (e) {
      console.error('reject failed', e);
    }
  }

  // ─── Status pip ─────────────────────────────────────────────────────
  let setsConnected = false;
  let wallConnected = false;
  function updateStatus() {
    const st  = (setsConnected && wallConnected) ? 'live'
              : (setsConnected || wallConnected) ? 'connecting'
              : 'error';
    els.status.dataset.state = st;
    els.statusLabel.textContent = st;
  }
  updateStatus();

  // ─── Initial fetch + SSE: /api/sets/stream ──────────────────────────
  async function loadInitialSets() {
    try {
      const r = await fetch('/api/sets');
      if (!r.ok) throw new Error('HTTP ' + r.status);
      const all = await r.json();
      (all || []).forEach(s => sets.set(s.ID, s));
      renderSets();
    } catch (e) {
      console.error('initial set load failed', e);
    }
  }

  function openSetsStream() {
    const upsert = e => { if (e.set) { sets.set(e.id, e.set); renderSets(); } };
    subscribeSetEvents({
      open: () => { setsConnected = true; updateStatus(); },
      error: () => { setsConnected = false; updateStatus(); },
      add: upsert,
      update: upsert,
      delete: e => { if (e.id) { sets.delete(e.id); renderSets(); } },
      bulk: () => loadInitialSets(),
    });
  }

  // ─── Wall events: wake / sleep / build_changed ──────────────────────
  function openWallStream() {
    const es = new EventSource('/api/wall/stream');
    es.onopen = () => { wallConnected = true; updateStatus(); };
    es.onerror = () => { wallConnected = false; updateStatus(); };

    es.addEventListener('wake',  () => document.body.classList.remove('sleeping'));
    es.addEventListener('sleep', () => document.body.classList.add('sleeping'));

    es.addEventListener('build_changed', ev => {
      const e = JSON.parse(ev.data);
      if (e.data && window._wallBuildSHA && e.data !== window._wallBuildSHA) {
        console.info('wall: build changed (' + window._wallBuildSHA + ' → ' + e.data + '), reloading');
        // Small jitter so a fleet of kiosks doesn't all reload in the
        // exact same instant if we ever have more than one.
        setTimeout(() => location.reload(), 500 + Math.random() * 1500);
      }
    });
  }

  // Tap the sleep overlay to wake locally (server may also wake us via SSE).
  els.sleep.addEventListener('click', () => {
    document.body.classList.remove('sleeping');
    fetch('/api/wall/wake', { method: 'POST' }).catch(() => {});
  });

  // ─── Cursor idle-hide ───────────────────────────────────────────────
  // Cursor is visible by default; hide it after CURSOR_IDLE_MS of no
  // pointer movement so the wall display is clean for distance viewing.
  // Any movement reveals it again.
  const CURSOR_IDLE_MS = 3 * 60 * 1000;   // 3 minutes
  let cursorIdleTimer = null;
  function resetCursorIdle() {
    document.body.classList.remove('cursor-idle');
    clearTimeout(cursorIdleTimer);
    cursorIdleTimer = setTimeout(() => {
      document.body.classList.add('cursor-idle');
    }, CURSOR_IDLE_MS);
  }
  // pointermove fires for both mouse and pen; touch is left alone since
  // the kiosk's touch input shouldn't keep the cursor visible.
  window.addEventListener('mousemove', resetCursorIdle, { passive: true });
  resetCursorIdle();

  // ─── Calibration wizard ─────────────────────────────────────────────
  // pump-cv enters a needs_calibration state when ≥2 cameras are
  // configured and no .npz files exist yet. This block polls
  // /api/cv/api/v1/calibration/state; whenever phase != "ready" the
  // wizard takeover is shown and the rest of the page (sets, cams)
  // stays paused. Once compute succeeds and the state transitions to
  // "ready" the takeover hides itself and the regular boot proceeds.
  const calib = {
    el:        document.getElementById('wallCalib'),
    prevs:     document.getElementById('wallCalibPrevs'),
    status:    document.getElementById('wallCalibStatus'),
    error:     document.getElementById('wallCalibError'),
    btnCap:    document.getElementById('wallCalibCapture'),
    btnUndo:   document.getElementById('wallCalibUndo'),
    btnCompute:document.getElementById('wallCalibCompute'),
    btnReset:  document.getElementById('wallCalibReset'),
    bootDone:  false,
    pollTimer: null,
    prevTimer: null,
  };
  const CALIB_POLL_MS    = 2000;
  const CALIB_PREVIEW_MS = 1000;
  const CALIB_NEEDED     = 10;

  async function calibFetchState() {
    try {
      const r = await fetch('/api/cv/api/v1/calibration/state');
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return await r.json();
    } catch (e) {
      console.warn('wall: calibration state unavailable', e);
      return null;
    }
  }

  function calibBuildPrevs(camNames) {
    if (!calib.prevs) return;
    if (calib.prevs.dataset.cams === camNames.join('|')) return;
    calib.prevs.dataset.cams = camNames.join('|');
    calib.prevs.innerHTML = camNames.map(n => `
      <div class="wall-calib-prev" data-cam="${escapeHtml(n)}">
        <span class="wall-calib-prev-label">${escapeHtml(n)}</span>
        <img alt="${escapeHtml(n)} preview" hidden>
        <span class="wall-calib-prev-wait">waiting for camera…</span>
        <span class="wall-calib-prev-detect" hidden></span>
      </div>
    `).join('');
    // Show the frame once it decodes; fall back to the "waiting" label
    // whenever the snapshot 404s (no frame yet) instead of a broken image.
    camNames.forEach(n => {
      const tile = calib.prevs.querySelector(`.wall-calib-prev[data-cam="${cssEscape(n)}"]`);
      if (!tile) return;
      const img  = tile.querySelector('img');
      const wait = tile.querySelector('.wall-calib-prev-wait');
      img.onload  = () => { img.hidden = false; if (wait) wait.hidden = true; };
      img.onerror = () => { img.hidden = true;  if (wait) wait.hidden = false; };
    });
  }

  function calibStartPreviews(camNames) {
    clearInterval(calib.prevTimer);
    const tick = () => {
      if (document.hidden) return;
      camNames.forEach(n => {
        const tile = calib.prevs.querySelector(`.wall-calib-prev[data-cam="${cssEscape(n)}"]`);
        if (!tile) return;
        const img = tile.querySelector('img');
        // Cache-bust so we always get the latest.
        img.src = `/api/cv/api/v1/cameras/${encodeURIComponent(n)}/snapshot?_=${Date.now()}`;
      });
    };
    tick();
    calib.prevTimer = setInterval(tick, CALIB_PREVIEW_MS);
  }
  function calibStopPreviews() {
    clearInterval(calib.prevTimer);
    calib.prevTimer = null;
  }

  function calibUpdateStatus(s) {
    if (!s) {
      calib.status.textContent = 'Waiting for pump-cv…';
      return;
    }
    const counts = Object.entries(s.captures || {})
      .map(([k, v]) => `${k}: ${v}`)
      .join('  •  ') || '0 captures';
    const need = Math.max(0, CALIB_NEEDED - Math.min(...Object.values(s.captures || {0:0})));
    const phaseLabel = ({
      needs_calibration: 'Awaiting first capture',
      calibrating: need > 0 ? `${need} more pair(s) needed` : 'Ready to compute',
      ready: 'Calibration complete',
      error: 'Compute failed',
    })[s.phase] || s.phase;
    calib.status.textContent = `${phaseLabel}  —  ${counts}`;
    if (s.phase === 'error' && s.error) {
      calib.error.textContent = s.error;
      calib.error.hidden = false;
    } else {
      calib.error.hidden = true;
    }
    const minCount = Math.min(...Object.values(s.captures || {0:0}));
    calib.btnCompute.disabled = (minCount < CALIB_NEEDED);
  }

  async function calibCapture() {
    calib.btnCap.disabled = true;
    try {
      const r = await fetch('/api/cv/api/v1/calibration/capture', { method: 'POST' });
      if (!r.ok) {
        const t = await r.text();
        calib.error.textContent = `Capture failed: ${t}`;
        calib.error.hidden = false;
        return;
      }
      const result = await r.json();
      // Briefly flash the per-camera detection result on the tile.
      Object.entries(result.detections || {}).forEach(([n, ok]) => {
        const tile = calib.prevs.querySelector(`.wall-calib-prev[data-cam="${cssEscape(n)}"]`);
        if (!tile) return;
        const badge = tile.querySelector('.wall-calib-prev-detect');
        badge.dataset.ok = ok ? 'true' : 'false';
        badge.textContent = ok ? '✓ board found' : '✗ no board';
        badge.hidden = false;
      });
    } finally {
      calib.btnCap.disabled = false;
    }
  }

  async function calibUndo() {
    const s = await calibFetchState();
    if (!s) return;
    const idx = Math.min(...Object.values(s.captures || {0:0})) - 1;
    if (idx < 0) return;
    await fetch(`/api/cv/api/v1/calibration/capture/${idx}`, { method: 'DELETE' });
  }

  async function calibCompute() {
    calib.btnCompute.disabled = true;
    calib.status.textContent = 'Computing calibration…';
    try {
      const r = await fetch('/api/cv/api/v1/calibration/compute', { method: 'POST' });
      if (!r.ok) {
        const t = await r.text();
        calib.error.textContent = `Compute failed: ${t}`;
        calib.error.hidden = false;
      }
    } catch (e) {
      calib.error.textContent = String(e);
      calib.error.hidden = false;
    } finally {
      calib.btnCompute.disabled = false;
    }
  }

  async function calibReset() {
    if (!window.confirm('Discard all captures and start over?')) return;
    await fetch('/api/cv/api/v1/calibration/reset', { method: 'POST' });
  }

  function calibStartBoot() {
    if (calib.bootDone) return;
    calib.bootDone = true;
    calibStopPreviews();
    if (calib.el) calib.el.hidden = true;
    clearInterval(calib.pollTimer);
    calib.pollTimer = null;
    loadInitialSets();
    loadCameras();
    openSetsStream();
    openWallStream();
  }

  function calibAttachHandlers() {
    if (!calib.btnCap) return;
    calib.btnCap.addEventListener('click', calibCapture);
    calib.btnUndo.addEventListener('click', calibUndo);
    calib.btnCompute.addEventListener('click', calibCompute);
    calib.btnReset.addEventListener('click', calibReset);
  }

  async function calibPollOnce() {
    const s = await calibFetchState();
    if (!s) {
      // pump-cv unreachable → assume single-cam / no wizard needed and
      // boot the normal page rather than blocking the user.
      calibStartBoot();
      return;
    }
    if (s.phase === 'ready' || s.camera_count < 2) {
      calibStartBoot();
      return;
    }
    if (calib.el) calib.el.hidden = false;
    const camNames = Object.keys(s.captures || {});
    if (camNames.length === 0) {
      // Pump-cv hasn't registered cameras yet; fall back to the
      // configured names by hitting /api/v1/cameras.
      try {
        const r = await fetch('/api/cv/api/v1/cameras');
        if (r.ok) {
          const cams = await r.json();
          cams.forEach(c => { (s.captures = s.captures || {})[c.name] = 0; });
        }
      } catch (_) { /* ignore */ }
    }
    const finalCamNames = Object.keys(s.captures || {});
    if (finalCamNames.length >= 2) {
      calibBuildPrevs(finalCamNames);
      if (!calib.prevTimer) calibStartPreviews(finalCamNames);
    }
    calibUpdateStatus(s);
  }

  calibAttachHandlers();
  calibPollOnce();
  calib.pollTimer = setInterval(calibPollOnce, CALIB_POLL_MS);

  // ─── Boot ───────────────────────────────────────────────────────────
  // calibStartBoot() above triggers the rest of the wall view once
  // pump-cv reports ready (or is unreachable). Don't double-boot here.

  // PWA: register the service worker for offline-resilient kiosk boot.
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/fs/public/wall-sw.js', { scope: '/wall/' })
      .catch(err => console.warn('wall: SW registration failed', err));
  }
})();
