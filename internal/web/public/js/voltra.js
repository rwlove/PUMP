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

var _voltraState = {ArmedSetID: 0, WantLoad: false, Loaded: false, Error: '', CompletedSeq: 0};

// The last completion sequence this page has applied. The server bumps
// CompletedSeq once per finished set; we act only on a higher value, so an SSE
// reconnect — which re-sends the current state — or any duplicate delivery
// never applies the same set twice. This is what makes the trainer update the
// row exactly once instead of the old sidecar appending a duplicate.
var _voltraLastSeq = 0;

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

// voltraEntryBySetId finds the workout row for a PUMP set id, or null.
function voltraEntryBySetId(sid) {
    if (!sid) return null;
    var entries = document.querySelectorAll('.workout-entry');
    for (var i = 0; i < entries.length; i++) {
        if (voltraSetId(entries[i]) === sid) return entries[i];
    }
    return null;
}

// voltraMaybeApplyCompletion acts on a finished set the trainer just reported.
// The server carries the rep count (CompletedReps) for the set it was attributed
// to (CompletedSetID) plus a monotonic CompletedSeq. We apply each seq exactly
// once: write the reps into that set's own row, stamp its provenance, let the
// existing per-set autosave PATCH it in place, then advance to the next Voltra
// set. Nothing here creates a row — the set the athlete already has is updated.
function voltraMaybeApplyCompletion(state) {
    var seq = state.CompletedSeq || 0;
    if (seq <= _voltraLastSeq) return;
    // Mark applied before doing the work: even if the arm below round-trips and
    // re-delivers this same state, the guard above now rejects it.
    _voltraLastSeq = seq;

    var entry = voltraEntryBySetId(state.CompletedSetID || 0);
    if (!entry) return;

    var repsInput = entry.querySelector('input[name="reps"]');
    if (repsInput) repsInput.value = state.CompletedReps;
    entry.dataset.source = 'voltra';
    markDirty(entry);
    scheduleAutosave();
    voltraChip(entry);

    voltraAdvance(entry);
}

// voltraAdvance arms the next Voltra set after `entry` in DOM order, so the
// athlete moves straight onto it. Arming only points recording at the set and
// marks it current (green); it never engages the motor — LOAD stays a deliberate
// press. With no next Voltra set, nothing is armed.
function voltraAdvance(entry) {
    var entries = Array.prototype.slice.call(document.querySelectorAll('.workout-entry'));
    var from = entries.indexOf(entry);
    for (var i = from + 1; i < entries.length; i++) {
        var next = entries[i];
        if (voltraFlagged(voltraEntryName(next)) && voltraSetId(next)) {
            voltraArm(next);
            return;
        }
    }
}

// voltraChip shows (or removes) the non-editable "Updated by Voltra" marker on a
// row, driven purely by its source. It renders on fresh page load too — the
// server-rendered rows carry their source — so the provenance survives a reload
// and the consistency-page round-trip. It lives beside the controls, never in
// the editable note field.
function voltraChip(entry) {
    if (!entry) return;
    var has = entry.dataset.source === 'voltra';
    var chip = entry.querySelector('.voltra-updated-chip');
    if (has && !chip) {
        var controls = entry.querySelector('.entry-controls');
        if (!controls) return;
        chip = document.createElement('span');
        chip.className = 'voltra-updated-chip';
        chip.textContent = 'Updated by Voltra';
        chip.setAttribute('title', 'Reps filled in by the Voltra trainer');
        var ctl = controls.querySelector('.voltra-ctl');
        if (ctl) controls.insertBefore(chip, ctl);
        else controls.appendChild(chip);
    } else if (!has && chip) {
        chip.remove();
    }
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

    // Toggle on the *request*, not on confirmed engagement. A load that keeps
    // failing never sets Loaded, so keying off Loaded left the button stuck on
    // "LOAD" with every press re-sending the same failing request and no way
    // to call it off. Pressing a set that already has a load requested — armed
    // and WantLoad — cancels it, whether or not the motor ever confirmed.
    var wantLoad = !(_voltraState.ArmedSetID === sid && _voltraState.WantLoad);

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

// voltraActionable reports whether a sidecar error is one of the curated,
// human-readable conditions the athlete can act on directly — as opposed to a
// raw BLE/GATT failure string. These are short enough to live in the row and
// they name the fix ("press LOAD again", correct the weight), so voltraPaint
// shows them verbatim instead of flattening them to a generic banner with the
// detail hidden on hover — otherwise the one message that tells the athlete how
// to recover is the one they never see. The phrases are the stable, instructive
// parts of the strings the sidecar/server already emit (pump_voltra control.py,
// voltra/state.go); match on those rather than the whole string so wording
// tweaks around them don't silently re-bury the message.
function voltraActionable(err) {
    if (!err) return false;
    return err.indexOf('press LOAD again') !== -1 ||
           err.indexOf('unusable weight') !== -1;
}

// voltraPaint renders one entry against the current state. Everything visual
// lives here so there is exactly one place that decides what "ready" looks
// like.
function voltraPaint(entry) {
    if (!entry.classList.contains('voltra-set')) return;
    var sid = voltraSetId(entry);
    var armed = sid !== 0 && _voltraState.ArmedSetID === sid;
    var loaded = armed && !!_voltraState.Loaded;
    // Requested but not (yet) confirmed — the mid-flight and the failing cases.
    var pending = armed && !loaded && !!_voltraState.WantLoad;

    // The armed-but-not-yet-loaded set is the one you are on now ("current"):
    // highlighted green so it reads as the active set after an auto-advance.
    // This is NOT the engaged state — LOAD is still a deliberate press, and the
    // status text below says so. voltra-loaded (motor confirmed) is a separate,
    // filled style.
    entry.classList.toggle('voltra-current', armed && !loaded);
    entry.classList.toggle('voltra-loaded', loaded);

    var btn = entry.querySelector('.voltra-load-btn');
    var status = entry.querySelector('.voltra-status');
    if (!btn || !status) return;

    // Whenever a load is requested the button cancels it — labelled UNLOAD once
    // the motor confirms, CANCEL while it is still pending or failing. Only
    // with no request outstanding does it read LOAD.
    btn.textContent = loaded ? 'UNLOAD' : (pending ? 'CANCEL' : 'LOAD');
    btn.classList.toggle('is-loaded', loaded || pending);

    if (status.classList.contains('voltra-status-warn')) return;
    status.title = '';
    if (loaded) {
        status.textContent = 'loaded — recording';
    } else if (pending && _voltraState.Error) {
        if (voltraActionable(_voltraState.Error)) {
            // A curated, actionable condition — e.g. reconnected while a load
            // was pending. It is short and names exactly what to do, so show it
            // in full: the whole point is that it gets read and acted on rather
            // than buried behind a generic label.
            status.textContent = _voltraState.Error;
        } else {
            // The raw error is a BLE/GATT string that overflows the row and
            // reads as noise. Show a short label and keep the full text on
            // hover.
            status.textContent = 'trainer error — CANCEL to stop';
            status.title = _voltraState.Error;
        }
    } else if (pending) {
        status.textContent = 'loading…';
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
        voltraChip(entry);
        voltraPaint(entry);
    });
}

function voltraApply(state) {
    if (state) {
        _voltraState = state;
        // Apply a finished set before repainting so the advanced-to row shows as
        // current in the same pass.
        voltraMaybeApplyCompletion(state);
    }
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
