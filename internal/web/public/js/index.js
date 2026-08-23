var id = 0;
var today = null;
var _saveTimer = null;

function saveStatus(state, text) {
    var s = document.getElementById('saveStatus');
    if (!s) return;
    s.className = 'save-status' + (state ? ' ' + state : '');
    s.textContent = text || '';
    if (state === 'saved') {
        setTimeout(function() {
            if (s.className === 'save-status saved') {
                s.className = 'save-status'; s.textContent = '';
            }
        }, 2000);
    }
}

// saveWorkout picks the save strategy. Per-set is required whenever a sidecar
// may be writing: saveWorkoutBulk rewrites the entire day from the DOM, so any
// row this browser never saw — a set pump-cv or pump-voltra just wrote — is
// deleted, and the rows that survive lose source/confidence/pending/clip_path.
function saveWorkout() {
    if (window._autoLog) {
        saveWorkoutPerSet();
    } else {
        saveWorkoutBulk();
    }
}

function saveWorkoutBulk() {
    var form = document.forms['sets'];
    if (!form) return;
    saveStatus('saving', 'Saving…');
    fetch('/set/', { method: 'POST', body: new FormData(form) })
        .then(function(r) {
            if (!r.ok) throw new Error('HTTP ' + r.status);
            saveStatus('saved', 'Saved');
        })
        .catch(function() { saveStatus('error', 'Error saving'); });
}

// Read the current field values from a workout entry.
function entryToSet(entry) {
    var name   = entry.querySelector('input[name="name"]').value;
    var weight = entry.querySelector('input[name="weight"]').value;
    var reps   = entry.querySelector('input[name="reps"]').value;
    var color  = entry.querySelector('input[name="workout_color"]').value;
    var note   = entry.querySelector('input[name="note"]').value;
    var dateInput = document.getElementById('formDate');
    var out = {
        Date: dateInput ? dateInput.value : (today || ''),
        Name: name,
        Color: color,
        WorkoutColor: color,
        Weight: weight || '0',
        Reps: parseInt(reps, 10) || 0,
        Note: note,
        Source: entry.dataset.source || 'manual',
    };
    // Only bodyweight rows carry AddedWeight; leaving it off keeps ordinary
    // sets' added_weight NULL server-side.
    if (entry.dataset.bodyweight === 'true') {
        var addedInput = entry.querySelector('input[name="added_weight"]');
        var raw = addedInput ? addedInput.value : '';
        out.AddedWeight = raw === '' ? '0' : raw;
    }
    return out;
}

function saveWorkoutPerSet() {
    var dirty = document.querySelectorAll('.workout-entry[data-dirty="true"]');
    if (dirty.length === 0) return;
    var promises = [];
    dirty.forEach(function(entry) {
        // A create already in flight has no id yet. Starting a second one
        // would POST the same row twice — type a weight within ~600 ms of
        // adding an exercise on a slow connection and you get two sets, one
        // blank. Leave it dirty; the next autosave PATCHes it with an id.
        if (entry.dataset.saving === 'true') return;

        var sid = entry.dataset.setId;
        var body = entryToSet(entry);
        var p;
        if (sid) {
            var patch = {
                Name: body.Name,
                Weight: body.Weight,
                Reps: body.Reps,
                Note: body.Note,
            };
            if (body.AddedWeight !== undefined) patch.AddedWeight = body.AddedWeight;
            p = fetch('/api/sets/' + sid, {
                method: 'PATCH',
                headers: writeHeaders(),
                body: JSON.stringify(patch),
            }).then(function(r) {
                // Without this a 500 resolves normally, the entry is marked
                // clean, and the UI reports "Saved" for an edit that was never
                // persisted and will never be retried.
                if (!r.ok) throw new Error('HTTP ' + r.status);
            });
        } else {
            entry.dataset.saving = 'true';
            p = fetch('/api/sets', {
                method: 'POST',
                headers: writeHeaders(),
                body: JSON.stringify(body),
            }).then(function(r) {
                if (!r.ok) throw new Error('HTTP ' + r.status);
                return r.json();
            }).then(function(out) {
                entry.dataset.setId = out.id;
            }).finally(function() {
                entry.dataset.saving = 'false';
            });
        }
        promises.push(p.then(function() { entry.dataset.dirty = 'false'; }));
    });
    if (promises.length === 0) return;
    saveStatus('saving', 'Saving…');
    Promise.all(promises)
        .then(function() { saveStatus('saved', 'Saved'); })
        .catch(function() { saveStatus('error', 'Error saving'); });
}

function scheduleAutosave() {
    clearTimeout(_saveTimer);
    _saveTimer = setTimeout(saveWorkout, 600);
}

function markDirty(entry) {
    if (entry) entry.dataset.dirty = 'true';
}

// Return the weight/reps from the setPosition-th (1-indexed) occurrence of
// `name` on the most recent past date it was performed before `currentDate`.
// Falls back to the last occurrence on that date if setPosition exceeds the count.
function getAutoFillData(name, setPosition, currentDate) {
    var sets = window._allSets;
    if (!sets || sets.length === 0) return null;

    // Collect past dates that have this exercise, grouping sets per date in order.
    var byDate = {};
    for (var i = 0; i < sets.length; i++) {
        var s = sets[i];
        if (s.Name !== name) continue;
        if (s.Date >= currentDate) continue; // only strictly past dates
        if (!byDate[s.Date]) byDate[s.Date] = [];
        byDate[s.Date].push(s);
    }

    var dates = Object.keys(byDate).sort();
    if (dates.length === 0) return null;

    var lastDate = dates[dates.length - 1];
    var daySets  = byDate[lastDate];

    // Use the k-th set (clamp to last if not enough sets that day)
    var idx = Math.min(setPosition - 1, daySets.length - 1);
    return daySets[idx];
}

// exerciseByName returns the exercise metadata object (from window._allExs)
// for a name, or null. Used to read the Bodyweight flag when building a row.
function exerciseByName(name) {
    var exs = window._allExs || [];
    for (var i = 0; i < exs.length; i++) {
        if (exs[i].Name === name) return exs[i];
    }
    return null;
}

// bodyWeightOnOrBefore returns the most recent body-weight reading on or
// before dateStr (YYYY-MM-DD), or null. window._allWeight is oldest-first.
function bodyWeightOnOrBefore(dateStr) {
    var ws = window._allWeight || [];
    var best = null;
    for (var i = 0; i < ws.length; i++) {
        if (ws[i].Date <= dateStr) best = ws[i];
        else break;
    }
    return best ? best.Weight : null;
}

// Count how many entries for `name` already exist in #todayEx.
function countExistingEntries(name) {
    var inputs = document.querySelectorAll('#todayEx input[data-exname]');
    var count  = 0;
    for (var i = 0; i < inputs.length; i++) {
        if (inputs[i].getAttribute('data-exname') === name) count++;
    }
    return count;
}

// addExercise — builds one workout entry row.
// fromPicker: when true, auto-fill logic is applied (if enabled).
// note: existing note text for this set (used when reloading saved data).
// meta:  optional {id, source, pending, confidence} — set when loading a
//        saved entry or when an SSE add event delivers a CV detection.
function addExercise(name, weight, reps, color, fromPicker, note, meta) {
    id++;
    meta = meta || {};
    var container = document.getElementById("todayEx");
    var entry = document.createElement('div');
    entry.className = 'workout-entry';
    entry.id = 'entry-' + id;
    if (meta.id)         entry.dataset.setId = meta.id;
    if (meta.source)     entry.dataset.source = meta.source;
    if (meta.pending)    entry.dataset.pending = 'true';
    if (meta.confidence !== undefined) entry.dataset.confidence = meta.confidence;

    var safeColor = color || '#6c757d';
    // Stored on the entry so refreshSetBadges() can read it when wrapping
    // contiguous same-name entries in a .workout-entry-group (the group
    // owns the visible color stripe — no per-entry strip element).
    entry.dataset.exerciseColor = safeColor;

    // Determine which set number this will be (1-indexed).
    var setNum = countExistingEntries(name) + 1;

    // Look up the matching set from the last past date this exercise was done.
    // Used both for auto-fill (weight/reps) and for showing the previous note.
    var prior = today ? getAutoFillData(name, setNum, today) : null;

    if (fromPicker && window._autoFill && prior) {
        weight = prior.Weight;
        reps   = prior.Reps;
    }

    // Bodyweight handling (Feature 6). An exercise flagged bodyweight logs the
    // athlete's body weight as the base load (Weight), with an optional
    // added-weight field. A saved row is a bodyweight set when it carries an
    // AddedWeight value (nil for ordinary weighted sets).
    var exMeta        = exerciseByName(name);
    var addedProvided = meta.addedWeight !== undefined && meta.addedWeight !== null;
    var isBodyweight  = addedProvided || !!(exMeta && exMeta.Bodyweight);
    var addedVal      = '';
    if (isBodyweight) {
        if (addedProvided) {
            addedVal = String(meta.addedWeight);
        } else if (prior && prior.AddedWeight !== undefined && prior.AddedWeight !== null) {
            addedVal = String(prior.AddedWeight);
        } else {
            addedVal = '0';
        }
        // From the picker, seed the base with the most recent body weight
        // rather than a stale prior snapshot.
        if (fromPicker) {
            var bw = bodyWeightOnOrBefore(today || todayStr());
            if (bw !== null && bw !== undefined) weight = bw;
        }
    }

    var safeWeight = (weight !== undefined && weight !== '' && weight !== '0') ? weight : '';
    var safeReps   = (reps   !== undefined && reps   !== '' && reps   !== '0') ? reps   : '';
    var safeNote   = note || '';
    var priorNote  = (prior && prior.Note) ? prior.Note : '';
    // Exercise names are unrestricted free text and this row is built with
    // innerHTML, so an unescaped name is script. Names reach here both from
    // the exercise list and from sidecar-written sets arriving over SSE, so an
    // injected one would execute on every render of the workout page.
    var safeName   = escapeHtml(name);

    var setBadge = '<span class="set-badge">Set ' + setNum + '</span>';

    // Note dictation runs off the microphone through PUMP's own /api/stt →
    // self-hosted whisper, not the browser's cloud speech API. Support is
    // just mic capture + Web Audio, which every current browser has.
    var micSupported = !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia &&
        (window.AudioContext || window.webkitAudioContext));
    var micBtn = micSupported
        ? `<button type="button" class="entry-mic-btn" title="Dictate note" aria-label="Dictate note">
               <i class="bi bi-mic"></i>
           </button>
           <span class="entry-mic-status" aria-live="polite"></span>`
        : '';

    var priorNoteIndicator = priorNote
        ? `<button type="button" class="entry-prior-note-btn" title="Show note from last time" aria-label="Show note from last time">
               <i class="bi bi-sticky"></i>
           </button>`
        : '';

    var sourceBadge = meta.source === 'cv'
        ? `<i class="bi bi-camera-video entry-source-badge" title="Detected by CV"></i>`
        : '';

    var actionButtons = meta.pending
        ? `<button type="button" class="entry-confirm-btn" title="Confirm set" aria-label="Confirm set">
               <i class="bi bi-check-lg"></i>
           </button>
           <button type="button" class="entry-reject-btn" title="Reject — delete this set" aria-label="Reject and delete this set">
               <i class="bi bi-x-lg"></i>
           </button>`
        : `<button type="button" class="entry-del-btn" title="Remove" aria-label="Remove set">
               <i class="bi bi-x-lg"></i>
           </button>`;

    var pendingBanner = meta.pending
        ? `<div class="entry-pending-banner">
               Pending — ${typeof meta.confidence === 'number' ? Math.round(meta.confidence * 100) + '% confidence' : 'low confidence'}. Edit values then confirm.
           </div>`
        : '';

    // Bodyweight rows label the base field "BW" and add a "+ lbs" added-weight
    // field; ordinary rows carry an empty hidden added_weight so the bulk-save
    // form's parallel arrays stay aligned (every row emits exactly one).
    var weightLabel = isBodyweight ? 'BW' : 'lbs';
    var addedField = isBodyweight
        ? `<div class="entry-field">
               <span class="entry-label">+ lbs</span>
               <input type="number" class="form-control entry-num entry-added" name="added_weight"
                   value="${escapeHtml(addedVal)}" min="0" step="any" placeholder="0">
           </div>`
        : `<input type="hidden" name="added_weight" value="">`;
    // Drag handle for reordering (Feature 5). touch-action:none (CSS) lets a
    // touch drag work without the page scrolling.
    var dragHandle = `<button type="button" class="entry-drag-handle" title="Drag to reorder" aria-label="Drag to reorder"><i class="bi bi-grip-vertical"></i></button>`;

    entry.dataset.bodyweight = isBodyweight ? 'true' : 'false';

    entry.innerHTML = `
        <div class="entry-main">
            <div class="entry-body">
                <div class="entry-header">
                    ${dragHandle}
                    <input type="hidden" name="name" value="${safeName}" data-exname="${safeName}">
                    ${sourceBadge}
                    <span class="entry-name" title="${safeName}">${safeName}</span>
                    ${setBadge}
                </div>
                ${pendingBanner}
                <div class="entry-controls">
                    <div class="entry-field">
                        <span class="entry-label">${weightLabel}</span>
                        <input type="number" class="form-control entry-num" name="weight"
                            value="${safeWeight}" min="0" step="any" placeholder="${weightLabel}">
                    </div>
                    ${addedField}
                    <div class="entry-field">
                        <span class="entry-label">reps</span>
                        <input type="number" class="form-control entry-num" name="reps"
                            value="${safeReps}" min="0" placeholder="reps">
                    </div>
                    <input type="hidden" name="workout_color" value="${safeColor}">
                    <input type="hidden" class="entry-note-input" name="note" value="">
                    ${priorNoteIndicator}
                    ${micBtn}
                    ${actionButtons}
                </div>
            </div>
        </div>
        <div class="entry-prior-note" style="display:none;"></div>
        <div class="entry-note-row" style="display:none;">
            <i class="bi bi-mic-fill entry-note-icon"></i>
            <span class="entry-note-text"></span>
            <button type="button" class="entry-note-clear" title="Clear note" aria-label="Clear note">
                <i class="bi bi-x"></i>
            </button>
        </div>
    `;

    // Set initial note value/visibility
    var noteInput = entry.querySelector('.entry-note-input');
    noteInput.value = safeNote;
    if (safeNote) renderNote(entry, safeNote);

    // Prior-note expand/collapse
    if (priorNote) {
        var priorPanel = entry.querySelector('.entry-prior-note');
        priorPanel.textContent = priorNote;
        entry.querySelector('.entry-prior-note-btn').addEventListener('click', function() {
            priorPanel.style.display = priorPanel.style.display === 'none' ? '' : 'none';
        });
    }

    // Wire up weight/reps/added inputs → autosave (and mark dirty for per-set save)
    entry.querySelectorAll('.entry-num').forEach(function(inp) {
        inp.addEventListener('change', function() {
            markDirty(entry);
            scheduleAutosave();
        });
    });

    // Drag handle → pointer-based reorder (works with mouse and touch).
    var dragBtn = entry.querySelector('.entry-drag-handle');
    if (dragBtn) dragBtn.addEventListener('pointerdown', onDragHandlePointerDown);

    // Mic dictation
    if (micSupported) {
        var micButton = entry.querySelector('.entry-mic-btn');
        micButton.addEventListener('click', function() {
            toggleDictation(entry, micButton);
        });
    }

    // Clear note
    entry.querySelector('.entry-note-clear').addEventListener('click', function() {
        noteInput.value = '';
        renderNote(entry, '');
        markDirty(entry);
        scheduleAutosave();
    });

    // Delete entry — locally always; in per-set mode, and if the row has a
    // server id, also send DELETE to the API so it actually goes away. In bulk
    // mode the following /set/ save rewrites the day, which does the deleting.
    var delBtn = entry.querySelector('.entry-del-btn');
    if (delBtn) {
        delBtn.addEventListener('click', function() {
            // Autolog mode deletes immediately and irreversibly via the API, so
            // guard it: a mis-tap on the small × on a sweaty phone would
            // otherwise drop a logged set with no undo. This matches the
            // confirm() already on every other destructive action (routines,
            // groups, prototypes). Bulk mode only pulls the DOM row — the next
            // save rewrites the day — so it stays friction-free and unconfirmed.
            if (window._autoLog && entry.dataset.setId &&
                !window.confirm('Delete this set?')) {
                return;
            }
            removeEntry(entry, /*viaApi*/ window._autoLog);
        });
    }

    // Pending: confirm and reject buttons.
    var confirmBtn = entry.querySelector('.entry-confirm-btn');
    if (confirmBtn) {
        confirmBtn.addEventListener('click', function() {
            confirmEntry(entry);
        });
    }
    var rejectBtn = entry.querySelector('.entry-reject-btn');
    if (rejectBtn) {
        rejectBtn.addEventListener('click', function() {
            removeEntry(entry, /*viaApi*/ true);
        });
    }

    container.appendChild(entry);
    // Voltra sets get their per-set LOAD control and row-click arming. No-op
    // for every other exercise.
    if (typeof voltraDecorate === 'function') {
        voltraDecorate(entry);
        voltraBindRowClick(entry);
    }
    refreshSetBadges();
    updateEmptyState();
    refreshWeekStreak();
    // Mark new (no server id yet) entries dirty so the next autosave POSTs them.
    //
    // Only schedule a save for genuinely new rows. Rendering a day calls this
    // once per existing set, and an unconditional autosave meant simply
    // *looking* at a day rewrote it: in bulk mode that is DELETE-the-day plus
    // re-INSERT from the DOM, so any set the browser had not seen — one added
    // from the wall kiosk or another tab since page load — was destroyed by a
    // save the user never asked for. A row that arrived with a server id is
    // already persisted and has nothing to write back.
    if (!meta.id) {
        markDirty(entry);
        scheduleAutosave();
    }
}

// removeEntry pulls an entry out of the DOM. When viaApi is true and the
// entry has a server id, also fires DELETE /api/sets/:id (don't wait).
function removeEntry(entry, viaApi) {
    if (viaApi && entry.dataset.setId) {
        fetch('/api/sets/' + entry.dataset.setId, {method: 'DELETE', headers: writeHeaders()});
    }
    entry.remove();
    updateEmptyState();
    refreshSetBadges();
    refreshWeekStreak();
    if (!viaApi) scheduleAutosave();
}

// ── Drag-and-drop reorder of workout entries (Feature 5) ─────────────────────
// Pointer-based so it works with both mouse and touch (the gym kiosk/phone).
// Entries live inside per-name .workout-entry-group wrappers; we move the
// dragged entry within the DOM live, then refreshSetBadges() re-wraps the whole
// list from document order after the drop, so intermediate wrapper membership
// doesn't matter.
var _drag = null;

function onDragHandlePointerDown(e) {
    var handle = e.currentTarget;
    var entry = handle.closest('.workout-entry');
    if (!entry) return;
    e.preventDefault();
    _drag = { entry: entry, handle: handle };
    entry.classList.add('dragging');
    try { handle.setPointerCapture(e.pointerId); } catch (_) {}
    handle.addEventListener('pointermove', onDragPointerMove);
    handle.addEventListener('pointerup', onDragPointerUp);
    handle.addEventListener('pointercancel', onDragPointerUp);
}

function onDragPointerMove(e) {
    if (!_drag) return;
    var container = document.getElementById('todayEx');
    if (!container) return;
    var el = document.elementFromPoint(e.clientX, e.clientY);
    var over = el ? el.closest('.workout-entry') : null;
    if (!over || over === _drag.entry || !container.contains(over)) return;
    // Insert before or after `over` based on the pointer vs its midpoint.
    var rect = over.getBoundingClientRect();
    var after = e.clientY > rect.top + rect.height / 2;
    over.parentNode.insertBefore(_drag.entry, after ? over.nextSibling : over);
}

function onDragPointerUp(e) {
    if (!_drag) return;
    var handle = _drag.handle;
    _drag.entry.classList.remove('dragging');
    try { handle.releasePointerCapture(e.pointerId); } catch (_) {}
    handle.removeEventListener('pointermove', onDragPointerMove);
    handle.removeEventListener('pointerup', onDragPointerUp);
    handle.removeEventListener('pointercancel', onDragPointerUp);
    _drag = null;
    // Re-wrap contiguous same-name runs / renumber, then persist the new order.
    refreshSetBadges();
    persistOrder();
}

// persistOrder saves the current top-to-bottom entry order. In per-set mode it
// sends the explicit id order to the reorder endpoint; in bulk mode the DOM
// order is the save order, so a normal autosave (which rewrites the whole day)
// reassigns position from insertion index.
function persistOrder() {
    if (window._autoLog) {
        var ids = [];
        document.querySelectorAll('#todayEx .workout-entry').forEach(function(en) {
            if (en.dataset.setId) ids.push(parseInt(en.dataset.setId, 10));
        });
        if (!ids.length) return;
        var dateInput = document.getElementById('formDate');
        var date = dateInput ? dateInput.value : today;
        saveStatus('saving', 'Saving…');
        fetch('/api/sets/reorder', {
            method: 'POST',
            headers: writeHeaders(),
            body: JSON.stringify({ date: date, ids: ids }),
        }).then(function(r) {
            if (!r.ok) throw new Error('HTTP ' + r.status);
            saveStatus('saved', 'Saved');
        }).catch(function() { saveStatus('error', 'Error saving'); });
    } else {
        scheduleAutosave();
    }
}

// refreshWeekStreak recomputes the "Last 7 days" panel using a snapshot
// that overrides the currently-viewed date with the live #todayEx count.
// This keeps the today bubble in sync the moment a set is added or
// removed, without needing the autosave roundtrip to mutate _allSets.
function refreshWeekStreak() {
    var dom = document.getElementById('todayEx');
    var domCount = dom ? dom.querySelectorAll('.workout-entry').length : 0;
    var historical = (window._allSets || []).filter(function(s) {
        return s.Date !== today;
    });
    var stubs = [];
    for (var i = 0; i < domCount; i++) stubs.push({ Date: today });
    renderWeekStreak(historical.concat(stubs));
}

// confirmEntry collects current values and POSTs them to the confirm endpoint.
// Server clears `pending` and applies any edits in one shot.
function confirmEntry(entry) {
    var sid = entry.dataset.setId;
    if (!sid) return;
    var body = entryToSet(entry);
    var confirmBody = {
        Name: body.Name,
        Weight: body.Weight,
        Reps: body.Reps,
        Note: body.Note,
    };
    if (body.AddedWeight !== undefined) confirmBody.AddedWeight = body.AddedWeight;
    fetch('/api/sets/' + sid + '/confirm', {
        method: 'POST',
        headers: writeHeaders(),
        body: JSON.stringify(confirmBody),
    }).then(function(r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        // Clear the pending state locally rather than waiting for SSE.
        //
        // This used to rely on receiving its own update event, but the
        // echo-suppression fix for the double-add bug means the server
        // deliberately skips the originating client. Nothing arrived, so the
        // banner and the ✓/✗ pair stayed on screen forever and the row never
        // gained a delete button — until a reload.
        clearPendingState(entry);
    }).catch(function() { saveStatus('error', 'Confirm failed'); });
}

// clearPendingState turns a confirmed row into an ordinary one: drop the
// banner, swap the confirm/reject pair for a delete button. A pending row
// renders one or the other, never both, so the delete button has to be built
// rather than un-hidden.
function clearPendingState(entry) {
    entry.dataset.pending = 'false';

    var banner = entry.querySelector('.entry-pending-banner');
    if (banner) banner.remove();

    var confirmBtn = entry.querySelector('.entry-confirm-btn');
    var rejectBtn = entry.querySelector('.entry-reject-btn');
    var anchor = confirmBtn || rejectBtn;
    if (!anchor) return; // already an ordinary row

    var del = document.createElement('button');
    del.type = 'button';
    del.className = 'entry-del-btn';
    del.title = 'Remove';
    del.setAttribute('aria-label', 'Remove set');
    del.innerHTML = '<i class="bi bi-x-lg"></i>';
    del.addEventListener('click', function() {
        removeEntry(entry, /*viaApi*/ window._autoLog);
    });
    anchor.parentNode.insertBefore(del, anchor);

    if (confirmBtn) confirmBtn.remove();
    if (rejectBtn) rejectBtn.remove();
}

// renderNote shows or hides the note row based on whether text is present.
function renderNote(entry, text) {
    var row = entry.querySelector('.entry-note-row');
    var span = entry.querySelector('.entry-note-text');
    if (text) {
        span.textContent = text;
        row.style.display = '';
    } else {
        span.textContent = '';
        row.style.display = 'none';
    }
}

// Dictation captures microphone audio, downsamples it to the 16 kHz mono PCM
// that whisper expects, and POSTs it to /api/stt — the transcript comes back
// from PUMP's self-hosted whisper, never a cloud service. Tap to start, tap
// again to stop and transcribe.
//
// Only one dictation runs at a time; _dictation holds the active session so a
// second tap (on the same button) knows to stop it.
var _dictation = null;
var STT_RATE = 16000;

function micStatus(entry, msg, isError) {
    var el = entry.querySelector('.entry-mic-status');
    if (!el) return;
    el.textContent = msg || '';
    el.classList.toggle('is-error', !!isError);
}

function micStatusClearLater(entry, ms) {
    setTimeout(function() { micStatus(entry, ''); }, ms || 3500);
}

function toggleDictation(entry, button) {
    if (_dictation) {
        // A session is live. A tap on its own button stops it; taps elsewhere
        // are ignored rather than starting a competing capture.
        if (_dictation.button === button) stopDictation();
        return;
    }
    startDictation(entry, button);
}

function startDictation(entry, button) {
    var AudioCtx = window.AudioContext || window.webkitAudioContext;
    navigator.mediaDevices.getUserMedia({ audio: true }).then(function(stream) {
        var ctx = new AudioCtx();
        var source = ctx.createMediaStreamSource(stream);
        var processor = ctx.createScriptProcessor(4096, 1, 1);
        // Route through a muted gain to destination: some browsers won't fire
        // onaudioprocess unless the node is connected downstream, but sending
        // the mic to the speakers would feed back. Zero gain keeps it silent.
        var mute = ctx.createGain();
        mute.gain.value = 0;

        var chunks = [];
        processor.onaudioprocess = function(ev) {
            chunks.push(new Float32Array(ev.inputBuffer.getChannelData(0)));
        };
        source.connect(processor);
        processor.connect(mute);
        mute.connect(ctx.destination);

        button.classList.add('recording');
        micStatus(entry, 'listening… tap to stop');
        _dictation = { entry: entry, button: button, ctx: ctx, stream: stream,
                       source: source, processor: processor, mute: mute,
                       chunks: chunks, sampleRate: ctx.sampleRate };
    }).catch(function() {
        micStatus(entry, 'mic blocked — allow access', true);
        micStatusClearLater(entry, 4000);
    });
}

function stopDictation() {
    var d = _dictation;
    _dictation = null;
    if (!d) return;

    d.button.classList.remove('recording');
    try { d.processor.disconnect(); d.mute.disconnect(); d.source.disconnect(); } catch (_) {}
    d.stream.getTracks().forEach(function(t) { t.stop(); });
    if (d.ctx.state !== 'closed') { try { d.ctx.close(); } catch (_) {} }

    var samples = mergeFloat32(d.chunks);
    if (!samples.length) { micStatus(d.entry, ''); return; }
    var pcm = floatToPCM16(downsampleTo(samples, d.sampleRate, STT_RATE));

    d.button.classList.add('transcribing');
    micStatus(d.entry, 'transcribing…');
    fetch('/api/stt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: pcm,
    }).then(function(r) {
        if (!r.ok) {
            return r.json().catch(function() { return {}; }).then(function(e) {
                throw new Error(e.error || 'transcription failed');
            });
        }
        return r.json();
    }).then(function(res) {
        d.button.classList.remove('transcribing');
        var text = (res.text || '').trim();
        if (!text) { micStatus(d.entry, 'no speech detected', true); micStatusClearLater(d.entry); return; }
        var noteInput = d.entry.querySelector('.entry-note-input');
        var existing = noteInput.value.trim();
        noteInput.value = existing ? existing + ' ' + text : text;
        renderNote(d.entry, noteInput.value);
        markDirty(d.entry);
        scheduleAutosave();
        micStatus(d.entry, '');
    }).catch(function(e) {
        d.button.classList.remove('transcribing');
        micStatus(d.entry, e.message || 'transcription failed', true);
        micStatusClearLater(d.entry, 4000);
    });
}

// mergeFloat32 flattens the captured per-callback buffers into one array.
function mergeFloat32(chunks) {
    var len = 0;
    chunks.forEach(function(c) { len += c.length; });
    var out = new Float32Array(len);
    var off = 0;
    chunks.forEach(function(c) { out.set(c, off); off += c.length; });
    return out;
}

// downsampleTo resamples from the capture rate (typically 44.1/48 kHz) to the
// target by averaging each source window — cheap, and adequate for speech.
function downsampleTo(buf, from, to) {
    if (to >= from) return buf;
    var ratio = from / to;
    var outLen = Math.round(buf.length / ratio);
    var out = new Float32Array(outLen);
    var pos = 0;
    for (var i = 0; i < outLen; i++) {
        var next = Math.round((i + 1) * ratio);
        var sum = 0, n = 0;
        for (var j = pos; j < next && j < buf.length; j++) { sum += buf[j]; n++; }
        out[i] = n ? sum / n : 0;
        pos = next;
    }
    return out;
}

// floatToPCM16 packs [-1,1] floats into little-endian signed 16-bit samples,
// the format the /api/stt handler and whisper expect.
function floatToPCM16(buf) {
    var view = new DataView(new ArrayBuffer(buf.length * 2));
    for (var i = 0; i < buf.length; i++) {
        var s = Math.max(-1, Math.min(1, buf[i]));
        view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7FFF, true);
    }
    return view.buffer;
}

// Renumber set badges after any structural change (delete, load).
// A badge is only visible when there is more than one set for that exercise name.
function refreshSetBadges() {
    var container = document.getElementById('todayEx');
    if (!container) return;

    // One DOM pass: collect each entry's name and badge, numbering as we go.
    var entries = container.querySelectorAll('.workout-entry');
    var counters = {}; // name → running count (1-indexed); ends as the total
    var rows = [];     // [{entry, name, badge, ordinal}]

    entries.forEach(function(entry) {
        var hidden = entry.querySelector('input[data-exname]');
        if (!hidden) return;
        var name = hidden.getAttribute('data-exname');
        counters[name] = (counters[name] || 0) + 1;
        rows.push({
            entry: entry,
            name: name,
            badge: entry.querySelector('.set-badge'),
            ordinal: counters[name],
        });
    });

    // Second pass over the collected rows (no DOM queries): a badge shows
    // "Set N" and is visible only when the exercise has more than one set.
    rows.forEach(function(r) {
        if (!r.badge) return;
        r.badge.textContent = 'Set ' + r.ordinal;
        r.badge.style.display = (counters[r.name] > 1) ? '' : 'none';
    });

    // Wrap consecutive same-name entries in a .workout-entry-group so the
    // group owns the per-exercise color stripe (a single ::before painted
    // along its full height) instead of each entry painting its own.
    // Entries keep their identity — we only re-parent them, never recreate
    // them, so attached event listeners survive.
    var flat = [];
    entries.forEach(function(e) { flat.push(e); });
    // Hoist all entries flat under the container so we can re-wrap cleanly.
    flat.forEach(function(e) { container.appendChild(e); });
    // Drop any now-empty wrappers left over from the previous pass.
    container.querySelectorAll('.workout-entry-group').forEach(function(w) {
        if (!w.querySelector('.workout-entry')) w.remove();
    });
    // Walk in order; wrap each contiguous same-name run in one group.
    var i = 0;
    while (i < flat.length) {
        var first = flat[i];
        var hidden = first.querySelector('input[data-exname]');
        if (!hidden) { i++; continue; }
        var name = hidden.getAttribute('data-exname');
        var j = i + 1;
        while (j < flat.length) {
            var nextHidden = flat[j].querySelector('input[data-exname]');
            if (!nextHidden || nextHidden.getAttribute('data-exname') !== name) break;
            j++;
        }
        var group = document.createElement('div');
        group.className = 'workout-entry-group';
        group.style.setProperty('--exercise-color',
            first.dataset.exerciseColor || '#6c757d');
        container.insertBefore(group, first);
        for (var k = i; k < j; k++) group.appendChild(flat[k]);
        i = j;
    }
}

function updateEmptyState() {
    var hasEntries = document.getElementById('todayEx').children.length > 0;
    document.getElementById('emptyState').style.display = hasEntries ? 'none' : '';
}

// Format a YYYY-MM-DD string as e.g. "Friday, May 1st 2026".
function formatFriendlyDate(dateStr) {
    // Parse as local date by splitting manually (avoids UTC shift).
    var parts = dateStr.split('-');
    var d = new Date(parseInt(parts[0]), parseInt(parts[1]) - 1, parseInt(parts[2]));
    var day = d.getDate();
    var suffix = (day === 1 || day === 21 || day === 31) ? 'st'
               : (day === 2 || day === 22)               ? 'nd'
               : (day === 3 || day === 23)               ? 'rd' : 'th';
    var weekday = d.toLocaleDateString('en-US', { weekday: 'long' });
    var month   = d.toLocaleDateString('en-US', { month: 'long' });
    var year    = d.getFullYear();
    return weekday + ', ' + month + ' ' + day + suffix + ' ' + year;
}

function setFormContent(sets, date) {
    window.sessionStorage.setItem("today", date);
    today = date;
    document.getElementById('todayEx').innerHTML = "";
    updateEmptyState();
    document.getElementById("formDate").value = date;
    document.getElementById("realDate").value = date;
    var btn = document.getElementById('dateDisplayBtn');
    if (btn) btn.textContent = formatFriendlyDate(date);

    // Mark the matching "Last 7 days" dot as selected (if the date is in range).
    document.querySelectorAll('.week-dot-item').forEach(function(it) {
        it.classList.toggle('is-selected', it.getAttribute('data-date') === date);
    });

    if (sets) {
        for (var i = 0; i < sets.length; i++) {
            if (sets[i].Date == date) {
                // fromPicker=false: loading saved data, no auto-fill override
                addExercise(sets[i].Name, sets[i].Weight, sets[i].Reps, sets[i].WorkoutColor, false, sets[i].Note, {
                    id:          sets[i].ID,
                    source:      sets[i].Source,
                    pending:     sets[i].Pending,
                    confidence:  sets[i].Confidence,
                    addedWeight: sets[i].AddedWeight,
                });
            }
        }
    }
}

// findEntryByServerId returns the DOM entry for a given server-side set id.
function findEntryByServerId(sid) {
    return document.querySelector('.workout-entry[data-set-id="' + sid + '"]');
}

// Whether any input inside the entry currently has focus. SSE updates are
// skipped on focused entries so the user's typing isn't clobbered.
function entryHasFocus(entry) {
    return entry.contains(document.activeElement);
}

// openSetsStream subscribes to SSE for set lifecycle events. Only called when
// CVAutoLog is on. Events for other dates are ignored. Events that match a
// row already in the DOM (own writes echoed back) are deduplicated by id.
function openSetsStream() {
    subscribeSetEvents({
    add: function(e) {
        if (!e.set || e.date !== today) return;
        if (findEntryByServerId(e.id)) return; // already present (our own write echoed back)
        addExercise(
            e.set.Name, e.set.Weight, e.set.Reps, e.set.WorkoutColor,
            false, e.set.Note,
            {id: e.id, source: e.set.Source, pending: e.set.Pending, confidence: e.set.Confidence, addedWeight: e.set.AddedWeight}
        );
    },

    update: function(e) {
        if (!e.set || e.date !== today) return;
        var entry = findEntryByServerId(e.id);
        if (!entry) return;
        if (entryHasFocus(entry)) return; // user is editing — don't clobber
        // Refresh the visible fields and pending state. Easier than diffing:
        // mutate inputs and toggle the pending banner / buttons.
        var w = entry.querySelector('input[name="weight"]');
        var r = entry.querySelector('input[name="reps"]');
        var n = entry.querySelector('.entry-note-input');
        if (w) w.value = e.set.Weight;
        if (r) r.value = e.set.Reps;
        if (n) { n.value = e.set.Note || ''; renderNote(entry, n.value); }
        var wasPending = entry.dataset.pending === 'true';
        var isPending  = !!e.set.Pending;
        if (wasPending !== isPending) {
            // Re-render the entry by replacing it. Easier than surgically
            // swapping confirm/reject buttons for a delete button.
            // Re-adds via addExercise's append-to-bottom; order may shift
            // to bottom. Single-user gym: ordering is forgivable here.
            entry.remove();
            addExercise(
                e.set.Name, e.set.Weight, e.set.Reps, e.set.WorkoutColor,
                false, e.set.Note,
                {id: e.id, source: e.set.Source, pending: isPending, confidence: e.set.Confidence, addedWeight: e.set.AddedWeight}
            );
        }
    },

    "delete": function(e) {
        var entry = findEntryByServerId(e.id);
        if (entry) {
            entry.remove();
            updateEmptyState();
            refreshSetBadges();
        }
    },

    bulk: function(e) {
        if (e.date !== today) return;
        // Re-fetch all sets and re-render the day.
        fetch('/api/sets').then(function(r) { return r.json(); }).then(function(sets) {
            window._allSets = sets || [];
            setFormContent(window._allSets, today);
        });
    },
    });
}

function setFormDate(sets) {
    var date = window.sessionStorage.getItem("today");
    if (!date) {
        date = todayStr();
    }
    setFormContent(sets, date);
}

function goToToday() {
    // Compute the live local date rather than window._serverDate: that value is
    // rendered server-side at page load, so a phone left open across midnight
    // would otherwise send "Today" to yesterday. en-CA gives YYYY-MM-DD.
    var date = localDateStr(new Date());
    setFormContent(window._allSets, date);
}


function moveDayLeftRight(where, sets) {
    var dateStr = document.getElementById("realDate").value;
    var year  = dateStr.substring(0, 4);
    var month = dateStr.substring(5, 7);
    var day   = dateStr.substring(8, 10);
    var date  = new Date(year, month - 1, day);
    date.setDate(date.getDate() + parseInt(where));
    var newDate = localDateStr(date);
    setFormContent(sets, newDate);
}

function selectGroup(gr) {
    window._selectedGroup = gr;

    document.querySelectorAll('.group-chip').forEach(function(chip) {
        chip.classList.toggle('active', chip.getAttribute('data-group') === gr);
    });

    var header = document.getElementById('groupHeader');
    var noGroup = document.getElementById('noGroupState');
    var searchInput = document.getElementById('exSearch');
    if (header) {
        header.style.display = 'flex';
        document.getElementById('groupHeaderName').textContent = gr;
    }
    if (noGroup) noGroup.style.display = 'none';
    if (searchInput) searchInput.value = '';

    document.querySelectorAll('.exercise-item').forEach(function(item) {
        item.style.display = item.getAttribute('data-group') === gr ? '' : 'none';
    });
}

function clearGroup() {
    window._selectedGroup = null;

    document.querySelectorAll('.group-chip').forEach(function(chip) {
        chip.classList.remove('active');
    });

    var header = document.getElementById('groupHeader');
    var noGroup = document.getElementById('noGroupState');
    if (header) header.style.display = 'none';
    if (noGroup) noGroup.style.display = '';
    document.querySelectorAll('.exercise-item').forEach(function(item) {
        item.style.display = 'none';
    });
}

function renderWeekStreak(sets) {
    var el = document.getElementById('weekStreak');
    if (!el) return;
    var today = new Date();
    var dayLetters = ['S','M','T','W','T','F','S'];
    var activeCount = 0;
    var items = [];
    for (var i = 6; i >= 0; i--) {
        var d = new Date(today.getFullYear(), today.getMonth(), today.getDate() - i);
        var dateStr = localDateStr(d);
        var isToday = i === 0;
        var hasWorkout = sets && sets.some(function(s) { return s.Date === dateStr; });
        if (hasWorkout) activeCount++;
        items.push({ label: dayLetters[d.getDay()], active: hasWorkout, today: isToday, date: dateStr });
    }
    var dotsHtml = items.map(function(item) {
        var dotCls = 'week-dot' + (item.active ? ' has-workout' : '') + (item.today ? ' is-today' : '');
        var lblCls = 'week-dot-label' + (item.today ? ' is-today' : '');
        // Click a day to jump the workout view to it (same path as prev/next).
        return '<div class="week-dot-item" role="button" tabindex="0" title="' + item.date + '" aria-label="Go to ' + item.date + '"' +
            ' style="cursor:pointer" data-date="' + item.date + '"' +
            ' onclick="setFormContent(window._allSets, \'' + item.date + '\')"' +
            ' onkeydown="if(event.key===\'Enter\'||event.key===\' \'){event.preventDefault();setFormContent(window._allSets,\'' + item.date + '\');}">' +
            '<div class="' + dotCls + '"></div><span class="' + lblCls + '">' + item.label + '</span></div>';
    }).join('');
    el.innerHTML = '<div class="panel week-streak-panel"><div class="week-streak-inner">' +
        '<span class="week-streak-title">Last 7 days</span>' +
        '<div class="week-dots">' + dotsHtml + '</div>' +
        '<span class="week-streak-count"><strong>' + activeCount + '</strong><span class="week-streak-of"> / 7</span></span>' +
        '</div></div>';
}

function filterExercises() {
    var query = document.getElementById('exSearch').value.toLowerCase().trim();
    var gr = window._selectedGroup;
    if (!gr) return;

    document.querySelectorAll('.exercise-item').forEach(function(item) {
        var nameMatch = item.getAttribute('data-name').toLowerCase().includes(query);
        var groupMatch = item.getAttribute('data-group') === gr;
        item.style.display = (groupMatch && (!query || nameMatch)) ? '' : 'none';
    });
}
