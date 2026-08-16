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
    // Compare date strings, not Date objects. s.Date is "YYYY-MM-DD"; new
    // Date(s.Date) parses it as UTC midnight, so in a western timezone the
    // window's boundary day shifts to the previous local day and its sets
    // silently drop out of every strength stat. localDateStr keeps both ends in
    // local wall-clock — the same fix parseDateStr applies elsewhere.
    const startStr = localDateStr(start), endStr = localDateStr(end);
    return sets.filter(s => s.Date >= startStr && s.Date <= endStr);
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

    // Refresh Body Weight (chart + log) if visible
    const wtTab = document.getElementById('tab-weight');
    if (wtTab && wtTab.classList.contains('show')) {
        generateWeightChart(filterWeightByPeriod(window._allWeight, period), window._chartColor, 'stats-body-weight');
        if (window.renderWeightTable) {
            renderWeightTable(filterWeightByPeriod(window._allWeight, period), window._pageStep);
        }
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

    // Refresh Rest & Recovery if visible (filters out exercises not performed in the period)
    const recoveryTab = document.getElementById('tab-recovery');
    if (recoveryTab && recoveryTab.classList.contains('show')) {
        updateRecoveryTab(window.currentSets, window.exercises, period);
    }

    // Refresh Consistency if visible (stat boxes + heatmap both scale to the period)
    const consistencyTab = document.getElementById('tab-consistency');
    if (consistencyTab && consistencyTab.classList.contains('show')) {
        updateConsistencyTab(window.currentSets, period);
    }

    // Refresh wearable tabs (Health Connect) if visible — render fns live in health-charts.js
    [['tab-steps', 'renderStepsTab'], ['tab-hr', 'renderHRTab'], ['tab-sleep', 'renderSleepTab'], ['tab-cardio', 'renderCardioTab']].forEach(function(p) {
        const tab = document.getElementById(p[0]);
        if (tab && tab.classList.contains('show') && typeof window[p[1]] === 'function') {
            window[p[1]](period);
        }
    });
}

// ─── Period helpers for non-set data ─────────────────────────────────────────

function filterWeightByPeriod(weight, period) {
    if (!weight) return [];
    if (period === 'alltime') return weight.slice();
    const { start, end } = getPeriodDates(period);
    // Date-string compare, same reason as filterByPeriod: avoid the UTC-parse
    // boundary shift on the "YYYY-MM-DD" w.Date.
    const startStr = localDateStr(start), endStr = localDateStr(end);
    return weight.filter(w => w.Date >= startStr && w.Date <= endStr);
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
        // A mid-grey slice outline separates adjacent slices and, critically,
        // keeps a dark/near-black exercise color visible against the dark
        // theme background — without it such a slice reads as a hole in the pie.
        data: { labels, datasets: [{ data, backgroundColor: colors, borderColor: 'rgba(128,128,128,0.6)', borderWidth: 1.5 }] },
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
let _sortedWeightSrc = null;
let _sortedWeightCache = null;
function getBodyWeightOnOrBefore(dateStr) {
    const allW = window._allWeight;
    if (!allW || allW.length === 0) return null;
    // _allWeight entries have a Date string (YYYY-MM-DD) and a Weight value.
    // Sort once per source array (this runs inside per-set loops) and walk
    // through sorted entries to find the closest past entry.
    if (_sortedWeightSrc !== allW) {
        _sortedWeightSrc = allW;
        _sortedWeightCache = allW.slice().sort((a, b) => a.Date < b.Date ? -1 : a.Date > b.Date ? 1 : 0);
    }
    const sorted = _sortedWeightCache;
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

    // Bodyweight model (Feature 6): a bodyweight set carries AddedWeight (may be
    // 0), and its logged Weight is the body-weight snapshot from when it was
    // done. Total load = Weight + AddedWeight. Legacy zero-weight leg/bodyweight
    // sets predate the flag, so we still substitute body weight for those.
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });
    const exObj = exMap[exerciseName] || {};
    const isBodyweight = !!exObj.Bodyweight;
    const isLegs = ((exObj.Group || '').toLowerCase()).includes('leg');

    // Group by date, preserving set order within each day.
    const setsByDate = {};   // date → [{weight, reps, vol, bodyWeightUsed}, ...]

    filtered.forEach(s => {
        let w = parseFloat(s.Weight);
        let bodyWeightUsed = false;
        const hasAdded = s.AddedWeight !== undefined && s.AddedWeight !== null;
        if (hasAdded) {
            // New bodyweight set: Weight is the snapshot; add the extra load.
            if (isNaN(w)) w = 0;
            w += (parseFloat(s.AddedWeight) || 0);
            bodyWeightUsed = true;
        } else if ((isBodyweight || isLegs) && (isNaN(w) || w === 0)) {
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

    const container = document.getElementById('prs-table-container');
    if (!container) return;

    if (Object.keys(prs).length === 0) {
        container.innerHTML = '<p class="text-center text-body-secondary py-4">No weighted sets logged yet.</p>';
        return;
    }

    const rows = Object.entries(prs)
        .map(([name, pr]) => ({ name, ...pr }))
        .sort((a, b) => (b.date || '').localeCompare(a.date || ''));

    let html = '';
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
    const lastDate = {};
    allSets.forEach(s => {
        const w = parseFloat(s.Weight);
        if (!isNaN(w) && w > 0) exWithWeight.add(s.Name);
        if (!lastDate[s.Name] || s.Date > lastDate[s.Name]) lastDate[s.Name] = s.Date;
    });

    const names = [...exWithWeight].sort((a, b) => (lastDate[b] || '').localeCompare(lastDate[a] || ''));
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

// Muscle-balance metric: 'sets' (default — number of sets per group) or
// 'volume' (weight × reps). Toggled by the Sets/Volume buttons on the tab.
var balanceMetric = 'sets';

// Balance dimension: 'group' (by training group) or 'muscle' (per focus muscle,
// crediting each set to its exercise's muscles — primary ×1.0, secondary ×0.5).
var balanceDim = 'group';

function setBalanceMetric(metric) {
    balanceMetric = (metric === 'volume') ? 'volume' : 'sets';
    document.querySelectorAll('#tab-balance [data-metric]').forEach(b => {
        b.classList.toggle('active', b.dataset.metric === balanceMetric);
    });
    updateBalanceTab(window.currentSets, window.exercises, currentPeriod);
}

function setBalanceDim(dim) {
    balanceDim = (dim === 'muscle') ? 'muscle' : 'group';
    document.querySelectorAll('#tab-balance [data-dim]').forEach(b => {
        b.classList.toggle('active', b.dataset.dim === balanceDim);
    });
    updateBalanceTab(window.currentSets, window.exercises, currentPeriod);
}

function updateBalanceTab(allSets, exercises, period) {
    const filtered = filterByPeriod(allSets, period);
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });

    const groupVolume = {};
    const groupSets = {};
    // Muscle dimension credits each set to its exercise's focus muscles (primary
    // ×1.0, secondary ×0.5); group dimension counts whole sets per training group.
    const byMuscle = balanceDim === 'muscle' && window._exerciseMuscles;
    filtered.forEach(s => {
        const ex = exMap[s.Name];
        if (!ex) return;
        const w = parseFloat(s.Weight);
        const r = parseInt(s.Reps, 10);
        const vol = (!isNaN(w) && w > 0 && !isNaN(r) && r > 0) ? w * r : 0;
        if (byMuscle) {
            const muscles = window._exerciseMuscles[String(ex.ID)] || [];
            muscles.forEach(m => {
                const f = m.Primary ? 1.0 : 0.5;
                groupSets[m.Name] = (groupSets[m.Name] || 0) + f;
                if (vol > 0) groupVolume[m.Name] = (groupVolume[m.Name] || 0) + vol * f;
            });
        } else {
            const group = ex.Group || 'Other';
            if (group.toLowerCase() === 'cardio') return;
            groupSets[group] = (groupSets[group] || 0) + 1;
            if (vol > 0) groupVolume[group] = (groupVolume[group] || 0) + vol;
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

    const bySets = balanceMetric === 'sets';
    const noun = bySets ? 'Sets' : 'Volume';
    const metricVal = g => bySets ? (groupSets[g] || 0) : Math.round(groupVolume[g] || 0);
    const fmtSets = g => { const n = Math.round((groupSets[g] || 0) * 10) / 10; return `${n} set${n === 1 ? '' : 's'}`; };
    const fmtVol = g => groupVolume[g] ? `${Math.round(groupVolume[g]).toLocaleString()} lbs` : '— lbs';

    const titleEl = document.getElementById('balance-title');
    if (titleEl) titleEl.textContent = `${noun} by ${byMuscle ? 'Muscle' : 'Muscle Group'}`;
    const breakdownEl = document.getElementById('balance-breakdown-title');
    if (breakdownEl) breakdownEl.textContent = `${noun} Breakdown`;

    _balanceChart = new Chart(ctx, {
        type: 'radar',
        data: {
            labels: groups,
            datasets: [{
                label: noun,
                data: groups.map(metricVal),
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
                            // Lead with the selected metric; show the other as context.
                            return bySets
                                ? `${fmtSets(g)}  ·  ${fmtVol(g)}`
                                : `${fmtVol(g)}  ·  ${fmtSets(g)}`;
                        }
                    }
                }
            }
        }
    });

    const summaryEl = document.getElementById('balance-summary');
    if (summaryEl) {
        summaryEl.innerHTML = groups.map(g => {
            const primary = bySets ? fmtSets(g) : fmtVol(g);
            const secondary = bySets ? fmtVol(g) : fmtSets(g);
            return `<div class="col-6 col-sm-4">
              <div class="p-3 rounded-3 bg-body-secondary text-center">
                <div class="stat-label">${escapeHtml(g)}</div>
                <div class="fw-bold">${primary}</div>
                <div class="stat-label">${secondary}</div>
              </div>
            </div>`;
        }).join('');
    }
}

// ─── Training Consistency ─────────────────────────────────────────────────────

function updateConsistencyTab(allSets, period) {
    period = period || currentPeriod;

    const todayYMD = todayStr();
    const today = parseDateStr(todayYMD);

    // All-time set of workout days. The heatmap colours cells from this map;
    // the visible window is scoped to the selected period below.
    const workoutDatesAll = {};
    allSets.forEach(s => { workoutDatesAll[s.Date] = (workoutDatesAll[s.Date] || 0) + 1; });

    // Period window for the four stat boxes.
    let periodStartStr, periodDays, periodLabel;
    if (period === 'weekly') {
        periodDays = 7;
        periodLabel = 'this week';
    } else if (period === 'monthly') {
        periodDays = 31;
        periodLabel = 'this month';
    } else if (period === 'annual') {
        periodDays = 365;
        periodLabel = 'last year';
    } else { // alltime
        const allKeys = Object.keys(workoutDatesAll).sort();
        const firstStr = allKeys.length > 0 ? allKeys[0] : todayYMD;
        periodDays = Math.max(1, Math.round((today - parseDateStr(firstStr)) / 86400000) + 1);
        periodLabel = 'all time';
    }
    {
        const start = new Date(today);
        start.setDate(today.getDate() - (periodDays - 1));
        periodStartStr = localDateStr(start);
    }

    // Filter workout days to the selected period.
    const workoutDates = {};
    Object.keys(workoutDatesAll).forEach(d => {
        if (d >= periodStartStr && d <= todayYMD) workoutDates[d] = workoutDatesAll[d];
    });

    // Current and longest streak are intrinsic to the training history, not the
    // display window: a 40-day streak is 40 days whether you're looking at the
    // weekly or all-time view. They're computed from workoutDatesAll. Only the
    // Total and Avg/week boxes (below) scope to the selected period — they carry
    // a period label; the streak boxes don't.
    const allSorted = Object.keys(workoutDatesAll).sort();
    const earliest = allSorted.length ? parseDateStr(allSorted[0]) : today;
    const maxLookback = Math.round((today - earliest) / 86400000) + 1;

    // Current streak: consecutive days back from today. Today not yet logged
    // doesn't break it (i > 0 guard) so a streak stands until a full rest day.
    let currentStreak = 0;
    for (let i = 0; i <= maxLookback; i++) {
        const d = new Date(today);
        d.setDate(today.getDate() - i);
        const ds = localDateStr(d);
        if (workoutDatesAll[ds]) {
            currentStreak++;
        } else if (i > 0) {
            break;
        }
    }

    // Longest streak over all history.
    let longestStreak = allSorted.length > 0 ? 1 : 0;
    let streak = longestStreak;
    for (let i = 1; i < allSorted.length; i++) {
        const diff = Math.round(
            (parseDateStr(allSorted[i]) - parseDateStr(allSorted[i - 1])) / 86400000
        );
        if (diff === 1) {
            streak++;
            if (streak > longestStreak) longestStreak = streak;
        } else {
            streak = 1;
        }
    }

    // Period-scoped counts for the Total and Avg/week boxes.
    const sortedDates = Object.keys(workoutDates).sort();

    // Avg per week over the period.
    const periodWeeks = Math.max(1, periodDays / 7);
    const avgPerWeek = (sortedDates.length / periodWeeks).toFixed(1);

    document.getElementById('consistency-streak').textContent = currentStreak;
    document.getElementById('consistency-longest').textContent = longestStreak;
    document.getElementById('consistency-total').textContent = sortedDates.length;
    document.getElementById('consistency-avg').textContent = avgPerWeek;
    const totalLbl = document.getElementById('consistency-total-period');
    const avgLbl = document.getElementById('consistency-avg-period');
    if (totalLbl) totalLbl.textContent = periodLabel;
    if (avgLbl) avgLbl.textContent = periodLabel;

    // Build heatmap
    const grid = document.getElementById('heatmap-grid');
    if (!grid) return;

    const { r: pr, g: pg, b: pb } = hexToRgb(window._chartColor || '#2780e3');

    // Window the heatmap to the selected period (previously fixed at 1 year).
    // gridStart is the Sunday on or before the period's first day; the grid
    // spans whole week-columns from there through the week containing today.
    const windowStart = new Date(today);
    windowStart.setDate(today.getDate() - (periodDays - 1));
    const windowStartStr = localDateStr(windowStart);
    const gridStart = new Date(windowStart);
    gridStart.setDate(gridStart.getDate() - gridStart.getDay()); // back to Sunday

    const numWeeks = Math.floor((today - gridStart) / (7 * 86400000)) + 1;

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
            const ds = localDateStr(cellDate);
            const count = workoutDatesAll[ds] || 0;
            const isFuture = ds > todayYMD;
            // Days in the leading partial week that fall before the period
            // start are shown muted, so the grid reads as the selected window.
            const beforeStart = ds < windowStartStr;
            const outOfRange = isFuture || beforeStart;

            const cell = document.createElement('div');
            cell.className = 'heatmap-cell';
            if (ds === todayYMD) cell.classList.add('heatmap-today');

            if (!outOfRange && count > 0) {
                const opacity = count <= 2 ? 0.4 : count <= 4 ? 0.62 : count <= 7 ? 0.82 : 1.0;
                cell.style.background = `rgba(${pr},${pg},${pb},${opacity})`;
                cell.style.borderColor = `rgba(${pr},${pg},${pb},${Math.min(1, opacity * 1.3)})`;
            } else if (outOfRange) {
                cell.classList.add('heatmap-future');
            }

            cell.title = outOfRange ? ds : `${ds}: ${count} set${count !== 1 ? 's' : ''}`;
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

function updateRecoveryTab(allSets, exercises, period) {
    const exMap = {};
    (exercises || []).forEach(ex => { exMap[ex.Name] = ex; });

    // Only show exercises performed within the selected period.
    const periodSets = filterByPeriod(allSets, period || currentPeriod);

    const lastDate = {};
    const sessionDates = {};
    periodSets.forEach(s => {
        if (!lastDate[s.Name] || s.Date > lastDate[s.Name]) lastDate[s.Name] = s.Date;
        if (!sessionDates[s.Name]) sessionDates[s.Name] = new Set();
        sessionDates[s.Name].add(s.Date);
    });

    const todayYMD = todayStr();
    const today = parseDateStr(todayYMD);

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

// With 12 tabs the bar overflows on narrow screens; keep the selected tab
// visible by centering it when it activates. shown.bs.tab bubbles.
document.addEventListener('shown.bs.tab', function(ev) {
    if (ev.target.classList && ev.target.classList.contains('page-tab-btn')) {
        ev.target.scrollIntoView({ block: 'nearest', inline: 'center', behavior: 'smooth' });
    }
});

// ─── Grip & Hang ──────────────────────────────────────────────────────────────

var _gripChart = null, _hangChart = null;

function gripSet(id, v) { var e = document.getElementById(id); if (e) e.textContent = v; }

function updateGripTab(measurements) {
    measurements = measurements || window._measurements || [];
    var grip = measurements.filter(function(m) { return m.Metric === 'grip'; });
    var hang = measurements.filter(function(m) { return m.Metric === 'dead_hang'; });

    var left  = grip.filter(function(m) { return m.Hand === 'left'; });
    var right = grip.filter(function(m) { return m.Hand === 'right'; });
    var gripDates = Array.from(new Set(grip.map(function(m) { return m.Date; }))).sort();
    function series(rows) {
        var map = {};
        rows.forEach(function(m) { map[m.Date] = parseFloat(m.Value); });
        return gripDates.map(function(d) { return (d in map) ? map[d] : null; });
    }

    if (_gripChart) { _gripChart.destroy(); _gripChart = null; }
    var gripNo = document.getElementById('grip-no-data');
    if (grip.length === 0) {
        if (gripNo) gripNo.style.display = '';
        gripSet('grip-left', '–'); gripSet('grip-right', '–');
    } else {
        if (gripNo) gripNo.style.display = 'none';
        var lastL = left.length ? parseFloat(left[left.length - 1].Value) : null;
        var lastR = right.length ? parseFloat(right[right.length - 1].Value) : null;
        gripSet('grip-left',  lastL != null ? lastL + ' lb' : '–');
        gripSet('grip-right', lastR != null ? lastR + ' lb' : '–');
        _gripChart = new Chart(document.getElementById('grip-chart'), {
            type: 'line',
            data: { labels: gripDates, datasets: [
                { label: 'Left',  data: series(left),  borderColor: '#2780e3', backgroundColor: '#2780e333', spanGaps: true, tension: 0.2 },
                { label: 'Right', data: series(right), borderColor: '#e37b27', backgroundColor: '#e37b2733', spanGaps: true, tension: 0.2 },
            ] },
            options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: true } }, scales: { y: { beginAtZero: false } } }
        });
    }

    if (_hangChart) { _hangChart.destroy(); _hangChart = null; }
    var hangNo = document.getElementById('hang-no-data');
    if (hang.length === 0) {
        if (hangNo) hangNo.style.display = '';
        gripSet('hang-latest', '–'); gripSet('hang-best', '–');
    } else {
        if (hangNo) hangNo.style.display = 'none';
        var hvals = hang.map(function(m) { return parseFloat(m.Value); });
        gripSet('hang-latest', hvals[hvals.length - 1] + 's');
        gripSet('hang-best', Math.max.apply(null, hvals) + 's');
        _hangChart = new Chart(document.getElementById('hang-chart'), {
            type: 'line',
            data: { labels: hang.map(function(m) { return m.Date; }), datasets: [
                { label: 'Dead hang (s)', data: hvals, borderColor: '#27b36b', backgroundColor: '#27b36b33', tension: 0.2 },
            ] },
            options: { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true } } }
        });
    }
}

function saveMeasurement() {
    var status = document.getElementById('m-status');
    var date = document.getElementById('m-date').value;
    if (!date) { status.textContent = 'date is required'; return; }
    var body = {
        Date: date,
        GripLeft:  document.getElementById('m-grip-left').value  || null,
        GripRight: document.getElementById('m-grip-right').value || null,
        DeadHang:  document.getElementById('m-hang').value       || null,
    };
    if (!body.GripLeft && !body.GripRight && !body.DeadHang) { status.textContent = 'enter at least one value'; return; }
    fetch('/api/measurements', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
        .then(function(r) {
            if (!r.ok) { return r.json().then(function(j) { status.textContent = (j && j.error) || ('HTTP ' + r.status); }); }
            var modalEl = document.getElementById('logMeasurementModal');
            var inst = bootstrap.Modal.getInstance(modalEl);
            if (inst) inst.hide();
            ['m-grip-left', 'm-grip-right', 'm-hang'].forEach(function(id) { document.getElementById(id).value = ''; });
            return fetch('/api/measurements').then(function(rr) { return rr.json(); }).then(function(ms) {
                window._measurements = ms || [];
                updateGripTab(window._measurements);
            });
        })
        .catch(function(e) { status.textContent = e.message || 'request failed'; });
}
