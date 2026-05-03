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
  openSetsStream();
  openWallStream();
})();
