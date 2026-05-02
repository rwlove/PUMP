var currentPeriod = 'weekly';
var distributionChart = null;
var _volumeChart = null;
var _selectedVolumeExercise = null;

// ─── Period helpers ───────────────────────────────────────────────────────────

function getPeriodDates(period) {
    const end = new Date();
    const start = new Date(end);
    if (period === 'weekly')       start.setDate(end.getDate() - 7);
    else if (period === 'monthly') start.setMonth(end.getMonth() - 1);
    else if (period === 'annual')  start.setFullYear(end.getFullYear() - 1);
    else if (period === 'alltime') start.setFullYear(2000); // effectively all data
    return { start, end };
}

function formatDate(date) {
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function filterByPeriod(sets, period) {
    if (period === 'alltime') return sets ? sets.slice() : [];
    const { start, end } = getPeriodDates(period);
    return sets.filter(s => { const d = new Date(s.Date); return d >= start && d <= end; });
}

// ─── Global period selector ───────────────────────────────────────────────────

function setGlobalPeriod(period) {
    currentPeriod = period;

    // Update button active states
    document.querySelectorAll('#global-period-btns .btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.period === period);
    });

    // Update period range label
    const rangeEl = document.getElementById('period-range');
    if (rangeEl) {
        if (period === 'alltime') {
            rangeEl.textContent = 'All time';
        } else {
            const { start, end } = getPeriodDates(period);
            rangeEl.textContent = `${formatDate(start)} \u2013 ${formatDate(end)}`;
        }
    }

    // Always refresh Overview
    const sets = filterByPeriod(window.currentSets, period);
    updateSummaryDisplay(sets);
    updateExerciseDistribution(sets, window.exercises);

    // Refresh Weight Moved if visible
    const actTab = document.getElementById('tab-activity');
    if (actTab && actTab.classList.contains('show')) {
        renderVolumeExerciseButtons(window.currentSets, window.exercises, period);
    }

    // Refresh Body Weight if visible
    const wtTab = document.getElementById('tab-weight');
    if (wtTab && wtTab.classList.contains('show')) {
        generateWeightChart(filterWeightByPeriod(window._allWeight, period), window._chartColor, 0, 'stats-body-weight');
    }

    // Refresh Personal Records if visible (updates NEW badge)
    const prsTab = document.getElementById('tab-prs');
    if (prsTab && prsTab.classList.contains('show')) {
        updatePRsTab(window.currentSets, window.exercises, period);
    }

    // Refresh Progressive Overload if visible
    const overloadTab = document.getElementById('tab-overload');
    if (overloadTab && overloadTab.classList.contains('show')) {
        refreshOverloadTab(window.currentSets, window.exercises, period);
    }

    // Refresh Muscle Balance if visible
    const balanceTab = document.getElementById('tab-balance');
    if (balanceTab && balanceTab.classList.contains('show')) {
        updateBalanceTab(window.currentSets, window.exercises, period);
    }
    // Consistency and Recovery are period-independent; no refresh needed here.
}

// ─── Period helpers for non-set data ─────────────────────────────────────────

function filterWeightByPeriod(weight, period) {
    if (!weight) return [];
    if (period === 'alltime') return weight.slice();
    const { start, end } = getPeriodDates(period);
    return weight.filter(w => { const d = new Date(w.Date); return d >= start && d <= end; });
}

// ─── Summary stats ────────────────────────────────────────────────────────────

function calculateSummaryStats(sets) {
    const counts = {};
    sets.forEach(s => { counts[s.Name] = (counts[s.Name] || 0) + 1; });

    let mostCommon = '-', leastCommon = '-', maxC = 0, minC = Infinity;
    for (const [name, c] of Object.entries(counts)) {
        if (c > maxC) { mostCommon = name; maxC = c; }
        if (c < minC) { leastCommon = name; minC = c; }
    }
    if (minC === Infinity) leastCommon = '-';

    const uniqueDates = new Set(sets.map(s => s.Date)).size;

    return { totalSets: sets.length, activeDays: uniqueDates, mostCommon, leastCommon };
}

function updateSummaryDisplay(sets) {
    const stats = calculateSummaryStats(sets);
    document.getElementById('total-sets').textContent   = stats.totalSets;
    document.getElementById('active-days').textContent  = stats.activeDays;
    document.getElementById('most-common').textContent  = stats.mostCommon;
    document.getElementById('least-common').textContent = stats.leastCommon;
}

// ─── Exercise distribution pie ────────────────────────────────────────────────

function updateExerciseDistribution(sets, exercises) {
    const counts = {};
    sets.forEach(s => { counts[s.Name] = (counts[s.Name] || 0) + 1; });
    const labels = Object.keys(counts);
    const data   = Object.values(counts);

    const colorMap = {};
    (exercises || []).forEach(ex => { colorMap[ex.Name] = ex.Color; });
    const colors = labels.map(l => colorMap[l] || '#ccc');

    const ctx = document.getElementById('exercise-distribution');
    if (!ctx) return;

    if (distributionChart) distributionChart.destroy();
    distributionChart = new Chart(ctx, {
        type: 'pie',
        data: { labels, datasets: [{ data, backgroundColor: colors }] },
        options: {
            responsive: true,
            plugins: {
                legend: { position: 'right', labels: { font: { size: 12 } } },
                tooltip: {
                    callbacks: {
                        label(context) {
                            const total = context.dataset.data.reduce((a, b) => a + b, 0);
                            const pct = ((context.raw / total) * 100).toFixed(1);
                            return `${context.label}: ${context.raw} (${pct}%)`;
                        }
                    }
                }
            }
        }
    });
}

// ─── Volume chart exercise buttons ───────────────────────────────────────────

// Render clickable exercise buttons for the Weight Moved tab.
// Only shows exercises performed within the period, sorted by frequency (most used first).
function renderVolumeExerciseButtons(allSets, exercises, period) {
    const container = document.getElementById('volume-ex-buttons');
    const noExEl    = document.getElementById('volume-no-exercises');
    if (!container) return;

    const periodSets = filterByPeriod(allSets, period);

    // Count frequency per exercise within this period
    const counts = {};
    periodSets.forEach(s => { counts[s.Name] = (counts[s.Name] || 0) + 1; });

    const names = Object.keys(counts).sort((a, b) => counts[b] - counts[a]);

    container.innerHTML = '';

    if (names.length === 0) {
        if (noExEl) noExEl.style.display = '';
        // Clear chart
        if (_volumeChart) { _volumeChart.destroy(); _volumeChart = null; }
        return;
    }
    if (noExEl) noExEl.style.display = 'none';

    // Build color map from exercise definitions
    const colorMap = {};
    (exercises || []).forEach(ex => { colorMap[ex.Name] = ex.Color; });

    // Pick initial selection: keep current if still in list, else pick first
    if (!_selectedVolumeExercise || !counts[_selectedVolumeExercise]) {
        _selectedVolumeExercise = names[0];
    }

    names.forEach(name => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'volume-ex-btn' + (name === _selectedVolumeExercise ? ' active' : '');
        btn.textContent = name;

        // Left accent stripe using the exercise color
        const exColor = colorMap[name] || '#6c757d';
        btn.style.setProperty('--ex-color', exColor);

        btn.addEventListener('click', function() {
            _selectedVolumeExercise = name;
            container.querySelectorAll('.volume-ex-btn').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            updateVolumeChart(allSets, exercises, period, name);
        });

        container.appendChild(btn);
    });

    // Draw chart for the selected exercise
    updateVolumeChart(allSets, exercises, period, _selectedVolumeExercise);
}

// ─── Volume chart (Weight Moved tab) ─────────────────────────────────────────

// Parse a 6-digit hex color string into {r, g, b}.
function hexToRgb(hex) {
    const h = hex.replace('#', '');
    return {
        r: parseInt(h.substring(0, 2), 16),
        g: parseInt(h.substring(2, 4), 16),
        b: parseInt(h.substring(4, 6), 16),
    };
}

// Blend a color toward white by factor t (0 = original, 1 = white).
function blendToWhite(hex, t) {
    const { r, g, b } = hexToRgb(hex);
    const rr = Math.round(r + (255 - r) * t);
    const gg = Math.round(g + (255 - g) * t);
    const bb = Math.round(b + (255 - b) * t);
    return `rgb(${rr},${gg},${bb})`;
}

// Return the most recent body weight value (lbs) on or before `dateStr`,
// or null if none is available.
function getBodyWeightOnOrBefore(dateStr) {
    const allW = window._allWeight;
    if (!allW || allW.length === 0) return null;
    // _allWeight entries have a Date string (YYYY-MM-DD) and a Weight value.
    // Walk backwards through sorted entries to find the closest past entry.
    const sorted = allW.slice().sort((a, b) => a.Date < b.Date ? -1 : a.Date > b.Date ? 1 : 0);
    let best = null;
    for (let i = 0; i < sorted.length; i++) {
        if (sorted[i].Date <= dateStr) best = sorted[i];
        else break;
    }
    return best ? parseFloat(best.Weight) : null;
}

function updateVolumeChart(sets, exercises, period, exerciseName) {
    const { start, end } = getPeriodDates(period);
    const filtered = sets.filter(s => {
        if (s.Name !== exerciseName) return false;
        const d = new Date(s.Date);
        return d >= start && d <= end;
    });

    // Determine whether this exercise belongs to a "legs" group so we can
    // substitute missing/zero weight with the nearest body weight entry.
    const groupMap = {};
    (exercises || []).forEach(ex => { groupMap[ex.Name] = (ex.Group || '').toLowerCase(); });
    const isLegs = (groupMap[exerciseName] || '').includes('leg');

    // Group by date, preserving set order within each day.
    const setsByDate = {};   // date → [{weight, reps, vol, bodyWeightUsed}, ...]

    filtered.forEach(s => {
        let w = parseFloat(s.Weight);
        let bodyWeightUsed = false;
        // For leg exercises with no weight (0, NaN, blank), fall back to body weight.
        if (isLegs && (isNaN(w) || w === 0)) {
            const bw = getBodyWeightOnOrBefore(s.Date);
            if (bw !== null) { w = bw; bodyWeightUsed = true; }
        }
        const r   = parseInt(s.Reps, 10);
        const vol = (!isNaN(w) && !isNaN(r)) ? w * r : 0;
        if (!setsByDate[s.Date]) setsByDate[s.Date] = [];
        setsByDate[s.Date].push({ weight: w, reps: r, vol, bodyWeightUsed });
    });

    const dates   = Object.keys(setsByDate).sort();
    const maxSets = dates.reduce((m, d) => Math.max(m, setsByDate[d].length), 0);

    const noDataEl = document.getElementById('volume-no-data');
    const ctx = document.getElementById('volume-chart');
    if (!ctx) return;

    if (_volumeChart) { _volumeChart.destroy(); _volumeChart = null; }

    if (dates.length === 0) {
        if (noDataEl) noDataEl.style.display = '';
        return;
    }
    if (noDataEl) noDataEl.style.display = 'none';

    // Use exercise's own color so bars match the distribution chart
    const colorMap = {};
    (exercises || []).forEach(ex => { colorMap[ex.Name] = ex.Color; });
    const color = colorMap[exerciseName] || window._chartColor || '#2780e3';

    // Build one dataset per set position (index 0 = first/bottom/darkest).
    // Lightness blend: set i out of N total → t = (i / (N-1)) * MAX_T
    // MAX_T = 0.55 keeps even the lightest shade visibly colored.
    const MAX_T = 0.55;

    const datasets = [];
    for (let i = 0; i < maxSets; i++) {
        const t    = maxSets === 1 ? 0 : (i / (maxSets - 1)) * MAX_T;
        const fill = blendToWhite(color, t);
        // Border: slightly darker than fill — use half the lightening
        const border = blendToWhite(color, t * 0.4);

        // For each date, use the i-th set's volume (null if that day has fewer sets).
        const data = dates.map(d => {
            const daySets = setsByDate[d];
            return (i < daySets.length) ? daySets[i].vol : null;
        });

        datasets.push({
            label: `Set ${i + 1}`,
            data,
            backgroundColor: fill,
            borderColor: border,
            borderWidth: 1,
            // Only round the top corners of the topmost non-null segment.
            // Chart.js applies borderRadius per-dataset; we enable it only on the last.
            borderRadius: (i === maxSets - 1) ? { topLeft: 4, topRight: 4 } : 0,
            borderSkipped: false,
            stack: 'sets',
        });
    }

    _volumeChart = new Chart(ctx, {
        type: 'bar',
        data: { labels: dates, datasets },
        options: {
            responsive: true,
            scales: {
                x: { stacked: true, grid: { display: false } },
                y: {
                    stacked: true,
                    beginAtZero: true,
                    grid: { display: false },
                    ticks: {
                        callback(v) { return v.toLocaleString() + ' lbs'; }
                    }
                }
            },
            plugins: {
                legend: { display: false },
                tooltip: {
                    mode: 'index',
                    callbacks: {
                        // Title: the date
                        title(items) { return items[0] ? items[0].label : ''; },
                        // One line per set: "Set N: W×R = V lbs"
                        label(ctx) {
                            const i       = ctx.datasetIndex;
                            const date    = ctx.label;
                            const daySets = setsByDate[date] || [];
                            if (i >= daySets.length) return null; // skip null entries
                            const s    = daySets[i];
                            const w    = isNaN(s.weight) ? '?' : s.weight;
                            const r    = isNaN(s.reps)   ? '?' : s.reps;
                            const vol  = s.vol.toLocaleString(undefined, { maximumFractionDigits: 1 });
                            const note = s.bodyWeightUsed ? ' (BW)' : '';
                            return `Set ${i + 1}: ${w}×${r}${note} = ${vol} lbs`;
                        },
                        // Footer: total volume for the day
                        footer(items) {
                            const date  = items[0] ? items[0].label : null;
                            if (!date) return '';
                            const total = (setsByDate[date] || []).reduce((s, x) => s + x.vol, 0);
                            return `Total: ${total.toLocaleString(undefined, { maximumFractionDigits: 1 })} lbs`;
                        }
                    }
                }
            }
        }
    });
}

function refreshVolumeChart() {
    renderVolumeExerciseButtons(window.currentSets, window.exercises, currentPeriod);
}

// ─── Personal Records ─────────────────────────────────────────────────────────

function escapeHtml(str) {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function computePRs(allSets) {
    // Per exercise: highest estimated 1RM set (Epley: w * (1 + r/30))
    const prs = {};
    allSets.forEach(s => {
        const w = parseFloat(s.Weight);
        const r = parseInt(s.Reps, 10);
        if (isNaN(w) || w <= 0 || isNaN(r) || r <= 0) return;
        const e1rm = w * (1 + r / 30);
        if (!prs[s.Name] || e1rm > prs[s.Name].e1rm) {
            prs[s.Name] = { weight: w, reps: r, e1rm, date: s.Date };
        }
    });
    return prs;
}

function updatePRsTab(allSets, exercises, period) {
    const prs = computePRs(allSets);
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });

    // Period start for NEW badge (null = alltime)
    const periodStart = (period !== 'alltime') ? getPeriodDates(period).start : null;

    // Group by muscle group
    const byGroup = {};
    for (const [name, pr] of Object.entries(prs)) {
        const group = (exMap[name] || {}).Group || 'Other';
        if (!byGroup[group]) byGroup[group] = [];
        byGroup[group].push({ name, ...pr });
    }

    const container = document.getElementById('prs-table-container');
    if (!container) return;

    if (Object.keys(prs).length === 0) {
        container.innerHTML = '<p class="text-center text-body-secondary py-4">No weighted sets logged yet.</p>';
        return;
    }

    let html = '';
    Object.keys(byGroup).sort().forEach(group => {
        const rows = byGroup[group].sort((a, b) => b.e1rm - a.e1rm);
        html += `<div class="pr-group-header">${escapeHtml(group)}</div>`;
        rows.forEach(row => {
            const ex = exMap[row.name] || {};
            const color = ex.Color || '#6c757d';
            const isNew = periodStart && new Date(row.date) >= periodStart;
            const badge = isNew ? ' <span class="pr-new-badge">NEW</span>' : '';
            html += `<div class="pr-row">
              <div class="pr-dot" style="background:${color}"></div>
              <div class="pr-name">${escapeHtml(row.name)}${badge}</div>
              <div class="pr-stats">
                <div class="pr-stat"><div class="pr-val">${row.weight} lbs</div><div class="pr-lbl">best weight</div></div>
                <div class="pr-stat"><div class="pr-val">${row.reps}</div><div class="pr-lbl">reps</div></div>
                <div class="pr-stat"><div class="pr-val">${Math.round(row.e1rm).toLocaleString()} lbs</div><div class="pr-lbl">est. 1RM</div></div>
                <div class="pr-stat pr-date-stat"><div class="pr-val">${row.date}</div><div class="pr-lbl">set on</div></div>
              </div>
            </div>`;
        });
    });
    container.innerHTML = html;

    const noteEl = document.getElementById('prs-period-note');
    if (noteEl) {
        noteEl.textContent = (periodStart && period !== 'alltime')
            ? `NEW = set since ${formatDate(periodStart)}`
            : '';
    }
}

// ─── Progressive Overload ─────────────────────────────────────────────────────

var _overloadChart = null;
var _selectedOverloadExercise = null;

function refreshOverloadTab(allSets, exercises, period) {
    const container = document.getElementById('overload-ex-buttons');
    if (!container) return;

    const exWithWeight = new Set();
    const counts = {};
    allSets.forEach(s => {
        counts[s.Name] = (counts[s.Name] || 0) + 1;
        const w = parseFloat(s.Weight);
        if (!isNaN(w) && w > 0) exWithWeight.add(s.Name);
    });

    const names = [...exWithWeight].sort((a, b) => (counts[b] || 0) - (counts[a] || 0));
    const colorMap = {};
    (exercises || []).forEach(ex => { colorMap[ex.Name] = ex.Color; });

    if (!_selectedOverloadExercise || !exWithWeight.has(_selectedOverloadExercise)) {
        _selectedOverloadExercise = names[0] || null;
    }

    container.innerHTML = '';
    names.forEach(name => {
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'volume-ex-btn' + (name === _selectedOverloadExercise ? ' active' : '');
        btn.textContent = name;
        btn.style.setProperty('--ex-color', colorMap[name] || '#6c757d');
        btn.addEventListener('click', function () {
            _selectedOverloadExercise = name;
            container.querySelectorAll('.volume-ex-btn').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            renderOverloadChart(allSets, exercises, period, name);
        });
        container.appendChild(btn);
    });

    if (_selectedOverloadExercise) {
        renderOverloadChart(allSets, exercises, period, _selectedOverloadExercise);
    }
}

function renderOverloadChart(allSets, exercises, period, exerciseName) {
    const ctx = document.getElementById('overload-chart');
    const noDataEl = document.getElementById('overload-no-data');
    if (!ctx) return;

    const periodSets = filterByPeriod(allSets, period);
    const filtered = periodSets.filter(s => {
        if (s.Name !== exerciseName) return false;
        const w = parseFloat(s.Weight);
        return !isNaN(w) && w > 0;
    });

    // Best e1RM per day
    const byDate = {};
    filtered.forEach(s => {
        const w = parseFloat(s.Weight);
        const r = parseInt(s.Reps, 10);
        if (isNaN(r) || r <= 0) return;
        const e1rm = w * (1 + r / 30);
        if (!byDate[s.Date] || e1rm > byDate[s.Date].e1rm) {
            byDate[s.Date] = { weight: w, reps: r, e1rm };
        }
    });

    const dates = Object.keys(byDate).sort();

    if (_overloadChart) { _overloadChart.destroy(); _overloadChart = null; }

    if (dates.length === 0) {
        if (noDataEl) noDataEl.style.display = '';
        return;
    }
    if (noDataEl) noDataEl.style.display = 'none';

    const colorMap = {};
    (exercises || []).forEach(ex => { colorMap[ex.Name] = ex.Color; });
    const color = colorMap[exerciseName] || window._chartColor || '#2780e3';
    const fadedColor = blendToWhite(color, 0.45);

    _overloadChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels: dates,
            datasets: [
                {
                    label: 'Est. 1RM (Epley)',
                    data: dates.map(d => Math.round(byDate[d].e1rm)),
                    borderColor: color,
                    backgroundColor: color + '22',
                    borderWidth: 2.5,
                    pointRadius: 4,
                    pointBackgroundColor: color,
                    tension: 0.3,
                    fill: true,
                },
                {
                    label: 'Top Set Weight',
                    data: dates.map(d => byDate[d].weight),
                    borderColor: fadedColor,
                    borderWidth: 1.5,
                    borderDash: [5, 4],
                    pointRadius: 2,
                    pointBackgroundColor: fadedColor,
                    tension: 0.3,
                    fill: false,
                }
            ]
        },
        options: {
            responsive: true,
            scales: {
                x: { grid: { display: false } },
                y: {
                    beginAtZero: false,
                    grid: { display: false },
                    ticks: { callback(v) { return v + ' lbs'; } }
                }
            },
            plugins: {
                legend: { display: true, labels: { font: { size: 12 }, boxWidth: 20 } },
                tooltip: {
                    callbacks: {
                        label(ctx) {
                            return ctx.datasetIndex === 0
                                ? `Est. 1RM: ${ctx.raw} lbs`
                                : `Top set: ${ctx.raw} lbs`;
                        }
                    }
                }
            }
        }
    });
}

// ─── Muscle Group Balance ─────────────────────────────────────────────────────

var _balanceChart = null;

function updateBalanceTab(allSets, exercises, period) {
    const filtered = filterByPeriod(allSets, period);
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });

    const groupVolume = {};
    const groupSets = {};
    filtered.forEach(s => {
        const ex = exMap[s.Name];
        if (!ex) return;
        const group = ex.Group || 'Other';
        const w = parseFloat(s.Weight);
        const r = parseInt(s.Reps, 10);
        groupSets[group] = (groupSets[group] || 0) + 1;
        if (!isNaN(w) && w > 0 && !isNaN(r) && r > 0) {
            groupVolume[group] = (groupVolume[group] || 0) + w * r;
        }
    });

    const ctx = document.getElementById('balance-chart');
    const noDataEl = document.getElementById('balance-no-data');
    if (!ctx) return;

    if (_balanceChart) { _balanceChart.destroy(); _balanceChart = null; }

    const groups = Object.keys(groupSets).sort();
    if (groups.length === 0) {
        if (noDataEl) noDataEl.style.display = '';
        document.getElementById('balance-summary').innerHTML = '';
        return;
    }
    if (noDataEl) noDataEl.style.display = 'none';

    const primaryColor = window._chartColor || '#2780e3';
    const labelColor = getComputedStyle(document.documentElement).getPropertyValue('--bs-body-color').trim() || '#e0e0e0';

    _balanceChart = new Chart(ctx, {
        type: 'radar',
        data: {
            labels: groups,
            datasets: [{
                label: 'Volume',
                data: groups.map(g => Math.round(groupVolume[g] || 0)),
                backgroundColor: primaryColor + '33',
                borderColor: primaryColor,
                borderWidth: 2,
                pointBackgroundColor: primaryColor,
                pointRadius: 4,
                pointHoverRadius: 6,
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                r: {
                    beginAtZero: true,
                    ticks: { display: false },
                    pointLabels: { font: { size: 13 }, color: labelColor }
                }
            },
            plugins: {
                legend: { display: false },
                tooltip: {
                    callbacks: {
                        label(ctx) {
                            const g = ctx.label;
                            const sets = groupSets[g] || 0;
                            const vol = ctx.raw
                                ? `${ctx.raw.toLocaleString()} lbs`
                                : '— lbs';
                            return `${vol}  ·  ${sets} set${sets === 1 ? '' : 's'}`;
                        }
                    }
                }
            }
        }
    });

    const summaryEl = document.getElementById('balance-summary');
    if (summaryEl) {
        summaryEl.innerHTML = groups.map(g => {
            const vol = groupVolume[g]
                ? `${Math.round(groupVolume[g]).toLocaleString()} lbs`
                : '— lbs';
            return `<div class="col-6 col-sm-4">
              <div class="p-3 rounded-3 bg-body-secondary text-center">
                <div class="stat-label">${escapeHtml(g)}</div>
                <div class="fw-bold">${groupSets[g]} set${groupSets[g] === 1 ? '' : 's'}</div>
                <div class="stat-label">${vol}</div>
              </div>
            </div>`;
        }).join('');
    }
}

// ─── Training Consistency ─────────────────────────────────────────────────────

function getTodayStr() {
    return window._serverDate || new Date().toLocaleDateString('en-CA');
}

function parseDateStr(str) {
    const [y, m, d] = str.split('-').map(Number);
    return new Date(y, m - 1, d);
}

function updateConsistencyTab(allSets) {
    const workoutDates = {};
    allSets.forEach(s => {
        workoutDates[s.Date] = (workoutDates[s.Date] || 0) + 1;
    });

    const todayStr = getTodayStr();
    const today = parseDateStr(todayStr);

    // Current streak (consecutive days back from today)
    let currentStreak = 0;
    for (let i = 0; i <= 730; i++) {
        const d = new Date(today);
        d.setDate(today.getDate() - i);
        const ds = d.toLocaleDateString('en-CA');
        if (workoutDates[ds]) {
            currentStreak++;
        } else if (i > 0) {
            break;
        }
    }

    // Longest streak (all time)
    const sortedDates = Object.keys(workoutDates).sort();
    let longestStreak = sortedDates.length > 0 ? 1 : 0;
    let streak = longestStreak;
    for (let i = 1; i < sortedDates.length; i++) {
        const diff = Math.round(
            (parseDateStr(sortedDates[i]) - parseDateStr(sortedDates[i - 1])) / 86400000
        );
        if (diff === 1) {
            streak++;
            if (streak > longestStreak) longestStreak = streak;
        } else {
            streak = 1;
        }
    }

    // Total days & avg/week over last year
    const yearAgoStr = new Date(today.getTime() - 364 * 86400000).toLocaleDateString('en-CA');
    const daysLastYear = sortedDates.filter(d => d >= yearAgoStr).length;
    const avgPerWeek = (daysLastYear / 52).toFixed(1);

    document.getElementById('consistency-streak').textContent = currentStreak;
    document.getElementById('consistency-longest').textContent = longestStreak;
    document.getElementById('consistency-total').textContent = sortedDates.length;
    document.getElementById('consistency-avg').textContent = avgPerWeek;

    // Build heatmap
    const grid = document.getElementById('heatmap-grid');
    if (!grid) return;

    const { r: pr, g: pg, b: pb } = hexToRgb(window._chartColor || '#2780e3');

    // Grid starts on the Sunday on or before (today - 364 days)
    const gridStart = new Date(today);
    gridStart.setDate(today.getDate() - 364);
    gridStart.setDate(gridStart.getDate() - gridStart.getDay()); // back to Sunday

    const numWeeks = 53;

    const inner = document.createElement('div');
    inner.className = 'heatmap-inner';

    // Day-of-week labels column
    const dayLabels = document.createElement('div');
    dayLabels.className = 'heatmap-day-labels';
    ['', 'Mon', '', 'Wed', '', 'Fri', ''].forEach(lbl => {
        const el = document.createElement('div');
        el.className = 'heatmap-day-label';
        el.textContent = lbl;
        dayLabels.appendChild(el);
    });
    inner.appendChild(dayLabels);

    // Right side: month labels + week columns
    const rightSide = document.createElement('div');
    rightSide.style.display = 'flex';
    rightSide.style.flexDirection = 'column';

    // Month labels
    const monthRow = document.createElement('div');
    monthRow.className = 'heatmap-month-row';
    const CELL_PX = 13; // cell (10px) + gap (3px)
    let lastMonth = -1;
    for (let w = 0; w < numWeeks; w++) {
        const weekDate = new Date(gridStart);
        weekDate.setDate(gridStart.getDate() + w * 7);
        const mo = weekDate.getMonth();
        if (mo !== lastMonth) {
            const lbl = document.createElement('span');
            lbl.className = 'heatmap-month-label';
            lbl.textContent = weekDate.toLocaleDateString(undefined, { month: 'short' });
            lbl.style.left = (w * CELL_PX) + 'px';
            monthRow.appendChild(lbl);
            lastMonth = mo;
        }
    }
    rightSide.appendChild(monthRow);

    // Weeks
    const weeksEl = document.createElement('div');
    weeksEl.className = 'heatmap-weeks';

    for (let w = 0; w < numWeeks; w++) {
        const col = document.createElement('div');
        col.className = 'heatmap-week';

        for (let d = 0; d < 7; d++) {
            const cellDate = new Date(gridStart);
            cellDate.setDate(gridStart.getDate() + w * 7 + d);
            const ds = cellDate.toLocaleDateString('en-CA');
            const count = workoutDates[ds] || 0;
            const isFuture = ds > todayStr;

            const cell = document.createElement('div');
            cell.className = 'heatmap-cell';
            if (ds === todayStr) cell.classList.add('heatmap-today');

            if (!isFuture && count > 0) {
                const opacity = count <= 2 ? 0.4 : count <= 4 ? 0.62 : count <= 7 ? 0.82 : 1.0;
                cell.style.background = `rgba(${pr},${pg},${pb},${opacity})`;
                cell.style.borderColor = `rgba(${pr},${pg},${pb},${Math.min(1, opacity * 1.3)})`;
            } else if (isFuture) {
                cell.classList.add('heatmap-future');
            }

            cell.title = isFuture ? ds : `${ds}: ${count} set${count !== 1 ? 's' : ''}`;
            col.appendChild(cell);
        }
        weeksEl.appendChild(col);
    }
    rightSide.appendChild(weeksEl);
    inner.appendChild(rightSide);

    grid.innerHTML = '';
    grid.appendChild(inner);

    // Update legend cells with actual colors
    ['heatmap-l1','heatmap-l2','heatmap-l3','heatmap-l4'].forEach((cls, i) => {
        const opacity = [0.4, 0.62, 0.82, 1.0][i];
        document.querySelectorAll('.' + cls).forEach(el => {
            el.style.background = `rgba(${pr},${pg},${pb},${opacity})`;
        });
    });
}

// ─── Rest & Recovery ─────────────────────────────────────────────────────────

function updateRecoveryTab(allSets, exercises) {
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });

    const lastDate = {};
    const sessionDates = {};
    allSets.forEach(s => {
        if (!lastDate[s.Name] || s.Date > lastDate[s.Name]) lastDate[s.Name] = s.Date;
        if (!sessionDates[s.Name]) sessionDates[s.Name] = new Set();
        sessionDates[s.Name].add(s.Date);
    });

    const todayStr = getTodayStr();
    const today = parseDateStr(todayStr);

    const rows = Object.keys(lastDate).map(name => {
        const last = lastDate[name];
        const daysSince = Math.round((today - parseDateStr(last)) / 86400000);

        const dates = [...sessionDates[name]].sort();
        let avgInterval = null;
        if (dates.length >= 2) {
            let total = 0;
            for (let i = 1; i < dates.length; i++) {
                total += Math.round((parseDateStr(dates[i]) - parseDateStr(dates[i - 1])) / 86400000);
            }
            avgInterval = total / (dates.length - 1);
        }
        return { name, last, daysSince, avgInterval };
    });

    // Sort: most overdue first
    rows.sort((a, b) => {
        const ra = a.avgInterval ? a.daysSince / a.avgInterval : a.daysSince / 7;
        const rb = b.avgInterval ? b.daysSince / b.avgInterval : b.daysSince / 7;
        return rb - ra;
    });

    const container = document.getElementById('recovery-table');
    if (!container) return;

    if (rows.length === 0) {
        container.innerHTML = '<p class="text-center text-body-secondary py-4">No exercises logged yet.</p>';
        return;
    }

    let html = `<table class="w-100 recovery-tbl">
      <thead><tr>
        <th class="recovery-th"></th>
        <th class="recovery-th">Exercise</th>
        <th class="recovery-th">Last Performed</th>
        <th class="recovery-th">Days Since</th>
        <th class="recovery-th">Typical Interval</th>
        <th class="recovery-th">Status</th>
      </tr></thead><tbody>`;

    rows.forEach(row => {
        const ex = exMap[row.name] || {};
        const color = ex.Color || '#6c757d';
        const group = ex.Group || '';

        let status, statusClass;
        if (row.avgInterval !== null) {
            const ratio = row.daysSince / row.avgInterval;
            if (ratio < 0.6)      { status = 'Fresh';   statusClass = 'recovery-ok'; }
            else if (ratio < 1.3) { status = 'Due';     statusClass = 'recovery-due'; }
            else                   { status = 'Overdue'; statusClass = 'recovery-overdue'; }
        } else {
            status = `${row.daysSince}d ago`;
            statusClass = 'recovery-neutral';
        }

        const intervalStr = row.avgInterval !== null ? `Every ${Math.round(row.avgInterval)}d` : '—';

        html += `<tr class="recovery-row">
          <td class="recovery-td"><div class="pr-dot" style="background:${color}"></div></td>
          <td class="recovery-td">
            <div style="font-weight:500;font-size:0.875rem">${escapeHtml(row.name)}</div>
            <div class="stat-label">${escapeHtml(group)}</div>
          </td>
          <td class="recovery-td">${row.last}</td>
          <td class="recovery-td">${row.daysSince}d</td>
          <td class="recovery-td">${intervalStr}</td>
          <td class="recovery-td"><span class="recovery-badge ${statusClass}">${status}</span></td>
        </tr>`;
    });

    html += '</tbody></table>';
    container.innerHTML = html;
}
