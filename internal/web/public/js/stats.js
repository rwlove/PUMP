var currentPeriod = 'weekly';
var distributionChart = null;
var _volumeChart = null;

// ─── Period helpers ───────────────────────────────────────────────────────────

function getPeriodDates(period) {
    const end = new Date();
    const start = new Date(end);
    if (period === 'weekly')  start.setDate(end.getDate() - 7);
    else if (period === 'monthly') start.setMonth(end.getMonth() - 1);
    else if (period === 'annual')  start.setFullYear(end.getFullYear() - 1);
    return { start, end };
}

function formatDate(date) {
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function filterByPeriod(sets, period) {
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
    const { start, end } = getPeriodDates(period);
    const rangeEl = document.getElementById('period-range');
    if (rangeEl) rangeEl.textContent = `${formatDate(start)} \u2013 ${formatDate(end)}`;

    // Always refresh Overview
    const sets = filterByPeriod(window.currentSets, period);
    updateSummaryDisplay(sets);
    updateExerciseDistribution(sets, window.exercises);

    // Refresh Weight Moved if visible
    const actTab = document.getElementById('tab-activity');
    if (actTab && actTab.classList.contains('show')) {
        refreshVolumeChart();
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

// ─── Volume chart (Weight Moved tab) ─────────────────────────────────────────

function updateVolumeChart(sets, exercises, period, exerciseName) {
    const { start, end } = getPeriodDates(period);
    const filtered = sets.filter(s => {
        if (s.Name !== exerciseName) return false;
        const d = new Date(s.Date);
        return d >= start && d <= end;
    });

    // Group by date — sum weight × reps for every set that day
    const volumeByDate = {};
    filtered.forEach(s => {
        const vol = parseFloat(s.Weight) * parseInt(s.Reps, 10);
        if (!isNaN(vol)) {
            volumeByDate[s.Date] = (volumeByDate[s.Date] || 0) + vol;
        }
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
                        label(ctx) {
                            return `Volume: ${ctx.raw.toLocaleString(undefined, { maximumFractionDigits: 1 })} lbs`;
                        }
                    }
                }
            }
        }
    });
}

function refreshVolumeChart() {
    const sel = document.getElementById('volume-ex-select');
    if (!sel) return;
    updateVolumeChart(window.currentSets, window.exercises, currentPeriod, sel.value);
}
