// Shared helpers loaded on every page (see layout.html) before the
// page-specific scripts. Everything here is a plain function definition —
// nothing touches the DOM at load time.

// escapeHtml escapes &<>"' so a string is safe to interpolate into HTML
// text or attribute context.
function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, function(c) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
}

// todayStr returns today as YYYY-MM-DD, preferring the server's idea of
// "today" (injected as window._serverDate) over the browser clock.
function todayStr() {
    return window._serverDate || new Date().toLocaleDateString('en-CA');
}

// localDateStr formats a Date as local YYYY-MM-DD.
function localDateStr(d) {
    return d.toLocaleDateString('en-CA');
}

// parseDateStr parses YYYY-MM-DD as local midnight (new Date(str) would
// parse it as UTC and shift the day in western timezones).
function parseDateStr(str) {
    var parts = str.split('-').map(Number);
    return new Date(parts[0], parts[1] - 1, parts[2]);
}

// subscribeSetEvents opens the set-lifecycle SSE stream and dispatches
// parsed payloads to the given handlers ({add, update, delete, bulk} —
// all optional — plus open/error passthroughs). Returns the EventSource.
function subscribeSetEvents(handlers) {
    var es = new EventSource('/api/sets/stream');
    ['add', 'update', 'delete', 'bulk'].forEach(function(type) {
        if (!handlers[type]) return;
        es.addEventListener(type, function(ev) {
            handlers[type](JSON.parse(ev.data));
        });
    });
    if (handlers.open) es.onopen = handlers.open;
    // EventSource auto-reconnects; the default handler is a no-op.
    es.onerror = handlers.error || function() {};
    return es;
}
