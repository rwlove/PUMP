var id = 0;
var today = null;
var _saveTimer = null;

function saveWorkout() {
    var form = document.forms['sets'];
    if (!form) return;
    var status = document.getElementById('saveStatus');
    if (status) { status.className = 'save-status saving'; status.textContent = 'Saving…'; }
    var data = new FormData(form);
    fetch('/set/', { method: 'POST', body: data })
        .then(function(r) {
            if (!r.ok) throw new Error('HTTP ' + r.status);
            if (status) { status.className = 'save-status saved'; status.textContent = 'Saved'; }
            setTimeout(function() {
                if (status && status.className === 'save-status saved') {
                    status.className = 'save-status'; status.textContent = '';
                }
            }, 2000);
        })
        .catch(function() {
            if (status) { status.className = 'save-status error'; status.textContent = 'Error saving'; }
        });
}

function scheduleAutosave() {
    clearTimeout(_saveTimer);
    _saveTimer = setTimeout(saveWorkout, 600);
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
function addExercise(name, weight, reps, color, fromPicker) {
    id++;
    var container = document.getElementById("todayEx");
    var entry = document.createElement('div');
    entry.className = 'workout-entry';
    entry.id = 'entry-' + id;

    var safeColor = color || '#6c757d';

    // Determine which set number this will be (1-indexed).
    var setNum = countExistingEntries(name) + 1;

    // Auto-fill: override default weight/reps with historical data when
    // the user is adding from the picker and the feature is enabled.
    if (fromPicker && window._autoFill && today) {
        var filled = getAutoFillData(name, setNum, today);
        if (filled) {
            weight = filled.Weight;
            reps   = filled.Reps;
        }
    }

    var safeWeight = (weight !== undefined && weight !== '' && weight !== '0') ? weight : '';
    var safeReps   = (reps   !== undefined && reps   !== '' && reps   !== '0') ? reps   : '';

    // Small set-number badge — always rendered so the DOM is stable.
    // CSS hides it when only one set exists for that exercise.
    var setBadge = '<span class="set-badge">S' + setNum + '</span>';

    entry.innerHTML = `
        <div class="entry-color-strip" style="background-color:${safeColor};"></div>
        <input type="hidden" name="name" value="${name}" data-exname="${name}">
        <span class="entry-name" title="${name}">${name}</span>
        ${setBadge}
        <div class="entry-controls">
            <div class="entry-field">
                <span class="entry-label">kg</span>
                <input type="number" class="form-control entry-num" name="weight"
                    value="${safeWeight}" min="0" step="any" placeholder="—">
            </div>
            <div class="entry-field">
                <span class="entry-label">reps</span>
                <input type="number" class="form-control entry-num" name="reps"
                    value="${safeReps}" min="0" placeholder="—">
            </div>
            <input type="color" class="entry-color-picker"
                name="workout_color" value="${safeColor}">
            <button type="button" class="entry-del-btn" title="Remove">
                <i class="bi bi-x-lg"></i>
            </button>
        </div>
    `;

    // Wire up weight/reps inputs → autosave
    entry.querySelectorAll('.entry-num').forEach(function(inp) {
        inp.addEventListener('change', scheduleAutosave);
    });

    // Wire up color picker → color strip live update + autosave
    var strip = entry.querySelector('.entry-color-strip');
    var colorPicker = entry.querySelector('.entry-color-picker');
    colorPicker.addEventListener('input', function() {
        strip.style.backgroundColor = this.value;
        scheduleAutosave();
    });

    // Wire delete button → autosave + re-badge
    entry.querySelector('.entry-del-btn').addEventListener('click', function() {
        entry.remove();
        updateEmptyState();
        refreshSetBadges();
        scheduleAutosave();
    });

    container.appendChild(entry);
    refreshSetBadges();
    updateEmptyState();
    scheduleAutosave();
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
        if (badge) badge.textContent = 'S' + counters[name];
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
}

function updateEmptyState() {
    var hasEntries = document.getElementById('todayEx').children.length > 0;
    document.getElementById('emptyState').style.display = hasEntries ? 'none' : '';
}

function setFormContent(sets, date) {
    window.sessionStorage.setItem("today", date);
    today = date;
    document.getElementById('todayEx').innerHTML = "";
    updateEmptyState();
    document.getElementById("formDate").value = date;
    document.getElementById("realDate").value = date;

    if (sets) {
        for (var i = 0; i < sets.length; i++) {
            if (sets[i].Date == date) {
                // fromPicker=false: loading saved data, no auto-fill override
                addExercise(sets[i].Name, sets[i].Weight, sets[i].Reps, sets[i].WorkoutColor, false);
            }
        }
    }
}

function setFormDate(sets) {
    var date = window.sessionStorage.getItem("today");
    if (!date) {
        date = new Date().toISOString().split('T')[0];
    }
    setFormContent(sets, date);
}

function goToToday() {
    var date = new Date().toISOString().split('T')[0];
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

function addAllGroup(exs, gr) {
    if (!exs) return;
    for (var i = 0; i < exs.length; i++) {
        if (exs[i].Group == gr) {
            addExercise(exs[i].Name, exs[i].Weight, exs[i].Reps, exs[i].Color, true);
        }
    }
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
        items.push({ label: dayLetters[d.getDay()], active: hasWorkout, today: isToday });
    }
    var dotsHtml = items.map(function(item) {
        var dotCls = 'week-dot' + (item.active ? ' has-workout' : '') + (item.today ? ' is-today' : '');
        var lblCls = 'week-dot-label' + (item.today ? ' is-today' : '');
        return '<div class="week-dot-item"><div class="' + dotCls + '"></div><span class="' + lblCls + '">' + item.label + '</span></div>';
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
