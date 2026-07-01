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

function saveWorkout() {
    if (window._cvAutoLog) {
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
    return {
        Date: dateInput ? dateInput.value : (today || ''),
        Name: name,
        Color: color,
        WorkoutColor: color,
        Weight: weight || '0',
        Reps: parseInt(reps, 10) || 0,
        Note: note,
        Source: entry.dataset.source || 'manual',
    };
}

function saveWorkoutPerSet() {
    var dirty = document.querySelectorAll('.workout-entry[data-dirty="true"]');
    if (dirty.length === 0) return;
    saveStatus('saving', 'Saving…');
    var promises = [];
    dirty.forEach(function(entry) {
        var sid = entry.dataset.setId;
        var body = entryToSet(entry);
        var p;
        if (sid) {
            p = fetch('/api/sets/' + sid, {
                method: 'PATCH',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    Name: body.Name,
                    Weight: body.Weight,
                    Reps: body.Reps,
                    Note: body.Note,
                }),
            });
        } else {
            p = fetch('/api/sets', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(body),
            }).then(function(r) {
                if (!r.ok) throw new Error('HTTP ' + r.status);
                return r.json();
            }).then(function(out) {
                entry.dataset.setId = out.id;
            });
        }
        promises.push(p.then(function() { entry.dataset.dirty = 'false'; }));
    });
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

    var safeWeight = (weight !== undefined && weight !== '' && weight !== '0') ? weight : '';
    var safeReps   = (reps   !== undefined && reps   !== '' && reps   !== '0') ? reps   : '';
    var safeNote   = note || '';
    var priorNote  = (prior && prior.Note) ? prior.Note : '';

    var setBadge = '<span class="set-badge">Set ' + setNum + '</span>';

    var speechSupported = ('webkitSpeechRecognition' in window) || ('SpeechRecognition' in window);
    var micBtn = speechSupported
        ? `<button type="button" class="entry-mic-btn" title="Dictate note">
               <i class="bi bi-mic"></i>
           </button>`
        : '';

    var priorNoteIndicator = priorNote
        ? `<button type="button" class="entry-prior-note-btn" title="Show note from last time">
               <i class="bi bi-sticky"></i>
           </button>`
        : '';

    var sourceBadge = meta.source === 'cv'
        ? `<i class="bi bi-camera-video entry-source-badge" title="Detected by CV"></i>`
        : '';

    var actionButtons = meta.pending
        ? `<button type="button" class="entry-confirm-btn" title="Confirm set">
               <i class="bi bi-check-lg"></i>
           </button>
           <button type="button" class="entry-reject-btn" title="Reject — delete this set">
               <i class="bi bi-x-lg"></i>
           </button>`
        : `<button type="button" class="entry-del-btn" title="Remove">
               <i class="bi bi-x-lg"></i>
           </button>`;

    var pendingBanner = meta.pending
        ? `<div class="entry-pending-banner">
               Pending — ${typeof meta.confidence === 'number' ? Math.round(meta.confidence * 100) + '% confidence' : 'low confidence'}. Edit values then confirm.
           </div>`
        : '';

    entry.innerHTML = `
        <div class="entry-main">
            <div class="entry-body">
                <div class="entry-header">
                    <input type="hidden" name="name" value="${name}" data-exname="${name}">
                    ${sourceBadge}
                    <span class="entry-name" title="${name}">${name}</span>
                    ${setBadge}
                </div>
                ${pendingBanner}
                <div class="entry-controls">
                    <div class="entry-field">
                        <span class="entry-label">lbs</span>
                        <input type="number" class="form-control entry-num" name="weight"
                            value="${safeWeight}" min="0" step="any" placeholder="—">
                    </div>
                    <div class="entry-field">
                        <span class="entry-label">reps</span>
                        <input type="number" class="form-control entry-num" name="reps"
                            value="${safeReps}" min="0" placeholder="—">
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
            <button type="button" class="entry-note-clear" title="Clear note">
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

    // Wire up weight/reps inputs → autosave (and mark dirty for per-set save)
    entry.querySelectorAll('.entry-num').forEach(function(inp) {
        inp.addEventListener('change', function() {
            markDirty(entry);
            scheduleAutosave();
        });
    });

    // Mic dictation
    if (speechSupported) {
        var micButton = entry.querySelector('.entry-mic-btn');
        micButton.addEventListener('click', function() {
            startDictation(entry, micButton);
        });
    }

    // Clear note
    entry.querySelector('.entry-note-clear').addEventListener('click', function() {
        noteInput.value = '';
        renderNote(entry, '');
        markDirty(entry);
        scheduleAutosave();
    });

    // Delete entry — locally always; if cv-mode and the row has a server id,
    // also send DELETE to the API so it actually goes away.
    var delBtn = entry.querySelector('.entry-del-btn');
    if (delBtn) {
        delBtn.addEventListener('click', function() {
            removeEntry(entry, /*viaApi*/ window._cvAutoLog);
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
    refreshSetBadges();
    updateEmptyState();
    refreshWeekStreak();
    // Mark new (no server id yet) entries dirty so the next autosave POSTs them.
    if (!meta.id) markDirty(entry);
    scheduleAutosave();
}

// removeEntry pulls an entry out of the DOM. When viaApi is true and the
// entry has a server id, also fires DELETE /api/sets/:id (don't wait).
function removeEntry(entry, viaApi) {
    if (viaApi && entry.dataset.setId) {
        fetch('/api/sets/' + entry.dataset.setId, {method: 'DELETE'});
    }
    entry.remove();
    updateEmptyState();
    refreshSetBadges();
    refreshWeekStreak();
    if (!viaApi) scheduleAutosave();
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
    fetch('/api/sets/' + sid + '/confirm', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
            Name: body.Name,
            Weight: body.Weight,
            Reps: body.Reps,
            Note: body.Note,
        }),
    }).then(function(r) {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        // SSE update event will refresh the entry visually.
    }).catch(function() { saveStatus('error', 'Confirm failed'); });
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

// startDictation runs the Web Speech API and writes the transcript into the note input.
function startDictation(entry, button) {
    var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!Recognition) return;

    var rec = new Recognition();
    rec.lang = 'en-US';
    rec.interimResults = false;
    rec.maxAlternatives = 1;

    var noteInput = entry.querySelector('.entry-note-input');

    button.classList.add('recording');
    rec.onresult = function(ev) {
        var transcript = '';
        for (var i = 0; i < ev.results.length; i++) {
            transcript += ev.results[i][0].transcript;
        }
        transcript = transcript.trim();
        if (!transcript) return;
        var existing = noteInput.value.trim();
        noteInput.value = existing ? existing + ' ' + transcript : transcript;
        renderNote(entry, noteInput.value);
        scheduleAutosave();
    };
    rec.onerror = function() { button.classList.remove('recording'); };
    rec.onend   = function() { button.classList.remove('recording'); };
    try { rec.start(); } catch (_) { button.classList.remove('recording'); }
}

// Renumber set badges after any structural change (delete, load).
// A badge is only visible when there is more than one set for that exercise name.
function refreshSetBadges() {
    var container = document.getElementById('todayEx');
    if (!container) return;

    // Build an ordered list of [entry, name] pairs
    var entries = container.querySelectorAll('.workout-entry');
    var counters = {}; // name → running count (1-indexed)

    entries.forEach(function(entry) {
        var hidden = entry.querySelector('input[data-exname]');
        if (!hidden) return;
        var name = hidden.getAttribute('data-exname');
        counters[name] = (counters[name] || 0) + 1;
        var badge = entry.querySelector('.set-badge');
        if (badge) badge.textContent = 'Set ' + counters[name];
    });

    // Count total occurrences per name so we know when to show/hide the badge.
    var totals = {};
    entries.forEach(function(entry) {
        var hidden = entry.querySelector('input[data-exname]');
        if (!hidden) return;
        var name = hidden.getAttribute('data-exname');
        totals[name] = (totals[name] || 0) + 1;
    });

    entries.forEach(function(entry) {
        var hidden = entry.querySelector('input[data-exname]');
        if (!hidden) return;
        var name = hidden.getAttribute('data-exname');
        var badge = entry.querySelector('.set-badge');
        if (badge) {
            badge.style.display = (totals[name] > 1) ? '' : 'none';
        }
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
                    id:         sets[i].ID,
                    source:     sets[i].Source,
                    pending:    sets[i].Pending,
                    confidence: sets[i].Confidence,
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
    var es = new EventSource('/api/sets/stream');

    es.addEventListener('add', function(ev) {
        var e = JSON.parse(ev.data);
        if (!e.set || e.date !== today) return;
        if (findEntryByServerId(e.id)) return; // already present (our own write echoed back)
        addExercise(
            e.set.Name, e.set.Weight, e.set.Reps, e.set.WorkoutColor,
            false, e.set.Note,
            {id: e.id, source: e.set.Source, pending: e.set.Pending, confidence: e.set.Confidence}
        );
    });

    es.addEventListener('update', function(ev) {
        var e = JSON.parse(ev.data);
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
                {id: e.id, source: e.set.Source, pending: isPending, confidence: e.set.Confidence}
            );
        }
    });

    es.addEventListener('delete', function(ev) {
        var e = JSON.parse(ev.data);
        var entry = findEntryByServerId(e.id);
        if (entry) {
            entry.remove();
            updateEmptyState();
            refreshSetBadges();
        }
    });

    es.addEventListener('bulk', function(ev) {
        var e = JSON.parse(ev.data);
        if (e.date !== today) return;
        // Re-fetch all sets and re-render the day.
        fetch('/api/sets').then(function(r) { return r.json(); }).then(function(sets) {
            window._allSets = sets || [];
            setFormContent(window._allSets, today);
        });
    });

    es.onerror = function() {
        // EventSource auto-reconnects; nothing to do.
    };
}

function setFormDate(sets) {
    var date = window.sessionStorage.getItem("today");
    if (!date) {
        date = window._serverDate || new Date().toLocaleDateString('en-CA');
    }
    setFormContent(sets, date);
}

function goToToday() {
    // Compute the live local date rather than window._serverDate: that value is
    // rendered server-side at page load, so a phone left open across midnight
    // would otherwise send "Today" to yesterday. en-CA gives YYYY-MM-DD.
    var date = new Date().toLocaleDateString('en-CA');
    setFormContent(window._allSets, date);
}


function moveDayLeftRight(where, sets) {
    var dateStr = document.getElementById("realDate").value;
    var year  = dateStr.substring(0, 4);
    var month = dateStr.substring(5, 7);
    var day   = dateStr.substring(8, 10);
    var date  = new Date(year, month - 1, day);
    date.setDate(date.getDate() + parseInt(where));
    var newDate = date.toLocaleDateString('en-CA');
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
        var dateStr = d.toLocaleDateString('en-CA');
        var isToday = i === 0;
        var hasWorkout = sets && sets.some(function(s) { return s.Date === dateStr; });
        if (hasWorkout) activeCount++;
        items.push({ label: dayLetters[d.getDay()], active: hasWorkout, today: isToday, date: dateStr });
    }
    var dotsHtml = items.map(function(item) {
        var dotCls = 'week-dot' + (item.active ? ' has-workout' : '') + (item.today ? ' is-today' : '');
        var lblCls = 'week-dot-label' + (item.today ? ' is-today' : '');
        // Click a day to jump the workout view to it (same path as prev/next).
        return '<div class="week-dot-item" role="button" tabindex="0" title="' + item.date + '"' +
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
