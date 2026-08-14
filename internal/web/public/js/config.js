// Settings › muscle catalog editor (Feature 4). Groups the flat window._muscles
// list by group and renders each group's muscles with a delete control; new
// muscles are added through the form. All CRUD goes through /api/muscles.

(function () {
    function status(msg, kind) {
        var el = document.getElementById('muscleStatus');
        if (!el) return;
        el.textContent = msg || '';
        el.className = 'small mt-2 ' +
            (kind === 'err' ? 'text-danger' : kind === 'ok' ? 'text-success' : 'text-body-secondary');
    }

    function render(muscles) {
        var wrap = document.getElementById('muscleGroups');
        if (!wrap) return;
        wrap.innerHTML = '';

        var byGroup = {};
        (muscles || []).forEach(function (m) {
            (byGroup[m.Group] = byGroup[m.Group] || []).push(m);
        });
        var groups = Object.keys(byGroup).sort();

        if (groups.length === 0) {
            wrap.innerHTML = '<p class="small text-body-secondary">No muscles defined yet.</p>';
            return;
        }

        groups.forEach(function (g) {
            var section = document.createElement('div');
            section.className = 'mb-3';

            var title = document.createElement('div');
            title.className = 'fw-semibold small mb-1';
            title.textContent = g;
            section.appendChild(title);

            byGroup[g].forEach(function (m) {
                var chip = document.createElement('span');
                chip.className = 'badge rounded-pill text-bg-secondary me-1 mb-1';
                chip.style.fontWeight = '500';
                chip.textContent = m.Name + ' ';

                var x = document.createElement('button');
                x.type = 'button';
                x.className = 'btn-close btn-close-white ms-1 align-middle';
                x.style.fontSize = '0.5rem';
                x.setAttribute('aria-label', 'Remove ' + m.Name);
                x.addEventListener('click', function () { del(m.ID); });

                chip.appendChild(x);
                section.appendChild(chip);
            });
            wrap.appendChild(section);
        });
    }

    function reload() {
        fetch('/api/muscles')
            .then(function (r) { return r.json(); })
            .then(function (m) { window._muscles = m || []; render(window._muscles); })
            .catch(function () { status('Failed to load muscles', 'err'); });
    }

    function add() {
        var groupInp = document.getElementById('newMuscleGroup');
        var nameInp  = document.getElementById('newMuscleName');
        var g = groupInp.value.trim();
        var n = nameInp.value.trim();
        if (!g || !n) { status('Group and muscle are both required', 'err'); return; }
        status('Adding…');
        fetch('/api/muscles', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ Group: g, Name: n }),
        }).then(function (r) {
            if (!r.ok) throw new Error('HTTP ' + r.status);
            nameInp.value = '';
            status('Added ' + n, 'ok');
            reload();
        }).catch(function () { status('Failed to add', 'err'); });
    }

    function del(id) {
        fetch('/api/muscles/' + id, { method: 'DELETE' })
            .then(function (r) {
                if (!r.ok && r.status !== 204) throw new Error('HTTP ' + r.status);
                reload();
            })
            .catch(function () { status('Failed to remove', 'err'); });
    }

    function init() {
        render(window._muscles || []);
        var btn = document.getElementById('addMuscleBtn');
        if (btn) btn.addEventListener('click', add);
        var nameInp = document.getElementById('newMuscleName');
        if (nameInp) {
            nameInp.addEventListener('keydown', function (e) {
                if (e.key === 'Enter') { e.preventDefault(); add(); }
            });
        }
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
