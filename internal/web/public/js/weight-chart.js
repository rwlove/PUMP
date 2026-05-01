var wChart = null;

function splitWeight(weight, show) {
    var dates = [];
    var ws = [];
    weight = weight.slice(show);
    for (let i = 0; i < weight.length; i++) {
        dates.push(weight[i].Date);
        ws.push(weight[i].Weight);
    }
    return { dates, ws };
}

function weightChart(id, dates, ws, wcolor, xticks) {
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
                borderColor: wcolor,
                backgroundColor: wcolor + '20',
                borderWidth: 2,
                fill: true,
                tension: 0.1
            }]
        },
        options: {
            responsive: true,
            scales: {
                x: { display: xticks, grid: { display: false } },
                y: { beginAtZero: false, grid: { display: false } }
            },
            plugins: { legend: { display: false } }
        }
    });
}

// generateWeightChart renders a weight line chart.
// canvasId defaults to 'weight-chart' if not provided.
function generateWeightChart(weight, wcolor, show, canvasId) {
    if (!weight) return;
    var id = canvasId || 'weight-chart';
    var { dates, ws } = splitWeight(weight, show);
    weightChart(id, dates, ws, wcolor, false);
}
