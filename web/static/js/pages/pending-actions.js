(() => {
    const uiLocale = window.JGConfig?.locale || undefined;

    function dateLabel(raw) {
        if (!raw) return '--';
        const date = new Date(raw);
        return Number.isNaN(date.getTime()) ? raw : date.toLocaleString(uiLocale);
    }

    function renderList(id, items, renderer, empty) {
        const el = document.getElementById(id);
        if (!el) return;
        if (!items || !items.length) {
            el.innerHTML = `<div class="jg-ds-empty">${JG.esc(empty || 'Rien à traiter')}</div>`;
            return;
        }
        el.innerHTML = items.map(renderer).join('');
    }

    function item(title, meta, status) {
        return `<div class="jg-ds-list-item">
            <span>
                <strong>${JG.esc(title || '--')}</strong>
                <small>${JG.esc(meta || '')}</small>
            </span>
            ${status ? `<em>${JG.esc(status)}</em>` : ''}
        </div>`;
    }

    async function loadPending() {
        const res = await JG.api('/admin/api/pending-actions');
        if (!res?.success) {
            JG.toast(res?.message || 'Actions à traiter indisponibles', 'error');
            return;
        }
        const data = res.data || {};
        const summary = data.summary || {};
        document.querySelectorAll('#pending-summary [data-key]').forEach((el) => {
            el.textContent = String(summary[el.dataset.key] || 0);
        });

        renderList('pending-expiring-accounts', data.expiring_accounts, (row) => {
            return item(row.username, `${row.email || 'sans e-mail'} · ${dateLabel(row.expires_at)}`, row.action || 'disable');
        }, 'Aucun compte proche expiration');

        renderList('pending-expiring-invitations', data.expiring_invitations, (row) => {
            const uses = `${row.used_count || 0}/${row.max_uses > 0 ? row.max_uses : '∞'}`;
            return item(row.label || row.code, `${uses} · ${dateLabel(row.expires_at)}`, row.created_by || 'system');
        }, 'Aucune invitation proche expiration');

        renderList('pending-smtp-errors', data.smtp_errors, (row) => {
            return item(row.action || row.message || 'Erreur SMTP', row.details || row.message || '--', dateLabel(row.created_at));
        }, 'Aucune erreur SMTP récente');
    }

    document.addEventListener('DOMContentLoaded', () => {
        document.getElementById('pending-refresh')?.addEventListener('click', loadPending);
        loadPending();
    });
})();
