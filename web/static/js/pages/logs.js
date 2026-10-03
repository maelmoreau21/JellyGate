(() => {
    const config = window.JGPageLogs || {};
    const i18n = config.i18n || {};
    const uiLocale = config.uiLocale || undefined;

    // ── État global ────────────────────────────────────────────────────────────
    const state = {
        activeTab: 'system', // 'system' ou 'audit'
        
        // System logs state
        selectedFile: '',
        files: [],
        lines: [],
        maxLines: 200,
        syslogSearch: '',
        autoRefresh: true,
        autoRefreshInterval: null,
        
        // Audit logs state
        page: 1,
        limit: 50,
        sort: 'created_at',
        order: 'desc',
        search: '',
        totalPages: 1,
    };

    let auditSearchTimeout;
    let syslogSearchTimeout;

    function escapeHtml(unsafe) {
        if (!unsafe) return '';
        return unsafe.toString()
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#039;');
    }

    function formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    function formatDate(dateStr) {
        if (!dateStr) return '-';
        try {
            const d = new Date(dateStr);
            return d.toLocaleString(uiLocale, {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                second: '2-digit'
            });
        } catch (_) {
            return dateStr;
        }
    }

    // ── 1. LOGS SYSTÈME RÉELS ──────────────────────────────────────────────────

    async function loadSystemLogs(silent = false) {
        const terminal = document.getElementById('syslog-terminal');
        if (!silent && terminal) {
            terminal.innerHTML = '<div class="text-slate-500 animate-pulse">Chargement des logs système...</div>';
        }

        try {
            const params = new URLSearchParams({
                lines: state.maxLines,
            });
            if (state.selectedFile) {
                params.set('file', state.selectedFile);
            }

            const res = await fetch(`/admin/api/logs/system?${params.toString()}`);
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            const data = await res.json();

            if (!data.success) throw new Error(data.message || 'Erreur inconnue');

            state.files = data.files || [];
            state.selectedFile = data.selected_file || (state.files[0] ? state.files[0].name : '');
            state.lines = data.lines || [];

            renderLogFilesList();
            renderTerminalLines();

            // Mettre à jour le bouton de téléchargement du fichier sélectionné
            const btnDownloadSelected = document.getElementById('btn-download-selected-file');
            if (btnDownloadSelected && state.selectedFile) {
                btnDownloadSelected.href = `/admin/api/logs/system/download?file=${encodeURIComponent(state.selectedFile)}`;
                btnDownloadSelected.classList.remove('hidden');
            }

            const viewingLabel = document.getElementById('current-viewing-filename');
            if (viewingLabel) {
                viewingLabel.textContent = state.selectedFile || 'Console Système';
            }
        } catch (err) {
            if (terminal) {
                terminal.innerHTML = `<div class="text-rose-400 font-bold">Erreur de chargement des logs: ${escapeHtml(err.message)}</div>`;
            }
        }
    }

    function renderLogFilesList() {
        const container = document.getElementById('log-files-container');
        const countLabel = document.getElementById('log-files-count');
        if (!container) return;

        if (countLabel) {
            countLabel.textContent = `${state.files.length} fichier(s) de journalisation`;
        }

        if (state.files.length === 0) {
            container.innerHTML = `<div class="p-4 rounded-xl border border-white/5 bg-black/20 text-center text-xs text-jg-text-muted col-span-full">Aucun fichier de log sur le disque</div>`;
            return;
        }

        container.innerHTML = state.files.map(f => {
            const isSelected = f.name === state.selectedFile;
            return `
                <div class="p-3.5 rounded-xl border ${isSelected ? 'border-jg-accent bg-jg-accent/10 shadow-lg shadow-purple-500/10' : 'border-white/5 bg-black/20 hover:border-white/20'} flex items-center justify-between gap-3 transition-all">
                    <div class="flex items-center gap-2.5 min-w-0 cursor-pointer flex-1 select-log-file-btn" data-file="${escapeHtml(f.name)}">
                        <div class="w-8 h-8 rounded-lg ${f.is_current ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30' : 'bg-white/5 text-slate-400'} flex items-center justify-center shrink-0">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" /></svg>
                        </div>
                        <div class="min-w-0 flex-1">
                            <div class="flex items-center gap-2">
                                <span class="text-xs font-mono font-bold text-slate-200 truncate">${escapeHtml(f.name)}</span>
                                ${f.is_current ? '<span class="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-[10px] font-bold">Actif</span>' : ''}
                            </div>
                            <div class="text-[11px] text-slate-400 mt-0.5">${formatBytes(f.size)} • ${formatDate(f.mod_time)}</div>
                        </div>
                    </div>

                    <a href="/admin/api/logs/system/download?file=${encodeURIComponent(f.name)}" class="jg-btn jg-btn-ghost p-2 text-xs border border-white/10 rounded-lg hover:bg-white/10 shrink-0" title="Télécharger ce fichier .log">
                        <svg class="w-4 h-4 text-slate-300" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>
                    </a>
                </div>
            `;
        }).join('');

        container.querySelectorAll('.select-log-file-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                state.selectedFile = btn.getAttribute('data-file');
                loadSystemLogs();
            });
        });
    }

    function renderTerminalLines() {
        const terminal = document.getElementById('syslog-terminal');
        if (!terminal) return;

        let linesToDisplay = state.lines;
        if (state.syslogSearch) {
            const filter = state.syslogSearch.toLowerCase();
            linesToDisplay = linesToDisplay.filter(l => l.toLowerCase().includes(filter));
        }

        if (linesToDisplay.length === 0) {
            terminal.innerHTML = '<div class="text-slate-500 italic">Aucune ligne de journal correspondant aux critères.</div>';
            return;
        }

        const formatted = linesToDisplay.map((line, idx) => {
            let colorClass = 'text-slate-300';
            if (line.includes('level=ERROR') || line.includes('level=error') || line.includes('ERR')) {
                colorClass = 'text-rose-400 font-semibold bg-rose-950/20';
            } else if (line.includes('level=WARN') || line.includes('level=warn') || line.includes('WARN')) {
                colorClass = 'text-amber-300 bg-amber-950/20';
            } else if (line.includes('level=INFO') || line.includes('level=info')) {
                colorClass = 'text-slate-300';
            }

            return `<div class="py-0.5 px-2 hover:bg-white/5 rounded font-mono text-[11px] whitespace-pre-wrap break-all ${colorClass}"><span class="text-slate-600 select-none mr-3">${idx + 1}</span>${escapeHtml(line)}</div>`;
        }).join('');

        terminal.innerHTML = formatted;
        terminal.scrollTop = terminal.scrollHeight;
    }

    function toggleAutoRefresh() {
        state.autoRefresh = !state.autoRefresh;
        const btn = document.getElementById('btn-toggle-autorefresh');
        const label = document.getElementById('autorefresh-label');

        if (state.autoRefresh) {
            if (btn) {
                btn.className = 'jg-btn jg-btn-ghost h-9 px-3 text-xs font-bold flex items-center gap-1.5 border border-emerald-500/30 text-emerald-400';
            }
            if (label) label.textContent = 'Auto-refresh (3s)';
            startAutoRefreshTimer();
        } else {
            if (btn) {
                btn.className = 'jg-btn jg-btn-ghost h-9 px-3 text-xs font-bold flex items-center gap-1.5 border border-slate-500/30 text-slate-400';
            }
            if (label) label.textContent = 'En pause';
            stopAutoRefreshTimer();
        }
    }

    function startAutoRefreshTimer() {
        stopAutoRefreshTimer();
        state.autoRefreshInterval = setInterval(() => {
            if (state.activeTab === 'system' && state.autoRefresh) {
                loadSystemLogs(true);
            }
        }, 3000);
    }

    function stopAutoRefreshTimer() {
        if (state.autoRefreshInterval) {
            clearInterval(state.autoRefreshInterval);
            state.autoRefreshInterval = null;
        }
    }

    // ── 2. JOURNAL D'AUDIT APPLICATIF ──────────────────────────────────────────

    async function loadAuditLogs() {
        const tbody = document.getElementById('logs-tbody');
        if (tbody) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="py-20 text-center">
                        <div class="flex flex-col items-center gap-3">
                            <span class="spinner w-10 h-10 border-2 border-jg-accent border-t-transparent animate-spin rounded-full"></span>
                            <span class="text-jg-text-muted animate-pulse">${escapeHtml(i18n.loadError ? 'Chargement...' : 'Chargement...')}</span>
                        </div>
                    </td>
                </tr>
            `;
        }

        try {
            const params = new URLSearchParams({
                page: state.page,
                limit: state.limit,
                sort: state.sort,
                order: state.order,
                search: state.search,
            });

            const res = await fetch(`/admin/api/logs?${params.toString()}`);
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            const data = await res.json();

            if (!data.success) throw new Error(data.message || 'Erreur');

            const rows = data.data || [];
            const pagination = data.pagination || {};
            state.totalPages = pagination.total_pages || 1;

            renderAuditTable(rows);
            renderPagination(pagination);
        } catch (err) {
            if (tbody) {
                tbody.innerHTML = `
                    <tr>
                        <td colspan="6" class="py-12 text-center text-rose-400 font-bold">
                            Erreur de chargement: ${escapeHtml(err.message)}
                        </td>
                    </tr>
                `;
            }
        }
    }

    function renderAuditTable(rows) {
        const tbody = document.getElementById('logs-tbody');
        if (!tbody) return;

        if (rows.length === 0) {
            tbody.innerHTML = `
                <tr>
                    <td colspan="6" class="py-12 text-center text-slate-500">
                        ${escapeHtml(i18n.noResults || 'Aucun événement d\'audit enregistré.')}
                    </td>
                </tr>
            `;
            return;
        }

        tbody.innerHTML = rows.map(r => `
            <tr class="hover:bg-white/[0.02] transition-colors border-b border-white/5">
                <td class="px-6 py-4 whitespace-nowrap text-xs text-slate-400 font-mono">${formatDate(r.created_at)}</td>
                <td class="px-6 py-4 whitespace-nowrap">
                    <span class="px-2.5 py-1 rounded-md text-xs font-mono font-bold bg-purple-500/10 text-purple-300 border border-purple-500/20">${escapeHtml(r.action)}</span>
                </td>
                <td class="px-6 py-4 whitespace-nowrap text-xs font-bold text-slate-200">${escapeHtml(r.actor || '-')}</td>
                <td class="px-6 py-4 whitespace-nowrap text-xs text-slate-400">${escapeHtml(r.target || '-')}</td>
                <td class="px-6 py-4 whitespace-nowrap text-xs font-mono text-slate-500">${escapeHtml(r.request_id || '-')}</td>
                <td class="px-6 py-4 text-xs text-slate-300 break-all">${escapeHtml(r.details || '-')}</td>
            </tr>
        `).join('');
    }

    function renderPagination(pagination) {
        const info = document.getElementById('pagination-info');
        const btnPrev = document.getElementById('btn-prev');
        const btnNext = document.getElementById('btn-next');

        if (info) {
            info.textContent = `Page ${pagination.page || state.page} / ${pagination.total_pages || 1} (${pagination.total_count || 0} événements)`;
        }
        if (btnPrev) {
            btnPrev.disabled = (pagination.page || state.page) <= 1;
        }
        if (btnNext) {
            btnNext.disabled = (pagination.page || state.page) >= (pagination.total_pages || 1);
        }
    }

    function triggerAuditExport(format) {
        const params = new URLSearchParams({
            sort: state.sort,
            order: state.order,
            search: state.search,
            export: format,
        });
        window.open(`/admin/api/logs?${params.toString()}`, '_blank');
    }

    // ── 3. INITIALISATION & ÉVÉNEMENTS ─────────────────────────────────────────

    function switchTab(tab) {
        state.activeTab = tab;
        const btnSystem = document.getElementById('tab-btn-system');
        const btnAudit = document.getElementById('tab-btn-audit');
        const panelSystem = document.getElementById('panel-system-logs');
        const panelAudit = document.getElementById('panel-audit-logs');

        if (tab === 'system') {
            if (btnSystem) {
                btnSystem.className = 'log-tab-btn px-5 py-2.5 rounded-lg text-xs font-bold transition-all bg-jg-accent text-always-white shadow-lg shadow-purple-500/20';
            }
            if (btnAudit) {
                btnAudit.className = 'log-tab-btn px-5 py-2.5 rounded-lg text-xs font-bold transition-all text-jg-text-muted hover:text-jg-text';
            }
            if (panelSystem) panelSystem.classList.remove('hidden');
            if (panelAudit) panelAudit.classList.add('hidden');

            loadSystemLogs();
            if (state.autoRefresh) startAutoRefreshTimer();
        } else {
            if (btnAudit) {
                btnAudit.className = 'log-tab-btn px-5 py-2.5 rounded-lg text-xs font-bold transition-all bg-jg-accent text-always-white shadow-lg shadow-purple-500/20';
            }
            if (btnSystem) {
                btnSystem.className = 'log-tab-btn px-5 py-2.5 rounded-lg text-xs font-bold transition-all text-jg-text-muted hover:text-jg-text';
            }
            if (panelSystem) panelSystem.classList.add('hidden');
            if (panelAudit) panelAudit.classList.remove('hidden');

            stopAutoRefreshTimer();
            loadAuditLogs();
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        // Tab switching
        document.getElementById('tab-btn-system')?.addEventListener('click', () => switchTab('system'));
        document.getElementById('tab-btn-audit')?.addEventListener('click', () => switchTab('audit'));

        // System logs controls
        document.getElementById('btn-toggle-autorefresh')?.addEventListener('click', toggleAutoRefresh);
        document.getElementById('btn-refresh-syslog')?.addEventListener('click', () => loadSystemLogs());
        
        document.getElementById('syslog-lines-select')?.addEventListener('change', (e) => {
            state.maxLines = parseInt(e.target.value, 10) || 200;
            loadSystemLogs();
        });

        document.getElementById('syslog-search')?.addEventListener('input', (e) => {
            clearTimeout(syslogSearchTimeout);
            syslogSearchTimeout = setTimeout(() => {
                state.syslogSearch = e.target.value.trim();
                renderTerminalLines();
            }, 200);
        });

        document.getElementById('btn-copy-syslog')?.addEventListener('click', () => {
            const terminal = document.getElementById('syslog-terminal');
            if (terminal) {
                navigator.clipboard.writeText(terminal.innerText).then(() => {
                    const originalText = document.getElementById('btn-copy-syslog').title;
                    alert('Lignes de logs copiées dans le presse-papier !');
                });
            }
        });

        // Audit logs controls
        document.getElementById('audit-search-input')?.addEventListener('input', (e) => {
            clearTimeout(auditSearchTimeout);
            auditSearchTimeout = setTimeout(() => {
                state.search = e.target.value.trim();
                state.page = 1;
                loadAuditLogs();
            }, 300);
        });

        document.getElementById('export-json')?.addEventListener('click', () => triggerAuditExport('json'));
        document.getElementById('export-csv')?.addEventListener('click', () => triggerAuditExport('csv'));

        document.getElementById('btn-prev')?.addEventListener('click', () => {
            if (state.page > 1) {
                state.page--;
                loadAuditLogs();
            }
        });

        document.getElementById('btn-next')?.addEventListener('click', () => {
            if (state.page < state.totalPages) {
                state.page++;
                loadAuditLogs();
            }
        });

        // Démarrage initial sur l'onglet système
        switchTab('system');
    });
})();