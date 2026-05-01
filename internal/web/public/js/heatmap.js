// Shared tooltip element
let sharedTooltipEl = null;

function createSharedTooltip() {
    if (!sharedTooltipEl) {
        sharedTooltipEl = document.createElement('div');
        sharedTooltipEl.id = 'shared-heatmap-tooltip';
        document.body.appendChild(sharedTooltipEl);

        const style = document.createElement('style');
        style.textContent = `
            #shared-heatmap-tooltip {
                background: rgba(255, 255, 255, 0.95);
                border: 1px solid rgba(0, 0, 0, 0.1);
                border-radius: 4px;
                box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
                color: #333;
                opacity: 0;
                padding: 6px 10px;
                pointer-events: none;
                position: fixed;
                transform: translate(-50%, 0);
                transition: all .1s ease;
                z-index: 10000;
                font-size: 13px;
            }
        `;
        document.head.appendChild(style);
    }
    return sharedTooltipEl;
}

function updateSharedTooltip(chart, context, data) {
    const tooltipEl = createSharedTooltip();
    const tooltipModel = context.tooltip;

    if (tooltipModel.opacity === 0) {
        tooltipEl.style.opacity = 0;
        return;
    }

    const dateObj = new Date(data.d);
    const dayOfWeek = dateObj.toLocaleDateString(undefined, { weekday: 'long' });
    tooltipEl.innerHTML = `${dayOfWeek}, ${data.d}`;

    const position = context.chart.canvas.getBoundingClientRect();
    tooltipEl.style.opacity = 1;
    tooltipEl.style.position = 'fixed';
    tooltipEl.style.left = position.left + window.pageXOffset + tooltipModel.caretX + 'px';
    tooltipEl.style.top = position.top + window.pageYOffset + tooltipModel.caretY + 'px';
}

function lowerData(heat) {
    var ldata = [];
    for (let i = 0; i < heat.length; i++) {
        let val = heat[i];
        ldata.push({
            x: val.X,
            y: val.Y,
            d: val.D,
            v: val.V,
            Color: val.Color || '',
            Colors: val.Colors || [],
            WorkoutNames: val.WorkoutNames || [],
            WorkoutWeights: val.WorkoutWeights || [],
            WorkoutReps: val.WorkoutReps || []
        });
    }
    return ldata;
}

// makeColorChart renders the exercise-color heatmap.
// onDateClick is an optional callback(date) called when a cell is clicked.
function makeColorChart(heat, onDateClick) {
    let ldata = lowerData(heat);
    var ctx = document.getElementById('color-chart').getContext('2d');
    window.colorChart = new Chart(ctx, {
        type: 'matrix',
        data: {
            datasets: [{
                label: 'Exercise Colors',
                data: ldata,
                selectedDate: null,
                backgroundColor(context) {
                    const data = context.dataset.data[context.dataIndex];
                    if (data.d === this.selectedDate) return 'rgba(255, 255, 0, 0.5)';
                    if (!data.Colors || data.Colors.length === 0) return 'rgba(200, 200, 200, 0.1)';
                    if (data.Colors.length === 1) return data.Colors[0];
                    return 'rgba(0, 0, 0, 0)';
                },
                borderColor(context) {
                    const data = context.dataset.data[context.dataIndex];
                    const alpha = data.d === this.selectedDate ? 1 : 0.5;
                    if (!data.Colors || data.Colors.length === 0) return 'rgba(200, 200, 200, 0.1)';
                    return Chart.helpers.color('grey').alpha(alpha).rgbString();
                },
                width: ({ chart }) => (chart.chartArea || {}).width / 53 - 1.5,
                height: ({ chart }) => (chart.chartArea || {}).height / 7 - 2
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            animation: { duration: 10 },
            onClick: function(event, elements) {
                if (elements && elements.length > 0) {
                    const date = this.data.datasets[0].data[elements[0].index].d;
                    this.data.datasets[0].selectedDate = date;
                    this.update();
                    if (typeof onDateClick === 'function') onDateClick(date);
                }
            },
            plugins: {
                legend: { display: false },
                tooltip: {
                    enabled: false,
                    external: function(context) {
                        if (context.tooltip.dataPoints && context.tooltip.dataPoints.length > 0) {
                            updateSharedTooltip(this, context, context.tooltip.dataPoints[0].raw);
                        }
                    }
                }
            },
            scales: {
                x: {
                    type: 'category',
                    labels: Array.from({length: 53}, (_, i) => i.toString()),
                    offset: true,
                    grid: { display: false }
                },
                y: {
                    type: 'category',
                    labels: ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'],
                    offset: true,
                    grid: { display: false }
                }
            },
            onHover: function(event, elements) {
                if (!elements.length) {
                    const tooltipEl = document.getElementById('shared-heatmap-tooltip');
                    if (tooltipEl) tooltipEl.style.opacity = 0;
                }
            }
        },
        plugins: [{
            id: 'gradientDrawer',
            afterDatasetsDraw: function(chart) {
                const dataset = chart.data.datasets[0];
                const meta = chart.getDatasetMeta(0);
                meta.data.forEach((element, index) => {
                    const data = dataset.data[index];
                    if (data.Colors && data.Colors.length > 1) {
                        const ctx = chart.ctx;
                        const gradient = ctx.createLinearGradient(element.x, element.y, element.x + element.width, element.y);
                        const step = 1.0 / data.Colors.length;
                        data.Colors.forEach((color, colorIndex) => {
                            gradient.addColorStop(colorIndex * step, color);
                            gradient.addColorStop((colorIndex + 1) * step, color);
                        });
                        ctx.save();
                        ctx.fillStyle = gradient;
                        ctx.fillRect(element.x, element.y, element.width, element.height);
                        ctx.restore();
                    }
                });
            }
        }]
    });
}
