// Voltra phase-2 controls on the workout page.
//
// Two states matter and they are not the same thing:
//
//   armed   this set is what the trainer's telemetry will be attributed to
//   loaded  the motor is confirmed engaged at this set's weight
//
// Recording needs BOTH. An armed-but-unloaded set is the dangerous one — you
// would do the set and nothing would be recorded — so it is drawn as
// deliberately unfinished (ghosted, dashed, no fill) and only becomes solid
// once the sidecar confirms the motor is engaged. Absence of substance reads
// as "not ready" from across the room, without reading text and without
// relying on colour.
//
// The page never talks to the trainer. It writes intent to PUMP; the sidecar
// does the hardware and reports back. So `loaded` here always means "the
// sidecar told us the read-back matched", never "we asked for it".

var _voltraState = {ArmedSetID: 0, WantLoad: false, Loaded: false, Error: ''};

// voltraFlagged reports whether an exercise is performed on the trainer.
// Driven by the per-exercise checkbox, not the name — plenty of exercises have
// "cable" in the name and run off a plate stack.
function voltraFlagged(name) {
    if (!name || !window._allExs) return false;
    var want = String(name).trim().toLowerCase();
    return window._allExs.some(function(ex) {
        return ex.Voltra && String(ex.Name || '').trim().toLowerCase() === want;
    });
}

function voltraEntryName(entry) {
    var el = entry.querySelector('input[name="name"]');
    return el ? el.value : '';
}

// voltraDecorate adds the per-set control to a workout entry, if its exercise
// uses the trainer. Idempotent — safe to call on re-render.
function voltraDecorate(entry) {
    if (!entry || entry.querySelector('.voltra-ctl')) return;
    if (!voltraFlagged(voltraEntryName(entry))) return;

    entry.classList.add('voltra-set');

    var controls = entry.querySelector('.entry-controls');
    if (!controls) return;

    var box = document.createElement('div');
    box.className = 'voltra-ctl';
    box.innerHTML =
        '<button type="button" class="voltra-load-btn" title="Set the weight on the trainer and engage">' +
            'LOAD</button>' +
        '<span class="voltra-status" aria-live="polite"></span>';

    // Insert before the action buttons so LOAD sits with the set's own fields
    // rather than off among the row actions — the fix belongs where the
    // problem is.
    var actions = controls.querySelector('.entry-del-btn, .entry-confirm-btn');
    if (actions) controls.insertBefore(box, actions);
    else controls.appendChild(box);

    box.querySelector('.voltra-load-btn').addEventListener('click', function(ev) {
        ev.stopPropagation();
        voltraToggleLoad(entry);
    });

    voltraPaint(entry);
}

// Clicking anywhere on a Voltra row makes it the current set. The pointer
// drifts for real reasons — the device splits a set on a long pause, a set is
// redone or skipped, the sidecar reconnects — and because the pointer selects
// the weight written to the motor, it has to be correctable without ceremony.
function voltraBindRowClick(entry) {
    if (!entry || entry.dataset.voltraBound) return;
    if (!voltraFlagged(voltraEntryName(entry))) return;
    entry.dataset.voltraBound = '1';
    entry.addEventListener('click', function(ev) {
        if (ev.target.closest('input, button, .voltra-ctl')) return;
        voltraArm(entry);
    });
}

function voltraSetId(entry) {
    var sid = entry.dataset.setId;
    return sid ? parseInt(sid, 10) : 0;
}

function voltraWeight(entry) {
    var el = entry.querySelector('input[name="weight"]');
    return el ? el.value : '';
}

function voltraArm(entry) {
    var sid = voltraSetId(entry);
    if (!sid) {
        // The row has not been saved yet, so there is nothing to attribute a
        // set to. Autosave is fast; the next click will work.
        voltraFlash(entry, 'saving…');
        return;
    }
    fetch('/api/voltra/arm', {
        method: 'POST',
        headers: writeHeaders(),
        body: JSON.stringify({SetID: sid, WeightLb: voltraWeight(entry)}),
    }).then(function(r) { return r.ok ? r.json() : r.json().then(function(e) { throw new Error(e.error); }); })
      .then(voltraApply)
      .catch(function(e) { voltraFlash(entry, e.message || 'could not arm'); });
}

function voltraToggleLoad(entry) {
    var sid = voltraSetId(entry);
    if (!sid) { voltraFlash(entry, 'saving…'); return; }

    var wantLoad = !(_voltraState.ArmedSetID === sid && _voltraState.Loaded);

    // Loading a set that isn't current arms it first — pressing LOAD is a
    // clear statement of which set you are about to do. The arm must succeed
    // before we load: a failed arm that still proceeded would tell the motor
    // to engage at a weight PUMP never attributed to a set.
    var pre = (_voltraState.ArmedSetID === sid)
        ? Promise.resolve()
        : fetch('/api/voltra/arm', {
              method: 'POST', headers: writeHeaders(),
              body: JSON.stringify({SetID: sid, WeightLb: voltraWeight(entry)}),
          }).then(function(r) {
              if (!r.ok) return r.json().then(function(e) { throw new Error(e.error || 'could not arm'); });
          });

    pre.then(function() {
        return fetch('/api/voltra/load', {
            method: 'POST', headers: writeHeaders(),
            body: JSON.stringify({Load: wantLoad}),
        });
    }).then(function(r) { return r.json(); })
      .then(voltraApply)
      .catch(function(e) { voltraFlash(entry, e.message || 'load failed'); });
}

function voltraFlash(entry, msg) {
    var s = entry.querySelector('.voltra-status');
    if (!s) return;
    s.textContent = msg;
    s.title = '';
    s.classList.add('voltra-status-warn');
    setTimeout(function() { s.classList.remove('voltra-status-warn'); voltraPaint(entry); }, 2500);
}

// voltraPaint renders one entry against the current state. Everything visual
// lives here so there is exactly one place that decides what "ready" looks
// like.
function voltraPaint(entry) {
    if (!entry.classList.contains('voltra-set')) return;
    var sid = voltraSetId(entry);
    var armed = sid !== 0 && _voltraState.ArmedSetID === sid;
    var loaded = armed && !!_voltraState.Loaded;

    entry.classList.toggle('voltra-armed', armed && !loaded);
    entry.classList.toggle('voltra-loaded', loaded);

    var btn = entry.querySelector('.voltra-load-btn');
    var status = entry.querySelector('.voltra-status');
    if (!btn || !status) return;

    btn.textContent = loaded ? 'UNLOAD' : 'LOAD';
    btn.classList.toggle('is-loaded', loaded);

    if (status.classList.contains('voltra-status-warn')) return;
    status.title = '';
    if (loaded) {
        status.textContent = 'loaded — recording';
    } else if (armed && _voltraState.Error) {
        // The raw error is a BLE/GATT string that overflows the row and reads
        // as noise. Show a short label and keep the full text on hover.
        status.textContent = 'trainer error';
        status.title = _voltraState.Error;
    } else if (armed) {
        status.textContent = 'not loaded — press LOAD';
    } else {
        status.textContent = '';
    }
}

function voltraPaintAll() {
    document.querySelectorAll('.workout-entry').forEach(function(entry) {
        voltraDecorate(entry);
        voltraBindRowClick(entry);
        voltraPaint(entry);
    });
}

function voltraApply(state) {
    if (state) _voltraState = state;
    voltraPaintAll();
}

// voltraInit wires the page to PUMP's Voltra state. Safe to call when the
// trainer is not in use — with nothing armed, nothing is drawn differently.
function voltraInit() {
    fetch('/api/voltra/state').then(function(r) { return r.json(); })
        .then(voltraApply).catch(function() {});

    var es = new EventSource('/api/voltra/stream');
    es.addEventListener('state', function(ev) {
        try { voltraApply(JSON.parse(ev.data)); } catch (e) { /* ignore */ }
    });
    es.onerror = function() {}; // EventSource reconnects on its own
    return es;
}
