var sOffset = 0;
var currentPeriod = 'weekly';
var distributionChart = null;

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

function toggleSummaryPeriod(period) {
    currentPeriod = period;

    document.querySelectorAll('#period-btns .btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.period === period);
    });

    const { start, end } = getPeriodDates(period);
    const rangeEl = document.getElementById('period-range');
    if (rangeEl) rangeEl.textContent = `${formatDate(start)} – ${formatDate(end)}`;

    const periodSets = filterByPeriod(window.currentSets, period);
    updateSummaryDisplay(periodSets);
    updateExerciseDistribution(periodSets, window.exercises);
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

// ─── Exercise history (Exercises tab) ────────────────────────────────────────

function addRow(i, date, weight, reps) {
    document.getElementById('stats-table').insertAdjacentHTML('beforeend',
        `<tr><td style="opacity:45%;">${i}.</td><td>${date}</td><td>${weight}</td><td>${reps}</td></tr>`);
}

function setStatsPage(sets, hcolor, off, step) {
    window.currentSets = sets;

    const periodSets = filterByPeriod(sets, currentPeriod);

    const { start, end } = getPeriodDates(currentPeriod);
    const rangeEl = document.getElementById('period-range');
    if (rangeEl) rangeEl.textContent = `${formatDate(start)} – ${formatDate(end)}`;

    const ex = document.getElementById('ex-value');
    if (!ex) return;
    const selectedEx = ex.value;
    const exSets = periodSets.filter(s => s.Name === selectedEx);

    sOffset = Math.max(0, sOffset + off);

    const len  = exSets.length;
    const move = step + sOffset * step;
    let start2, end2;

    if (len > move) {
        start2 = len - move;
        end2   = start2 + step;
    } else {
        sOffset = Math.max(0, sOffset - 1);
        end2    = Math.min(step, len);
        start2  = 0;
    }

    document.getElementById('stats-table').innerHTML = '';
    const dates = [], ws = [];
    for (let i = start2; i < end2; i++) {
        addRow(i + 1, exSets[i].Date, exSets[i].Weight, exSets[i].Reps);
        dates.push(exSets[i].Date);
        ws.push(exSets[i].Weight);
    }

    weightChart('stats-ex-weight', dates, ws, hcolor, true);
    updateSummaryDisplay(periodSets);
    updateExerciseDistribution(periodSets, window.exercises);
}
