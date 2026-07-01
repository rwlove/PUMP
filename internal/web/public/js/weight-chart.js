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
                        if (next < prev) return '#28a745'; // falling → green
                        if (next > prev) return '#dc3545'; // rising → red
                        return wcolor;
                    }
                },
                backgroundColor: wcolor + '15',
                borderWidth: 2,
                fill: true,
                tension: 0.1,
                pointBackgroundColor: '#888',
                pointBorderColor: '#888',
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

// generateWeightChart renders a weight line chart on the given canvas.
function generateWeightChart(weight, wcolor, canvasId) {
    if (!weight) return;
    var { dates, ws } = splitWeight(weight);
    weightChart(canvasId, dates, ws, wcolor);
}
