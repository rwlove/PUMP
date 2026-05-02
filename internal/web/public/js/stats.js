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

function updateVolumeChart(sets, exercises, period, exerciseName) {
    const { start, end } = getPeriodDates(period);
    const filtered = sets.filter(s => {
        if (s.Name !== exerciseName) return false;
        const d = new Date(s.Date);
        return d >= start && d <= end;
    });

    // Group by date — collect individual sets and compute total volume per day
    const volumeByDate = {};    // date → total volume
    const detailByDate = {};    // date → [{weight, reps, vol}, ...]

    filtered.forEach(s => {
        const w   = parseFloat(s.Weight);
        const r   = parseInt(s.Reps, 10);
        const vol = (!isNaN(w) && !isNaN(r)) ? w * r : 0;
        volumeByDate[s.Date] = (volumeByDate[s.Date] || 0) + vol;
        if (!detailByDate[s.Date]) detailByDate[s.Date] = [];
        detailByDate[s.Date].push({ weight: w, reps: r, vol });
    });

    const dates   = Object.keys(volumeByDate).sort();
    const volumes = dates.map(d => volumeByDate[d]);

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

    _volumeChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: dates,
            datasets: [{
                label: 'Total Volume',
                data: volumes,
                backgroundColor: color + '55',
                borderColor: color,
                borderWidth: 1.5,
                borderRadius: 4,
            }]
        },
        options: {
            responsive: true,
            scales: {
                x: { grid: { display: false } },
                y: {
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
                    callbacks: {
                        // Show math breakdown: e.g. "80×10 + 75×8 + 75×8 = 1,400 lbs"
                        label(ctx) {
                            const date    = ctx.label;
                            const details = detailByDate[date] || [];
                            const total   = ctx.raw;

                            if (details.length === 0) {
                                return `Volume: ${total.toLocaleString(undefined, { maximumFractionDigits: 1 })} lbs`;
                            }

                            const math = details
                                .map(d => {
                                    const w = isNaN(d.weight) ? '?' : d.weight;
                                    const r = isNaN(d.reps)   ? '?' : d.reps;
                                    return `${w}×${r}`;
                                })
                                .join(' + ');

                            const totalFmt = total.toLocaleString(undefined, { maximumFractionDigits: 1 });
                            return `${math} = ${totalFmt} lbs`;
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
