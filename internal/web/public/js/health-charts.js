// health-charts.js — wearable charts for the Stats tabs (Steps / Heart Rate /
// Sleep / Cardio) and the Overall Health dashboard. Data comes from
// window.healthData (server-aggregated models.HealthStats). Period filtering
// reuses filterByPeriod() from stats.js when present (Stats page); the
// dashboard uses fixed recent windows and works standalone.

var _stepsChart = null, _hrChart = null, _sleepChart = null, _cardioChart = null;
var _hdSparks = {};

// ─── helpers ────────────────────────────────────────────────────────────────

function hcPrimary() {
    return getComputedStyle(document.documentElement).getPropertyValue('--bs-primary').trim() || '#2780e3';
}
function hcDestroy(c) { if (c) { c.clear(); c.destroy(); } return null; }
function hcData(key) { return (window.healthData && window.healthData[key]) ? window.healthData[key] : []; }
function hcByPeriod(series, period) {
    if (typeof filterByPeriod === 'function') return filterByPeriod(series || [], period);
    return series || [];
}
function hcSetText(id, txt) { const el = document.getElementById(id); if (el) el.textContent = txt; }
function hcLast(series, n) { return series.slice(Math.max(0, series.length - n)); }
function hcMean(vals) { return vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : 0; }

// 7-point trailing rolling average over a numeric array (nulls for the warm-up).
function hcRollingAvg(vals, win) {
    return vals.map((_, i) => {
        if (i < win - 1) return null;
        let s = 0; for (let k = i - win + 1; k <= i; k++) s += vals[k];
        return Math.round(s / win);
    });
}

// fmtDur turns minutes into "7h 12m" / "48m".
function hcFmtDur(min) {
    const h = Math.floor(min / 60), m = Math.round(min % 60);
    return h > 0 ? (h + 'h ' + m + 'm') : (m + 'm');
}

// hcDelta builds a coloured "▲3 / ▼2 / —" delta vs a baseline. lowerBetter
// flips the colour (e.g. resting HR going down is good).
function hcDelta(id, cur, base, lowerBetter, suffix) {
    const el = document.getElementById(id);
    if (!el) return;
    if (base == null || cur == null || !isFinite(base) || base === 0) { el.textContent = ''; return; }
    const d = cur - base;
    const arrow = d > 0 ? '▲' : (d < 0 ? '▼' : '—');
    const good = lowerBetter ? d < 0 : d > 0;
    el.textContent = arrow + ' ' + Math.abs(Math.round(d)) + (suffix || '');
    el.style.color = d === 0 ? 'var(--bs-secondary-color)' : (good ? '#28a745' : '#dc3545');
}

// hcSpark draws a tiny axis-less sparkline (dashboard tiles).
function hcSpark(id, vals, color) {
    const ctx = document.getElementById(id);
    if (!ctx) return;
    _hdSparks[id] = hcDestroy(_hdSparks[id]);
    if (!vals || !vals.length) return;
    _hdSparks[id] = new Chart(ctx, {
        type: 'line',
        data: { labels: vals.map((_, i) => i), datasets: [{
            data: vals, borderColor: color || hcPrimary(), borderWidth: 2,
            backgroundColor: (color || hcPrimary()) + '18', fill: true,
            pointRadius: 0, tension: 0.35,
        }] },
        options: {
            responsive: true, maintainAspectRatio: false,
            plugins: { legend: { display: false }, tooltip: { enabled: false } },
            scales: { x: { display: false }, y: { display: false } },
            elements: { line: { capBezierPoints: true } },
        }
    });
}

const hcAxisOpts = {
    x: { grid: { display: false }, ticks: { maxRotation: 45, font: { size: 11 }, autoSkip: true, maxTicksLimit: 12 } },
    yCount: (title, suffix) => ({
        beginAtZero: true, grid: { display: false },
        title: { display: true, text: title, font: { size: 12 }, color: 'var(--bs-secondary-color)' },
        ticks: { callback(v) { return Number(v).toLocaleString() + (suffix || ''); } }
    }),
};

// ─── Steps tab ──────────────────────────────────────────────────────────────

function renderStepsTab(period) {
    const all = hcData('DailySteps');
    const today = window._serverDate;
    const todayRow = all.find(d => d.Date === today);
    hcSetText('steps-today', todayRow ? Math.round(todayRow.Value).toLocaleString() : '0');
    const last7 = hcLast(all, 7).map(d => d.Value);
    hcSetText('steps-avg', last7.length ? Math.round(hcMean(last7)).toLocaleString() : '–');
    hcSetText('steps-best', all.length ? Math.round(Math.max(...all.map(d => d.Value))).toLocaleString() : '–');

    const data = hcByPeriod(all, period);
    const canvas = document.getElementById('steps-chart');
    const noData = document.getElementById('steps-no-data');
    if (!data.length) { if (canvas) canvas.style.display = 'none'; if (noData) noData.style.display = 'block'; _stepsChart = hcDestroy(_stepsChart); return; }
    if (canvas) canvas.style.display = ''; if (noData) noData.style.display = 'none';

    const labels = data.map(d => d.Date), vals = data.map(d => Math.round(d.Value));
    _stepsChart = hcDestroy(_stepsChart);
    _stepsChart = new Chart(canvas, {
        data: {
            labels, datasets: [
                { type: 'bar', label: 'Steps', data: vals, backgroundColor: hcPrimary() + 'cc', borderRadius: 4, order: 2 },
                { type: 'line', label: '7-day avg', data: hcRollingAvg(vals, 7), borderColor: '#888', borderWidth: 2, pointRadius: 0, tension: 0.3, order: 1 },
            ]
        },
        options: { responsive: true, scales: { x: hcAxisOpts.x, y: hcAxisOpts.yCount('Steps') }, plugins: { legend: { display: true, position: 'top', labels: { boxWidth: 12, font: { size: 11 } } } } }
    });
}

// ─── Heart Rate tab ─────────────────────────────────────────────────────────

function renderHRTab(period) {
    const resting = hcData('RestingHR');
    const latest = resting.length ? resting[resting.length - 1].Value : null;
    hcSetText('hr-resting', latest != null ? Math.round(latest) : '–');
    const last7 = hcLast(resting, 7).map(d => d.Value);
    hcSetText('hr-resting-avg', last7.length ? Math.round(hcMean(last7)) : '–');
    const dailyAll = hcData('DailyHR');
    const todayHR = dailyAll.find(d => d.Date === window._serverDate);
    hcSetText('hr-range', todayHR ? (Math.round(todayHR.Min) + '–' + Math.round(todayHR.Max)) : '–');

    const r = hcByPeriod(resting, period);
    const dh = hcByPeriod(dailyAll, period);
    const canvas = document.getElementById('hr-chart');
    const noData = document.getElementById('hr-no-data');
    if (!r.length && !dh.length) { if (canvas) canvas.style.display = 'none'; if (noData) noData.style.display = 'block'; _hrChart = hcDestroy(_hrChart); return; }
    if (canvas) canvas.style.display = ''; if (noData) noData.style.display = 'none';

    // Union of dates so the resting line and the min/max band align.
    const labels = dh.length ? dh.map(d => d.Date) : r.map(d => d.Date);
    const restMap = {}; r.forEach(d => restMap[d.Date] = Math.round(d.Value));
    _hrChart = hcDestroy(_hrChart);
    _hrChart = new Chart(canvas, {
        data: {
            labels, datasets: [
                { type: 'line', label: 'Max', data: dh.map(d => Math.round(d.Max)), borderColor: 'transparent', backgroundColor: hcPrimary() + '18', pointRadius: 0, fill: '+1', order: 3 },
                { type: 'line', label: 'Min', data: dh.map(d => Math.round(d.Min)), borderColor: 'transparent', backgroundColor: hcPrimary() + '18', pointRadius: 0, fill: false, order: 3 },
                { type: 'line', label: 'Avg', data: dh.map(d => Math.round(d.Avg)), borderColor: '#888', borderWidth: 1.5, pointRadius: 0, tension: 0.3, order: 2 },
                { type: 'line', label: 'Resting', data: labels.map(l => restMap[l] != null ? restMap[l] : null), borderColor: hcPrimary(), borderWidth: 2.5, pointRadius: 2, tension: 0.3, spanGaps: true, order: 1 },
            ]
        },
        options: { responsive: true, scales: { x: hcAxisOpts.x, y: { beginAtZero: false, grid: { display: false }, title: { display: true, text: 'Heart Rate (bpm)', font: { size: 12 }, color: 'var(--bs-secondary-color)' } } }, plugins: { legend: { display: true, position: 'top', labels: { boxWidth: 12, font: { size: 11 }, filter: i => i.text !== 'Min' } } } }
    });
}

// ─── Sleep tab ──────────────────────────────────────────────────────────────

function renderSleepTab(period) {
    const all = hcData('Sleep');
    const last = all.length ? all[all.length - 1] : null;
    hcSetText('sleep-last', last ? hcFmtDur(last.Minutes) : '–');
    const last7 = hcLast(all, 7).map(d => d.Minutes);
    hcSetText('sleep-avg', last7.length ? hcFmtDur(hcMean(last7)) : '–');
    const deepPct = last && last.Minutes > 0 ? Math.round(100 * last.Deep / last.Minutes) : null;
    hcSetText('sleep-deep', deepPct != null && deepPct > 0 ? deepPct + '%' : '–');

    const data = hcByPeriod(all, period);
    const canvas = document.getElementById('sleep-chart');
    const noData = document.getElementById('sleep-no-data');
    if (!data.length) { if (canvas) canvas.style.display = 'none'; if (noData) noData.style.display = 'block'; _sleepChart = hcDestroy(_sleepChart); return; }
    if (canvas) canvas.style.display = ''; if (noData) noData.style.display = 'none';

    const labels = data.map(d => d.Date);
    const hasStages = data.some(d => (d.Deep + d.Light + d.REM + d.Awake) > 0);
    let datasets;
    if (hasStages) {
        datasets = [
            { label: 'Deep', data: data.map(d => +(d.Deep / 60).toFixed(2)), backgroundColor: hcPrimary() },
            { label: 'Light', data: data.map(d => +(d.Light / 60).toFixed(2)), backgroundColor: hcPrimary() + '99' },
            { label: 'REM', data: data.map(d => +(d.REM / 60).toFixed(2)), backgroundColor: '#6f42c1aa' },
            { label: 'Awake', data: data.map(d => +(d.Awake / 60).toFixed(2)), backgroundColor: '#dc354566' },
        ];
    } else {
        datasets = [{ label: 'Sleep', data: data.map(d => +(d.Minutes / 60).toFixed(2)), backgroundColor: hcPrimary() + 'cc', borderRadius: 4 }];
    }
    _sleepChart = hcDestroy(_sleepChart);
    _sleepChart = new Chart(canvas, {
        type: 'bar',
        data: { labels, datasets },
        options: {
            responsive: true,
            scales: { x: Object.assign({ stacked: true }, hcAxisOpts.x), y: { stacked: true, beginAtZero: true, grid: { display: false }, title: { display: true, text: 'Hours', font: { size: 12 }, color: 'var(--bs-secondary-color)' }, ticks: { callback(v) { return v + 'h'; } } } },
            plugins: { legend: { display: hasStages, position: 'top', labels: { boxWidth: 12, font: { size: 11 } } }, tooltip: { callbacks: { label(c) { return c.dataset.label + ': ' + hcFmtDur(c.raw * 60); } } } }
        }
    });
}

// ─── Cardio tab ─────────────────────────────────────────────────────────────

function renderCardioTab(period) {
    const data = hcByPeriod(hcData('Cardio'), period);
    hcSetText('cardio-count', data.length);
    hcSetText('cardio-mins', Math.round(data.reduce((a, b) => a + b.Minutes, 0)));
    const km = data.reduce((a, b) => a + b.Meters, 0) / 1000;
    hcSetText('cardio-dist', km > 0 ? km.toFixed(1) + ' km' : '–');

    const canvas = document.getElementById('cardio-chart');
    const noData = document.getElementById('cardio-no-data');
    const list = document.getElementById('cardio-list');
    if (!data.length) {
        if (canvas) canvas.style.display = 'none';
        if (noData) noData.style.display = 'block';
        if (list) list.innerHTML = '';
        _cardioChart = hcDestroy(_cardioChart);
        return;
    }
    if (canvas) canvas.style.display = ''; if (noData) noData.style.display = 'none';

    _cardioChart = hcDestroy(_cardioChart);
    _cardioChart = new Chart(canvas, {
        type: 'bar',
        data: { labels: data.map(d => d.Date), datasets: [{ label: 'Minutes', data: data.map(d => Math.round(d.Minutes)), backgroundColor: hcPrimary() + 'cc', borderRadius: 4 }] },
        options: { responsive: true, scales: { x: hcAxisOpts.x, y: hcAxisOpts.yCount('Minutes') }, plugins: { legend: { display: false }, tooltip: { callbacks: { label(c) { return data[c.dataIndex].Type + ': ' + hcFmtDur(c.raw); } } } } }
    });

    if (list) {
        const rows = data.slice().reverse().slice(0, 12).map(s => {
            const dist = s.Meters > 0 ? (s.Meters / 1000).toFixed(2) + ' km' : '—';
            return `<tr><td class="ps-3">${escapeHtml(s.Date)}</td><td>${escapeHtml(s.Type)}</td><td>${hcFmtDur(s.Minutes)}</td><td>${dist}</td></tr>`;
        }).join('');
        list.innerHTML = rows;
    }
}

// ─── Overall Health dashboard ─────────────────────────────────────────────────

function renderHealthDashboard() {
    // Body weight (window._allWeight: [{Date, Weight}], oldest-first).
    const w = (window._allWeight || []).map(r => ({ Date: r.Date, Value: parseFloat(r.Weight) }));
    if (w.length) {
        const cur = w[w.length - 1].Value;
        hcSetText('hd-weight-val', cur.toFixed(1));
        const base = w.length > 1 ? w[Math.max(0, w.length - 30)].Value : null;
        hcDelta('hd-weight-delta', cur, base, true, ' lb');
        hcSpark('hd-weight-spark', hcLast(w, 30).map(d => d.Value));
    } else { hcSetText('hd-weight-val', '–'); }

    // Steps today + 30-day sparkline.
    const steps = hcData('DailySteps');
    if (steps.length) {
        const todayRow = steps.find(d => d.Date === window._serverDate);
        hcSetText('hd-steps-val', todayRow ? Math.round(todayRow.Value).toLocaleString() : '0');
        const avg7 = hcMean(hcLast(steps, 7).map(d => d.Value));
        hcDelta('hd-steps-delta', todayRow ? todayRow.Value : 0, avg7, false, '');
        hcSpark('hd-steps-spark', hcLast(steps, 30).map(d => d.Value));
    } else { hcSetText('hd-steps-val', '–'); }

    // Resting HR.
    const rhr = hcData('RestingHR');
    if (rhr.length) {
        const cur = rhr[rhr.length - 1].Value;
        hcSetText('hd-hr-val', Math.round(cur));
        const base = hcMean(hcLast(rhr, 7).map(d => d.Value));
        hcDelta('hd-hr-delta', cur, base, true, '');
        hcSpark('hd-hr-spark', hcLast(rhr, 30).map(d => d.Value), '#dc3545');
    } else { hcSetText('hd-hr-val', '–'); }

    // Sleep last night.
    const sleep = hcData('Sleep');
    if (sleep.length) {
        const last = sleep[sleep.length - 1];
        hcSetText('hd-sleep-val', hcFmtDur(last.Minutes));
        const avg7 = hcMean(hcLast(sleep, 7).map(d => d.Minutes));
        hcSetText('hd-sleep-sub', '7-day avg ' + hcFmtDur(avg7));
        hcSpark('hd-sleep-spark', hcLast(sleep, 30).map(d => d.Minutes / 60), '#6f42c1');
    } else { hcSetText('hd-sleep-val', '–'); }

    // Training this week + streak (from window.currentSets: [{Date,...}]).
    const sets = window.currentSets || [];
    const byDay = {};
    sets.forEach(s => { byDay[s.Date] = (byDay[s.Date] || 0) + 1; });
    const days = Object.keys(byDay).sort();
    const weekAgo = new Date(); weekAgo.setDate(weekAgo.getDate() - 7);
    const weekSets = sets.filter(s => new Date(s.Date) >= weekAgo).length;
    hcSetText('hd-train-val', weekSets);
    hcSetText('hd-train-sub', days.length ? (Object.keys(byDay).filter(d => new Date(d) >= weekAgo).length + ' days this week') : 'no sets yet');
    // Current streak: consecutive days ending today/yesterday with a set.
    let streak = 0; const d = new Date(window._serverDate || new Date());
    for (;;) {
        const key = d.toLocaleDateString('en-CA');
        if (byDay[key]) { streak++; d.setDate(d.getDate() - 1); }
        else if (streak === 0 && key === (window._serverDate)) { d.setDate(d.getDate() - 1); } // allow rest today
        else break;
    }
    hcSetText('hd-streak-val', streak);
    hcSetText('hd-streak-sub', streak === 1 ? 'day' : 'days');

    // Weekly training volume sparkline (last 8 weeks set counts).
    const weeks = {};
    sets.forEach(s => { const dt = new Date(s.Date); const wk = Math.floor(dt.getTime() / (7 * 864e5)); weeks[wk] = (weeks[wk] || 0) + 1; });
    const wkVals = Object.keys(weeks).sort().map(k => weeks[k]);
    hcSpark('hd-train-spark', hcLast(wkVals, 12), '#28a745');
}
