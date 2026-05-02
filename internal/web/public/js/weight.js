var offset = 0;

function setToday() {
    var today = new Date().toJSON().slice(0, 10);
    document.getElementById("todayDate").value = today;
}

function addWeight(i, date, weight, id) {
    // Delete via POST form to avoid GET-based deletion (CSRF risk)
    var deleteForm = '<form action="/wdel/" method="post" style="display:inline;">'
        + '<input type="hidden" name="id" value="' + id + '">'
        + '<button type="submit" class="btn btn-sm del-set-button" title="Delete">'
        + '<i class="bi bi-x-square"></i></button></form>';

    var row = '<tr>'
        + '<td class="ps-3" style="opacity:45%;">' + i + '.</td>'
        + '<td>' + date + '</td>'
        + '<td>' + weight + '</td>'
        + '<td>' + deleteForm + '</td>'
        + '</tr>';

    document.getElementById('weightList').insertAdjacentHTML('beforeend', row);
}

function setWeights(weights, wcolor, off, step) {
    offset = Math.max(0, offset + off);

    var len  = weights.length;
    var end  = Math.max(0, len - offset * step);
    var start = Math.max(0, end - step);

    // Clamp offset if we've gone past the beginning
    if (end === 0 && offset > 0) {
        offset = Math.ceil(len / step) - 1;
        end   = Math.max(0, len - offset * step);
        start = Math.max(0, end - step);
    }

    document.getElementById('weightList').innerHTML = '';

    var dates = [], ws = [];
    for (var i = start; i < end; i++) {
        dates.push(weights[i].Date);
        ws.push(weights[i].Weight);
        addWeight(i + 1, weights[i].Date, weights[i].Weight, weights[i].ID);
    }
    weightChart('weight-chart', dates, ws, wcolor, true);
}
