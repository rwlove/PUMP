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
        + '<td>' + parseFloat(weight).toFixed(1) + '</td>'
        + '<td>' + deleteForm + '</td>'
        + '</tr>';

    document.getElementById('weightList').insertAdjacentHTML('beforeend', row);
}

function setWeights(weights, off, step) {
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

    for (var i = start; i < end; i++) {
        addWeight(i + 1, weights[i].Date, weights[i].Weight, weights[i].ID);
    }

    // Grey out a paging button only when there's no further page that way:
    // "Older" needs entries before the current window (start > 0); "Newer"
    // needs a more-recent page (offset > 0).
    var olderBtn = document.getElementById('weight-older-btn');
    var newerBtn = document.getElementById('weight-newer-btn');
    if (olderBtn) olderBtn.disabled = start <= 0;
    if (newerBtn) newerBtn.disabled = offset <= 0;
}

// Render the log table for an already period-filtered weight set, resetting
// paging to the newest page. Used by Stats so the log honors the selected
// time period (the Older/Newer buttons then page within that filtered set).
function renderWeightTable(weights, step) {
    offset = 0;
    setWeights(weights, 0, step || 10);
}
