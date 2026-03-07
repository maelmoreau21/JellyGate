(() => {
    const config = window.JGPageLogs || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const state = {
        page: 1,
        limit: 50,
        sort: 'created_at',
        order: 'desc',
        search: '',
        action: '',
        actor: '',
        target: '',
        request_id: '',
        result: '',
        from: '',
        to: '',
        totalPages: 1,
    };
    let searchTimeout;

    function escapeHtml(unsafe) {
        if (!unsafe) {
            return '';
        }
        return unsafe.toString()
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }

    function triggerExport(format) {
        const params = new URLSearchParams({
            sort: state.sort,
            order: state.order,
            search: state.search,
            action: state.action,
            actor: state.actor,
            target: state.target,
            request_id: state.request_id,
            result: state.result,
            from: state.from,
            to: state.to,
            export: format,
        });
        window.open(`/admin/api/logs?${params.toString()}`, '_blank');
    }

    function toggleSort(col) {
        if (state.sort === col) {
            state.order = state.order === 'desc' ? 'asc' : 'desc';
        } else {
            state.sort = col;
            state.order = 'desc';
        }

        document.querySelectorAll('.sort-icon').forEach((icon) => {
            icon.classList.add('hidden');
            if (icon.dataset.col === state.sort) {
                icon.classList.remove('hidden');
                icon.textContent = state.order === 'desc' ? '▼' : '▲';
            }
        });

        fetchLogs();
    }

    function changePage(delta) {
        const newPage = state.page + delta;
        if (newPage >= 1 && newPage <= state.totalPages) {
            state.page = newPage;
            fetchLogs();
        }
    }

    async function fetchLogs() {
        try {
            const tbody = document.getElementById('logs-tbody');
            const btnPrev = document.getElementById('btn-prev');
            const btnNext = document.getElementById('btn-next');
            const pageInfo = document.getElementById('pagination-info');

            if (!tbody || !btnPrev || !btnNext || !pageInfo) {
                return;
            }

            btnPrev.disabled = true;
            btnNext.disabled = true;

            const params = new URLSearchParams({
                page: state.page,
                limit: state.limit,
                sort: state.sort,
                order: state.order,
                search: state.search,
                action: state.action,
                actor: state.actor,
                target: state.target,
                request_id: state.request_id,
                result: state.result,
                from: state.from,
                to: state.to,
            });

            const res = await fetch(`/admin/api/logs?${params}`);
            if (!res.ok) {
                throw new Error(i18n.networkError || 'Network error');
            }
            const data = await res.json();

            if (!data.success || !data.data || !data.data.logs || data.data.logs.length === 0) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center py-8 text-slate-400">${escapeHtml(i18n.noResults || 'No results')}</td></tr>`;
                pageInfo.textContent = i18n.noResultsShort || 'No results';
                state.totalPages = 1;
                return;
            }

            const logs = data.data.logs;
            const meta = data.data.meta;
            state.totalPages = meta.total_pages;

            pageInfo.innerHTML = `${escapeHtml(i18n.pageLabel || 'Page')} <span class="text-white font-medium">${meta.page}</span> ${escapeHtml(i18n.ofLabel || 'of')} <span class="text-white font-medium">${meta.total_pages}</span> <span class="mx-2 text-slate-600">|</span> ${escapeHtml(i18n.totalLabel || 'Total')}: ${meta.total}`;
            btnPrev.disabled = meta.page <= 1;
            btnNext.disabled = meta.page >= meta.total_pages;

            tbody.innerHTML = '';
            logs.forEach((log) => {
                const tr = document.createElement('tr');
                tr.className = 'hover:bg-white/5 transition-colors';

                const date = new Date(log.created_at);
                const dateStr = date.toLocaleString(uiLocale, {
                    day: '2-digit', month: '2-digit', year: 'numeric',
                    hour: '2-digit', minute: '2-digit', second: '2-digit',
                });

                let actionBadge = 'bg-slate-500/20 text-slate-300 border-slate-500/30';
                if (log.action.includes('login') || log.action.includes('success')) {
                    actionBadge = 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30';
                } else if (log.action.includes('delete') || log.action.includes('fail') || log.action.includes('error')) {
                    actionBadge = 'bg-red-500/20 text-red-400 border-red-500/30';
                } else if (log.action.includes('create') || log.action.includes('invite')) {
                    actionBadge = 'bg-blue-500/20 text-blue-400 border-blue-500/30';
                } else if (log.action.includes('update') || log.action.includes('modify') || log.action.includes('toggle')) {
                    actionBadge = 'bg-amber-500/20 text-amber-400 border-amber-500/30';
                }

                const actorHtml = log.actor === 'system'
                    ? `<span class="px-2 py-0.5 rounded text-xs bg-slate-700/50 text-slate-300 border border-slate-600">${escapeHtml(i18n.systemActor || 'system')}</span>`
                    : `<span class="font-medium text-slate-200">${escapeHtml(log.actor)}</span>`;

                tr.innerHTML = `
                    <td class="px-4 py-3 text-sm text-slate-400 whitespace-nowrap">${dateStr}</td>
                    <td class="px-4 py-3"><span class="px-2 py-1 rounded text-xs font-medium border ${actionBadge}">${escapeHtml(log.action)}</span></td>
                    <td class="px-4 py-3 text-sm">${actorHtml}</td>
                    <td class="px-4 py-3 text-sm text-slate-300">${escapeHtml(log.target || '-')}</td>
                    <td class="px-4 py-3 text-xs text-cyan-300 font-mono">${escapeHtml(log.request_id || '-')}</td>
                    <td class="px-4 py-3 text-xs text-slate-500 truncate max-w-xs font-mono" title="${escapeHtml(log.details)}">${escapeHtml(log.details || '-')}</td>
                `;
                tbody.appendChild(tr);
            });
        } catch (err) {
            console.error(err);
            const tbody = document.getElementById('logs-tbody');
            if (tbody) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center py-8 text-red-400">${escapeHtml(i18n.loadError || 'Load failed')}</td></tr>`;
            }
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        document.querySelectorAll('[data-sort-col]').forEach((el) => {
            el.addEventListener('click', () => toggleSort(el.dataset.sortCol || 'created_at'));
        });

        document.querySelectorAll('[data-page-delta]').forEach((el) => {
            el.addEventListener('click', () => changePage(parseInt(el.dataset.pageDelta || '0', 10)));
        });

        const searchInput = document.getElementById('search-input');
        if (searchInput) {
            searchInput.addEventListener('input', (event) => {
                clearTimeout(searchTimeout);
                searchTimeout = setTimeout(() => {
                    state.search = event.target.value;
                    state.page = 1;
                    fetchLogs();
                }, 300);
            });
        }

        const limitSelect = document.getElementById('limit-select');
        if (limitSelect) {
            limitSelect.addEventListener('change', (event) => {
                state.limit = parseInt(event.target.value, 10);
                state.page = 1;
                fetchLogs();
            });
        }

        ['filter-action', 'filter-actor', 'filter-target', 'filter-request-id', 'filter-result', 'filter-from', 'filter-to'].forEach((id) => {
            const el = document.getElementById(id);
            if (!el) {
                return;
            }
            el.addEventListener('input', () => {
                state.action = document.getElementById('filter-action')?.value.trim() || '';
                state.actor = document.getElementById('filter-actor')?.value.trim() || '';
                state.target = document.getElementById('filter-target')?.value.trim() || '';
                state.request_id = document.getElementById('filter-request-id')?.value.trim() || '';
                state.result = document.getElementById('filter-result')?.value || '';
                state.from = document.getElementById('filter-from')?.value || '';
                state.to = document.getElementById('filter-to')?.value || '';
                state.page = 1;
                fetchLogs();
            });
            el.addEventListener('change', () => el.dispatchEvent(new Event('input')));
        });

        document.getElementById('export-json')?.addEventListener('click', () => triggerExport('json'));
        document.getElementById('export-csv')?.addEventListener('click', () => triggerExport('csv'));

        fetchLogs();
    });
})();