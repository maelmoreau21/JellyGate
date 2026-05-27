(() => {
    const uiLocale = window.JGConfig?.locale || undefined;

    function severityBadge(value) {
        const severity = String(value || 'info').toLowerCase();
        const cls = severity === 'critical' ? 'danger' : (severity === 'warning' ? 'warn' : '');
        return `<span class="jg-ds-tag ${cls}">${JG.esc(severity)}</span>`;
    }

    function dateLabel(raw) {
        if (!raw) return '--';
        const date = new Date(raw);
        return Number.isNaN(date.getTime()) ? raw : date.toLocaleString(uiLocale);
    }

    async function loadOverview() {
        const res = await JG.api('/admin/api/security/overview');
        if (!res?.success) {
            JG.toast(res?.message || 'Vue sécurité indisponible', 'error');
            return;
        }
        const overview = res.data?.overview || {};
        document.querySelectorAll('#security-overview [data-key]').forEach((el) => {
            el.textContent = String(overview[el.dataset.key] || 0);
        });
    }

    async function loadEvents() {
        const category = document.getElementById('security-category')?.value || '';
        const severity = document.getElementById('security-severity')?.value || '';
        const search = document.getElementById('security-search')?.value || '';
        const params = new URLSearchParams({ limit: '80' });
        if (category) params.set('category', category);
        if (severity) params.set('severity', severity);
        if (search) params.set('search', search);

        const res = await JG.api(`/admin/api/security/events?${params.toString()}`);
        const tbody = document.getElementById('security-events-body');
        if (!tbody) return;
        if (!res?.success) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center py-12 text-rose-300">Chargement impossible</td></tr>';
            return;
        }
        const events = res.data?.events || [];
        if (!events.length) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center py-12 text-jg-text-muted">Aucun événement</td></tr>';
            return;
        }
        tbody.innerHTML = events.map((event) => `<tr>
            <td>${JG.esc(dateLabel(event.created_at))}</td>
            <td>${JG.esc(event.category || '')}</td>
            <td><code>${JG.esc(event.event_type || '')}</code></td>
            <td>${severityBadge(event.severity)}</td>
            <td>${JG.esc(event.actor || '--')}</td>
            <td>${JG.esc(event.target || '--')}</td>
            <td>${JG.esc(event.ip || '--')}</td>
            <td>${JG.esc(event.message || event.metadata || '--')}</td>
        </tr>`).join('');
    }

    function debounce(fn, wait) {
        let timer = 0;
        return (...args) => {
            window.clearTimeout(timer);
            timer = window.setTimeout(() => fn(...args), wait);
        };
    }

    document.addEventListener('DOMContentLoaded', () => {
        const refresh = () => {
            loadOverview();
            loadEvents();
        };
        document.getElementById('security-refresh')?.addEventListener('click', refresh);
        document.getElementById('security-category')?.addEventListener('change', loadEvents);
        document.getElementById('security-severity')?.addEventListener('change', loadEvents);
        document.getElementById('security-search')?.addEventListener('input', debounce(loadEvents, 250));
        refresh();
    });
})();
