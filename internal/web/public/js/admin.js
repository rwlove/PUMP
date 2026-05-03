// PUMP CV admin panel.
//
// All live data comes from pump-cv via /api/cv/* (proxied by PUMP).
// Each tab degrades gracefully: if the backing endpoint is missing or
// pump-cv is down, the tab shows the error inline rather than breaking
// the page.

(function() {
  'use strict';

  // ─── Tab switching ─────────────────────────────────────────────────
  const tabs = document.querySelectorAll('.admin-tab');
  const panes = document.querySelectorAll('.admin-pane');
  const loaders = {};   // tab name → load function (called on first activation)
  const loaded = {};

  function activate(name) {
    tabs.forEach(t => t.classList.toggle('is-active', t.dataset.tab === name));
    panes.forEach(p => p.classList.toggle('is-active', p.dataset.pane === name));
    if (loaders[name] && !loaded[name]) {
      loaded[name] = true;
      try { loaders[name](); } catch (e) { console.error(e); }
    }
  }
  tabs.forEach(t => t.addEventListener('click', () => activate(t.dataset.tab)));

  // ─── Helpers ───────────────────────────────────────────────────────
  async function cvFetch(path, opts) {
    const r = await fetch('/api/cv' + path, opts);
    if (!r.ok) {
      const text = await r.text().catch(() => '');
      throw new Error('HTTP ' + r.status + ' ' + text);
    }
    const ct = r.headers.get('Content-Type') || '';
    return ct.includes('json') ? r.json() : r.text();
  }

  function showError(el, e) {
    el.innerHTML = '<div class="admin-stub">pump-cv unavailable: ' +
                   String(e.message || e).replace(/[&<>]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[c])) +
                   '</div>';
  }

  // ─── Pipeline state (live, polled) ─────────────────────────────────
  loaders.state = function() {
    const el = document.getElementById('paneState');
    async function tick() {
      try {
        const data = await cvFetch('/api/v1/state');
        el.textContent = JSON.stringify(data, null, 2);
      } catch (e) { showError(el, e); }
    }
    tick();
    setInterval(tick, 2000);
  };

  // ─── Metrics (Prometheus text format) ──────────────────────────────
  loaders.metrics = function() {
    const el = document.getElementById('paneMetrics');
    async function tick() {
      try {
        const text = await cvFetch('/metrics');
        el.textContent = text;
      } catch (e) { showError(el, e); }
    }
    tick();
    setInterval(tick, 5000);
  };

  // ─── Threshold sliders (hot-reload) ───────────────────────────────
  loaders.thresholds = function() {
    const el = document.getElementById('paneThresholds');
    cvFetch('/api/v1/thresholds').then(cfg => {
      el.innerHTML = '';
      const fields = [
        ['rep.min_amplitude_deg',   'Rep min amplitude (deg)',  5,  60, 1],
        ['rep.min_period_s',        'Rep min period (s)',       0.2, 3, 0.1],
        ['rep.smoothing_window',    'Rep smoothing window',     3,  21, 2],
        ['set_boundary.quiet_seconds', 'Set quiet seconds',     5,  120, 1],
        ['confidence_threshold',    'Confidence threshold',     0,  1, 0.01],
      ];
      const updated = {};
      fields.forEach(([key, label, min, max, step]) => {
        const value = key.split('.').reduce((o, k) => o && o[k], cfg);
        const row = document.createElement('div');
        row.className = 'admin-slider-row';
        row.innerHTML = `
          <label>${label}</label>
          <input type="range" min="${min}" max="${max}" step="${step}" value="${value}">
          <span class="value">${value}</span>
        `;
        const input = row.querySelector('input');
        const span  = row.querySelector('.value');
        input.addEventListener('input', () => {
          span.textContent = input.value;
          updated[key] = parseFloat(input.value);
          saveBtn.disabled = false;
        });
        el.appendChild(row);
      });
      const saveBtn = document.createElement('button');
      saveBtn.className = 'admin-save-btn';
      saveBtn.textContent = 'Apply (hot-reload)';
      saveBtn.disabled = true;
      saveBtn.addEventListener('click', async () => {
        try {
          await cvFetch('/api/v1/thresholds', {
            method: 'PATCH',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(updated),
          });
          saveBtn.disabled = true;
          saveBtn.textContent = 'Applied ✓';
          setTimeout(() => saveBtn.textContent = 'Apply (hot-reload)', 2000);
        } catch (e) {
          saveBtn.textContent = 'Failed: ' + e.message;
        }
      });
      el.appendChild(saveBtn);
    }).catch(e => showError(el, e));
  };

  // ─── Prototype manager ────────────────────────────────────────────
  loaders.prototypes = function() {
    const el = document.getElementById('panePrototypes');
    function render() {
      cvFetch('/api/v1/prototypes').then(list => {
        if (!list.length) {
          el.innerHTML = '<div class="admin-muted">No prototypes recorded yet.</div>';
          return;
        }
        el.innerHTML = '<div class="admin-list">' + list.map(p => `
          <div class="admin-list-row" data-path="${p.path}">
            <div>
              <div class="name">${p.exercise_name}</div>
              <div class="meta">${p.frames} frames · from ${p.source_clip || '(unknown)'}</div>
            </div>
            <div class="meta">${p.path}</div>
            <button class="danger" data-action="delete">Delete</button>
          </div>
        `).join('') + '</div>';
        el.querySelectorAll('button[data-action="delete"]').forEach(btn => {
          btn.addEventListener('click', async () => {
            const path = btn.closest('.admin-list-row').dataset.path;
            if (!confirm('Delete prototype ' + path + '?')) return;
            try {
              await cvFetch('/api/v1/prototypes?path=' + encodeURIComponent(path),
                            { method: 'DELETE' });
              render();
            } catch (e) { alert('Delete failed: ' + e.message); }
          });
        });
      }).catch(e => showError(el, e));
    }
    render();
  };

  // ─── Snapshot gallery ─────────────────────────────────────────────
  loaders.snapshots = function() {
    const el = document.getElementById('paneSnapshots');
    cvFetch('/api/v1/snapshots').then(list => {
      if (!list.length) {
        el.innerHTML = '<div class="admin-muted">No snapshots saved yet.</div>';
        return;
      }
      el.innerHTML = '<div class="admin-gallery">' + list.map(s => `
        <a class="admin-gallery-tile" href="/api/cv/api/v1/snapshots/${encodeURIComponent(s.path)}" target="_blank">
          <img src="/api/cv/api/v1/snapshots/${encodeURIComponent(s.path)}" alt="${s.path}" loading="lazy">
          <div class="caption">${s.path}</div>
        </a>
      `).join('') + '</div>';
    }).catch(e => showError(el, e));
  };

  // ─── Cameras ──────────────────────────────────────────────────────
  loaders.cameras = function() {
    const el = document.getElementById('paneCameras');
    function tick() {
      cvFetch('/api/v1/cameras').then(data => {
        if (!data.length) {
          el.innerHTML = '<div class="admin-muted">No cameras registered. (pump-cv may not have started a pose source yet.)</div>';
          return;
        }
        el.innerHTML = '<div class="admin-list">' + data.map(c => `
          <div class="admin-list-row">
            <div>
              <div class="name">${c.name}</div>
              <div class="meta">${c.source} · ${c.fps ? c.fps.toFixed(1) + ' fps' : 'no signal'}</div>
            </div>
            <div class="meta">${c.connected ? 'connected' : 'disconnected'}</div>
          </div>
        `).join('') + '</div>';
      }).catch(e => showError(el, e));
    }
    tick();
    setInterval(tick, 5000);
  };

  // ─── HSV mask preview ─────────────────────────────────────────────
  loaders.hsv = function() {
    const el = document.getElementById('paneHsv');
    el.innerHTML = `
      <div style="display:grid;grid-template-columns:300px 1fr;gap:24px">
        <div>
          <label style="display:block;margin-bottom:8px">
            Source image:
            <input type="file" id="hsvFile" accept="image/*" style="display:block;margin-top:4px">
          </label>
          <div id="hsvSliders"></div>
        </div>
        <div>
          <h3 style="margin-top:0">Original</h3>
          <img id="hsvOrig" style="max-width:100%;border:1px solid var(--wall-divider);border-radius:6px">
          <h3>Mask</h3>
          <img id="hsvMask" style="max-width:100%;border:1px solid var(--wall-divider);border-radius:6px;background:#222">
        </div>
      </div>
    `;
    const sliders = document.getElementById('hsvSliders');
    const ranges = [
      ['h_lo', 'Hue low',    0, 180,   0],
      ['h_hi', 'Hue high',   0, 180, 180],
      ['s_lo', 'Sat low',    0, 255, 100],
      ['s_hi', 'Sat high',   0, 255, 255],
      ['v_lo', 'Val low',    0, 255,  60],
      ['v_hi', 'Val high',   0, 255, 255],
    ];
    const state = {};
    ranges.forEach(([key, label, min, max, def]) => {
      state[key] = def;
      const row = document.createElement('div');
      row.className = 'admin-slider-row';
      row.innerHTML = `<label>${label}</label>
                       <input type="range" min="${min}" max="${max}" value="${def}">
                       <span class="value">${def}</span>`;
      const inp = row.querySelector('input');
      const val = row.querySelector('.value');
      inp.addEventListener('input', () => {
        val.textContent = inp.value;
        state[key] = parseInt(inp.value, 10);
        debouncedRefresh();
      });
      sliders.appendChild(row);
    });

    let _file = null;
    const hsvFile = document.getElementById('hsvFile');
    const hsvOrig = document.getElementById('hsvOrig');
    const hsvMask = document.getElementById('hsvMask');
    hsvFile.addEventListener('change', () => {
      const f = hsvFile.files[0];
      if (!f) return;
      _file = f;
      hsvOrig.src = URL.createObjectURL(f);
      refresh();
    });

    let timer = null;
    function debouncedRefresh() {
      clearTimeout(timer);
      timer = setTimeout(refresh, 150);
    }
    async function refresh() {
      if (!_file) return;
      const fd = new FormData();
      fd.append('image', _file);
      Object.entries(state).forEach(([k, v]) => fd.append(k, v));
      try {
        const r = await fetch('/api/cv/api/v1/hsv-preview', { method: 'POST', body: fd });
        if (!r.ok) throw new Error('HTTP ' + r.status);
        const blob = await r.blob();
        hsvMask.src = URL.createObjectURL(blob);
      } catch (e) {
        console.error('hsv preview failed', e);
      }
    }
  };

  // Activate the default tab.
  activate('state');
})();
