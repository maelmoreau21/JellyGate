(() => {
    const config = window.JGPageI18n || {};

    async function loadI18nReport() {
        const tbody = document.getElementById('i18n-tbody');
        if (!tbody) {
            return;
        }

        const res = await JG.api('/admin/api/i18n/report');
        if (!res || !res.success || !res.data) {
            tbody.innerHTML = `<tr><td colspan="6" class="px-4 py-6 text-red-300">${JG.esc(config.loadError || 'Load error')}</td></tr>`;
            return;
        }

        const rows = (res.data.locales || []).map((row) => {
            const issues =
                (row.missing_template_keys?.length || 0) +
                (row.missing_from_base?.length || 0) +
                (row.placeholder_mismatches?.length || 0) +
                (row.fallback_values?.length || 0);
            const statusClass = issues > 0 ? 'text-amber-300' : 'text-emerald-300';
            return `
                <tr>
                    <td class="px-4 py-3 text-sm text-white font-semibold">${JG.esc(row.locale)}</td>
                    <td class="px-4 py-3 text-sm text-slate-300">${row.total_keys || 0}</td>
                    <td class="px-4 py-3 text-sm ${statusClass}">${(row.missing_template_keys || []).length}</td>
                    <td class="px-4 py-3 text-sm ${statusClass}">${(row.missing_from_base || []).length}</td>
                    <td class="px-4 py-3 text-sm ${statusClass}">${(row.placeholder_mismatches || []).length}</td>
                    <td class="px-4 py-3 text-sm ${statusClass}">${(row.fallback_values || []).length}</td>
                </tr>
            `;
        }).join('');

        tbody.innerHTML = rows || `<tr><td colspan="6" class="px-4 py-6 text-slate-400">${JG.esc(config.noData || 'No data')}</td></tr>`;
    }

    document.addEventListener('DOMContentLoaded', loadI18nReport);
})();