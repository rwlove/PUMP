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

  // Today, in the page's local timezone (matches the server's timezone
  // since the kiosk is in the same room as the server in practice).
  const today = new Date().toISOString().slice(0, 10);

  const els = {
    date:     document.getElementById('wallDate'),
    clock:    document.getElementById('wallClock'),
    sets:     document.getElementById('wallSets'),
    cams:     document.getElementById('wallCams'),
    status:   document.getElementById('wallStatus'),
    statusLabel: document.getElementById('wallStatusLabel'),
    sleep:    document.querySelector('.wall-sleep'),
  };

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
      .filter(s => s.Date === today)
      .sort((a, b) => b.ID - a.ID); // newest first

    if (todays.length === 0) {
      els.sets.innerHTML = '<div class="wall-empty">No sets yet today.</div>';
      return;
    }

    els.sets.innerHTML = todays.map((s, i) => renderSetCard(s, i === 0)).join('');

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
      <div class="wall-cam" data-cam="${escapeHTML(c.name)}">
        <div class="wall-cam-header">
          <span class="wall-cam-name">${escapeHTML(c.name)}</span>
          <label class="wall-cam-toggle">
            <input type="checkbox" ${enabled.has(c.name) ? 'checked' : ''}>
            <span class="wall-cam-toggle-slider"></span>
          </label>
        </div>
        <div class="wall-cam-frame">
          <img class="wall-cam-img" alt="${escapeHTML(c.name)} preview" hidden>
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
    img.hidden = false;
    off.hidden = true;
    const tick = () => {
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
    img.hidden = true;
    img.removeAttribute('src');
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
    const color   = s.WorkoutColor || s.Color || '#6c757d';

    const cvBadge = cv ? '<span class="wall-set-cvbadge" title="Detected by camera">CV</span>' : '';
    const note    = s.Note ? `<div class="wall-set-meta">${escapeHTML(s.Note)}</div>` : '';
    const actions = pending
      ? `<div class="wall-set-actions">
           <button class="wall-set-btn confirm" title="Confirm">✓</button>
           <button class="wall-set-btn reject"  title="Reject">✗</button>
         </div>`
      : '';

    return `
      <article class="wall-set${isCurrent ? ' is-current' : ''}"
               data-set-id="${s.ID}" data-pending="${pending}">
        <div class="wall-set-color" style="background:${color}"></div>
        <div class="wall-set-body">
          <div class="wall-set-name">${escapeHTML(s.Name)} ${cvBadge}</div>
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

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, c => ({
      '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
    }[c]));
  }

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
    const es = new EventSource('/api/sets/stream');
    es.onopen = () => { setsConnected = true; updateStatus(); };
    es.onerror = () => { setsConnected = false; updateStatus(); };

    es.addEventListener('add', ev => {
      const e = JSON.parse(ev.data);
      if (e.set) { sets.set(e.id, e.set); renderSets(); }
    });
    es.addEventListener('update', ev => {
      const e = JSON.parse(ev.data);
      if (e.set) { sets.set(e.id, e.set); renderSets(); }
    });
    es.addEventListener('delete', ev => {
      const e = JSON.parse(ev.data);
      if (e.id) { sets.delete(e.id); renderSets(); }
    });
    es.addEventListener('bulk', () => loadInitialSets());
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

  // ─── Boot ───────────────────────────────────────────────────────────
  loadInitialSets();
  loadCameras();
  openSetsStream();
  openWallStream();

  // PWA: register the service worker for offline-resilient kiosk boot.
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/fs/public/wall-sw.js', { scope: '/wall/' })
      .catch(err => console.warn('wall: SW registration failed', err));
  }
})();
