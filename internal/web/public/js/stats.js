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

function updateVolumeChart(sets, exercises, period, exerciseName) {
    const { start, end } = getPeriodDates(period);
    const filtered = sets.filter(s => {
        if (s.Name !== exerciseName) return false;
        const d = new Date(s.Date);
        return d >= start && d <= end;
    });

    // Group by date, preserving set order within each day.
    const setsByDate = {};   // date → [{weight, reps, vol}, ...]

    filtered.forEach(s => {
        const w   = parseFloat(s.Weight);
        const r   = parseInt(s.Reps, 10);
        const vol = (!isNaN(w) && !isNaN(r)) ? w * r : 0;
        if (!setsByDate[s.Date]) setsByDate[s.Date] = [];
        setsByDate[s.Date].push({ weight: w, reps: r, vol });
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
                            const s   = daySets[i];
                            const w   = isNaN(s.weight) ? '?' : s.weight;
                            const r   = isNaN(s.reps)   ? '?' : s.reps;
                            const vol = s.vol.toLocaleString(undefined, { maximumFractionDigits: 1 });
                            return `Set ${i + 1}: ${w}×${r} = ${vol} lbs`;
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
