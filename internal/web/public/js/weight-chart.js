var wChart = null;

function splitWeight(weight) {
    var dates = [];
    var ws = [];
    for (let i = 0; i < weight.length; i++) {
        dates.push(weight[i].Date);
        ws.push(parseFloat(weight[i].Weight));
    }
    return { dates, ws };
}

function weightChart(id, dates, ws, wcolor) {
    const ctx = document.getElementById(id);
    if (!ctx) return;

    if (wChart) {
        wChart.clear();
        wChart.destroy();
    }

    wChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels: dates,
            datasets: [{
                type: 'line',
                label: 'Weight',
                data: ws,
                segment: {
                    borderColor(ctx) {
                        const prev = ctx.p0.parsed.y;
                        const next = ctx.p1.parsed.y;
                        if (next < prev) return cssVar('--bs-success', '#28a745'); // falling → green
                        if (next > prev) return cssVar('--bs-danger', '#dc3545'); // rising → red
                        return wcolor;
                    }
                },
                backgroundColor: wcolor + '15',
                borderWidth: 2,
                fill: true,
                tension: 0.1,
                pointBackgroundColor: cssVar('--bs-secondary-color', '#888'),
                pointBorderColor: cssVar('--bs-secondary-color', '#888'),
                pointRadius: 3,
            }]
        },
        options: {
            responsive: true,
            scales: {
                x: {
                    display: true,
                    grid: { display: false },
                    ticks: {
                        maxRotation: 45,
                        font: { size: 11 }
                    }
                },
                y: {
                    display: true,
                    beginAtZero: false,
                    grid: { display: false },
                    title: {
                        display: true,
                        text: 'Body Weight (lbs)',
                        font: { size: 12 },
                        color: 'var(--bs-secondary-color)'
                    },
                    ticks: {
                        callback(v) { return Number(v).toFixed(1) + ' lbs'; }
                    }
                }
            },
            plugins: {
                legend: { display: false },
                tooltip: {
                    callbacks: {
                        label(ctx) {
                            return `Weight: ${parseFloat(ctx.raw).toFixed(1)} lbs`;
                        }
                    }
                }
            }
        }
    });
}

// updateWeightPeriodDelta shows the net body-weight change across the charted
// period next to the panel header. Green when weight fell (a loss), red when it
// rose (a gain) — the same direction semantics the chart line segments use.
function updateWeightPeriodDelta(weight) {
    var el = document.getElementById('weight-period-delta');
    if (!el) return;
    el.classList.remove('is-loss', 'is-gain', 'is-flat');
    // Sort a copy by date so first/last are the period endpoints regardless of
    // the incoming order; drop any rows without a usable date/weight.
    var pts = (weight || []).filter(function(w) {
        return w && w.Date && !isNaN(parseFloat(w.Weight));
    }).slice().sort(function(a, b) {
        return a.Date < b.Date ? -1 : (a.Date > b.Date ? 1 : 0);
    });
    if (pts.length < 2) { el.textContent = ''; el.title = ''; return; }
    var first = parseFloat(pts[0].Weight);
    var last = parseFloat(pts[pts.length - 1].Weight);
    var delta = last - first;
    var mag = Math.abs(delta).toFixed(1);
    if (delta < 0) {
        el.classList.add('is-loss');
        el.textContent = '▼ ' + mag + ' lbs';   // ▼ lost weight
    } else if (delta > 0) {
        el.classList.add('is-gain');
        el.textContent = '▲ ' + mag + ' lbs';   // ▲ gained weight
    } else {
        el.classList.add('is-flat');
        el.textContent = '±0.0 lbs';
    }
    el.title = first.toFixed(1) + ' → ' + last.toFixed(1) + ' lbs over the period';
}

// generateWeightChart renders a weight line chart on the given canvas.
function generateWeightChart(weight, wcolor, canvasId) {
    // Keep the header delta in lockstep with whatever is charted, even when
    // the period has no data to draw.
    if (canvasId === 'stats-body-weight') updateWeightPeriodDelta(weight);
    if (!weight) return;
    var { dates, ws } = splitWeight(weight);
    weightChart(canvasId, dates, ws, wcolor);
}
