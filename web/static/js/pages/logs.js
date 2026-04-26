(() => {
    const config = window.JGPageLogs || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;
    const LOG_PRESETS_STORAGE_KEY = 'jg.logs.presets.v1';
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
        category: 'app',
        totalPages: 1,
    };
    let searchTimeout;
    let filterTimeout;

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
            category: state.category,
            export: format,
        });
        window.open(`/admin/api/logs?${params.toString()}`, '_blank');
    }

    function getCurrentFilters() {
        return {
            action: state.action,
            actor: state.actor,
            result: state.result,
            from: state.from,
            to: state.to,
            search: state.search,
            limit: state.limit,
            sort: state.sort,
            order: state.order,
            category: state.category,
        };
    }

    function readStoredPresets() {
        try {
            const raw = localStorage.getItem(LOG_PRESETS_STORAGE_KEY);
            if (!raw) {
                return [];
            }
            const parsed = JSON.parse(raw);
            if (!Array.isArray(parsed)) {
                return [];
            }
            return parsed
                .filter((item) => item && typeof item.name === 'string' && item.filters && typeof item.filters === 'object')
                .map((item) => ({
                    name: item.name.trim(),
                    filters: item.filters,
                }))
                .filter((item) => item.name !== '');
        } catch (_) {
            return [];
        }
    }

    function writeStoredPresets(presets) {
        localStorage.setItem(LOG_PRESETS_STORAGE_KEY, JSON.stringify(presets));
    }

    function renderPresetOptions(selectedName = '') {
        const select = document.getElementById('logs-preset-select');
        const btnDelete = document.getElementById('logs-preset-delete');
        if (!select) {
            return;
        }

        const presets = readStoredPresets();
        const defaultLabel = i18n.presetsLocal || 'Filter presets (local)';
        const options = [`<option value="">${escapeHtml(defaultLabel)}</option>`]
            .concat(presets.map((p) => `<option value="${escapeHtml(p.name)}">${escapeHtml(p.name)}</option>`));
        select.innerHTML = options.join('');
        if (selectedName) {
            select.value = selectedName;
        }

        if (btnDelete) {
            btnDelete.classList.toggle('hidden', !selectedName);
        }
    }

    function applyFiltersToInputs(filters) {
        const filterAction = document.getElementById('filter-action');
        const filterActor = document.getElementById('filter-actor');
        const filterResult = document.getElementById('filter-result');
        const filterFrom = document.getElementById('filter-from');
        const filterTo = document.getElementById('filter-to');
        const searchInput = document.getElementById('search-input');
        const limitSelect = document.getElementById('limit-select');

        state.action = String(filters.action || '').trim();
        state.actor = String(filters.actor || '').trim();
        state.result = String(filters.result || '').trim();
        state.from = String(filters.from || '').trim();
        state.to = String(filters.to || '').trim();
        state.search = String(filters.search || '').trim();
        state.limit = Number.parseInt(filters.limit || state.limit, 10) || state.limit;
        state.sort = String(filters.sort || state.sort).trim() || 'created_at';
        state.order = String(filters.order || state.order).trim() === 'asc' ? 'asc' : 'desc';
        state.category = String(filters.category || state.category).trim() || 'app';
        state.page = 1;

        if (filterAction) {
            filterAction.value = state.action;
        }
        if (filterActor) {
            filterActor.value = state.actor;
        }
        if (filterResult) {
            filterResult.value = state.result;
        }
        if (filterFrom) {
            filterFrom.value = state.from;
        }
        if (filterTo) {
            filterTo.value = state.to;
        }
        if (searchInput) {
            searchInput.value = state.search;
        }
        if (limitSelect) {
            limitSelect.value = String(state.limit);
        }

        document.querySelectorAll('.sort-icon').forEach((icon) => {
            icon.classList.add('hidden');
            if (icon.dataset.col === state.sort) {
                icon.classList.remove('hidden');
                icon.textContent = state.order === 'desc' ? '▼' : '▲';
            }
        });

        updateActiveFilterCount();
        updateCategoryTabUI();
    }

    function saveCurrentPreset() {
        const currentName = document.getElementById('logs-preset-select')?.value || '';
        const nameInput = window.prompt(i18n.presetPromptName || 'Preset name', currentName);
        if (!nameInput) {
            return;
        }
        const name = nameInput.trim();
        if (!name) {
            return;
        }

        const presets = readStoredPresets().filter((p) => p.name !== name);
        presets.push({ name, filters: getCurrentFilters() });
        presets.sort((a, b) => a.name.localeCompare(b.name, uiLocale || 'fr'));
        writeStoredPresets(presets);
        renderPresetOptions(name);
    }

    function deleteSelectedPreset() {
        const select = document.getElementById('logs-preset-select');
        if (!select || !select.value) {
            return;
        }
        const selected = select.value;
        const presets = readStoredPresets().filter((p) => p.name !== selected);
        writeStoredPresets(presets);
        renderPresetOptions('');
    }

    function applySelectedPreset() {
        const select = document.getElementById('logs-preset-select');
        if (!select) {
            return;
        }

        const selected = select.value;
        const btnDelete = document.getElementById('logs-preset-delete');
        if (btnDelete) {
            btnDelete.classList.toggle('hidden', !selected);
        }

        if (!selected) {
            return;
        }

        const preset = readStoredPresets().find((p) => p.name === selected);
        if (!preset) {
            return;
        }
        applyFiltersToInputs(preset.filters || {});
        fetchLogs();
    }

    function refreshFilterStateFromInputs() {
        state.action = document.getElementById('filter-action')?.value.trim() || '';
        state.actor = document.getElementById('filter-actor')?.value.trim() || '';
        state.result = document.getElementById('filter-result')?.value || '';
        state.from = document.getElementById('filter-from')?.value || '';
        state.to = document.getElementById('filter-to')?.value || '';
    }

    function formatDateInputValue(date) {
        const year = date.getFullYear();
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function applyQuickRange(range) {
        const now = new Date();
        const start = new Date(now);
        if (range === 'today') {
            start.setHours(0, 0, 0, 0);
        } else if (range === '24h') {
            start.setDate(start.getDate() - 1);
        } else if (range === '7d') {
            start.setDate(start.getDate() - 7);
        } else if (range === '30d') {
            start.setDate(start.getDate() - 30);
        }

        const filterFrom = document.getElementById('filter-from');
        const filterTo = document.getElementById('filter-to');
        if (!filterFrom || !filterTo) {
            return;
        }

        filterFrom.value = formatDateInputValue(start);
        filterTo.value = formatDateInputValue(now);
        refreshFilterStateFromInputs();
        state.page = 1;
        updateActiveFilterCount();
        fetchLogs();
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
                category: state.category,
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
                    <td class="px-4 py-3 text-xs text-slate-500 max-w-md break-all font-mono" title="${escapeHtml(log.details)}">${escapeHtml(log.details || '-')}</td>
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

        }
    }

    function updateActiveFilterCount() {
        const filters = ['filter-action', 'filter-actor', 'filter-result', 'filter-from', 'filter-to'];
        let count = 0;
        filters.forEach(id => {
            const el = document.getElementById(id);
            if (el && el.value.trim() !== '') count++;
        });

        const badge = document.getElementById('active-filter-count');
        if (badge) {
            badge.textContent = count;
            badge.classList.toggle('hidden', count === 0);
        }
    }

    function updateCategoryTabUI() {
        document.querySelectorAll('.log-category-btn').forEach(btn => {
            if (btn.dataset.category === state.category) {
                btn.classList.add('bg-jg-accent', 'text-black');
                btn.classList.remove('text-jg-text-muted', 'hover:text-jg-text');
            } else {
                btn.classList.remove('bg-jg-accent', 'text-black');
                btn.classList.add('text-jg-text-muted', 'hover:text-jg-text');
            }
        });
    }

    document.addEventListener('DOMContentLoaded', () => {
        // --- Filter Toggle Logic ---
        const btnToggleFilters = document.getElementById('btn-toggle-filters');
        const filterPanel = document.getElementById('filter-panel');
        const toggleIcon = document.getElementById('filter-toggle-icon');

        if (btnToggleFilters && filterPanel && toggleIcon) {
            btnToggleFilters.addEventListener('click', () => {
                const isHidden = filterPanel.classList.toggle('hidden');
                toggleIcon.style.transform = isHidden ? 'rotate(0deg)' : 'rotate(180deg)';
                btnToggleFilters.classList.toggle('bg-white/10', !isHidden);
            });
        }

        // --- Event Listeners for Filters ---
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

        ['filter-action', 'filter-actor', 'filter-result', 'filter-from', 'filter-to'].forEach((id) => {
            const el = document.getElementById(id);
            if (!el) return;

            const handleFastRefresh = () => {
                refreshFilterStateFromInputs();
                state.page = 1;
                updateActiveFilterCount();
                fetchLogs();
            };

            if (id === 'filter-action' || id === 'filter-actor') {
                el.addEventListener('input', () => {
                    clearTimeout(filterTimeout);
                    filterTimeout = setTimeout(handleFastRefresh, 250);
                });
            } else {
                el.addEventListener('change', handleFastRefresh);
            }
        });

        document.getElementById('logs-preset-save')?.addEventListener('click', saveCurrentPreset);
        document.getElementById('logs-preset-delete')?.addEventListener('click', deleteSelectedPreset);
        document.getElementById('logs-preset-select')?.addEventListener('change', applySelectedPreset);

        document.querySelectorAll('#logs-quick-ranges [data-range]').forEach((btn) => {
            btn.addEventListener('click', () => applyQuickRange(btn.dataset.range || ''));
        });

        document.querySelectorAll('.log-category-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                if (state.category === btn.dataset.category) return;
                state.category = btn.dataset.category;
                state.page = 1;
                updateCategoryTabUI();
                fetchLogs();
            });
        });

        // --- Export Listeners ---
        document.getElementById('export-json')?.addEventListener('click', () => triggerExport('json'));
        document.getElementById('export-csv')?.addEventListener('click', () => triggerExport('csv'));

        renderPresetOptions('');
        updateActiveFilterCount();
        fetchLogs();
    });
})();