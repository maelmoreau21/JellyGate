(() => {
    'use strict';

    const uiLocale = window.JGConfig?.locale || undefined;

    // ── Global State ────────────────────────────────────────────────────────────
    const state = {
        activeTab: 'overview', // 'overview', 'audit', 'system'

        // Security Events state
        eventsPage: 1,
        eventsLimit: 50,
        currentEvents: [],

        // Audit Log state
        auditPage: 1,
        auditLimit: 50,
        auditSearch: '',
        auditTotalPages: 1,

        // System Console state
        selectedFile: '',
        files: [],
        lines: [],
        maxLines: 200,
        syslogSearch: '',
        autoRefresh: true,
        autoRefreshInterval: null,
    };

    let auditSearchTimeout;
    let syslogSearchTimeout;
    let secSearchTimeout;

    // ── Helpers ────────────────────────────────────────────────────────────────
    function dateLabel(raw) {
        if (!raw) return '--';
        const date = new Date(raw);
        if (Number.isNaN(date.getTime())) return raw;
        return date.toLocaleString(uiLocale, {
            day: '2-digit',
            month: '2-digit',
            year: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        });
    }

    function formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    function copyToClipboard(text, msg) {
        if (!text) return;
        if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(text).then(() => {
                JG.toast(msg || 'Copié dans le presse-papier', 'success');
            }).catch(() => fallbackCopy(text, msg));
        } else {
            fallbackCopy(text, msg);
        }
    }

    function fallbackCopy(text, msg) {
        const textarea = document.createElement('textarea');
        textarea.value = text;
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        try {
            document.execCommand('copy');
            JG.toast(msg || 'Copié dans le presse-papier', 'success');
        } catch (_) {
            JG.toast('Impossible de copier', 'error');
        }
        document.body.removeChild(textarea);
    }

    // ── 1. SÉCURITÉ & PROTECTION ───────────────────────────────────────────────
    function severityBadge(value) {
        const severity = String(value || 'info').toLowerCase();
        if (severity === 'critical') {
            return `<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-rose-500/15 text-rose-300 border border-rose-500/30 shadow-[0_0_10px_rgba(244,63,94,0.15)]">
                <span class="w-1.5 h-1.5 rounded-full bg-rose-400 animate-pulse"></span>
                Critique
            </span>`;
        }
        if (severity === 'warning') {
            return `<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-amber-500/15 text-amber-300 border border-amber-500/30">
                <span class="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                Alerte
            </span>`;
        }
        return `<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-sky-500/15 text-sky-300 border border-sky-500/30">
            <span class="w-1.5 h-1.5 rounded-full bg-sky-400"></span>
            Info
        </span>`;
    }

    function categoryBadge(category) {
        const cat = String(category || '').toLowerCase();
        let icon = `<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>`;
        let label = category || 'Général';
        let cls = 'bg-slate-500/10 text-slate-300 border-slate-500/20';

        if (cat === 'invite_abuse') {
            icon = `<svg class="w-3 h-3 text-rose-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636"/></svg>`;
            label = 'IP Bloquée';
            cls = 'bg-rose-500/10 text-rose-300 border-rose-500/25';
        } else if (cat === 'captcha') {
            icon = `<svg class="w-3 h-3 text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>`;
            label = 'CAPTCHA';
            cls = 'bg-amber-500/10 text-amber-300 border-amber-500/25';
        } else if (cat === 'invalid_invite') {
            icon = `<svg class="w-3 h-3 text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>`;
            label = 'Invite Invalide';
            cls = 'bg-yellow-500/10 text-yellow-300 border-yellow-500/25';
        } else if (cat === 'admin_login' || cat === 'oidc_login') {
            icon = `<svg class="w-3 h-3 text-indigo-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/></svg>`;
            label = cat === 'oidc_login' ? 'Auth OIDC' : 'Connexion';
            cls = 'bg-indigo-500/10 text-indigo-300 border-indigo-500/25';
        } else if (cat === 'smtp') {
            icon = `<svg class="w-3 h-3 text-sky-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>`;
            label = 'SMTP';
            cls = 'bg-sky-500/10 text-sky-300 border-sky-500/25';
        }

        return `<span class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-lg text-[10px] font-semibold border ${cls}">
            ${icon}
            <span>${JG.esc(label)}</span>
        </span>`;
    }

    async function loadOverview() {
        const res = await JG.api('/admin/api/security/overview');
        if (!res?.success) return;
        const overview = res.data?.overview || {};
        document.querySelectorAll('#security-overview [data-key]').forEach((el) => {
            el.textContent = String(overview[el.dataset.key] || 0);
        });

        const suspicious = overview.suspicious_alerts || 0;
        const blocked = overview.blocked_ips || 0;
        const failures = overview.admin_login_failures || 0;
        const healthBadge = document.getElementById('security-health-badge');

        if (healthBadge) {
            if (suspicious > 0) {
                healthBadge.className = 'px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-rose-500/20 text-rose-400 border border-rose-500/30 flex items-center gap-1.5';
                healthBadge.innerHTML = `<span class="w-1.5 h-1.5 rounded-full bg-rose-400 animate-ping"></span> ${suspicious} alerte(s) critique(s)`;
            } else if (blocked > 0 || failures > 5) {
                healthBadge.className = 'px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-amber-500/20 text-amber-400 border border-amber-500/30 flex items-center gap-1.5';
                healthBadge.innerHTML = `<span class="w-1.5 h-1.5 rounded-full bg-amber-400"></span> Surveillance active`;
            } else {
                healthBadge.className = 'px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 flex items-center gap-1.5';
                healthBadge.innerHTML = `<span class="w-1.5 h-1.5 rounded-full bg-emerald-400"></span> Système sain`;
            }
        }

        const lastUpdated = document.getElementById('security-last-updated');
        if (lastUpdated) {
            lastUpdated.textContent = `Dernière synchro : ${new Date().toLocaleTimeString(uiLocale)}`;
        }
    }

    async function loadEvents(page = 1) {
        state.eventsPage = page;
        const category = document.getElementById('security-category')?.value || '';
        const severity = document.getElementById('security-severity')?.value || '';
        const search = document.getElementById('security-search')?.value || '';

        const params = new URLSearchParams({
            page: String(state.eventsPage),
            limit: String(state.eventsLimit),
        });
        if (category) params.set('category', category);
        if (severity) params.set('severity', severity);
        if (search) params.set('search', search);

        const tbody = document.getElementById('security-events-body');
        if (!tbody) return;
        tbody.innerHTML = `<tr>
            <td colspan="8" class="text-center py-16 text-slate-400">
                <div class="flex flex-col items-center justify-center gap-3">
                    <span class="spinner w-6 h-6 border-2 border-indigo-400 border-t-transparent rounded-full animate-spin"></span>
                    <span>Recherche des événements...</span>
                </div>
            </td>
        </tr>`;

        const res = await JG.api(`/admin/api/security/events?${params.toString()}`);
        if (!res?.success) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center py-12 text-rose-300 font-semibold">Impossible de charger le journal de sécurité</td></tr>';
            return;
        }

        state.currentEvents = res.data?.events || [];
        const meta = res.data?.meta || {};
        const total = meta.total || 0;
        const totalPages = meta.total_pages || 1;

        const summary = document.getElementById('security-count-summary');
        if (summary) {
            summary.textContent = total > 0 
                ? `Affichage de ${state.currentEvents.length} sur ${total} événement(s)`
                : `Aucun événement correspondant aux critères`;
        }

        const pageNum = document.getElementById('security-page-num');
        if (pageNum) pageNum.textContent = `Page ${state.eventsPage} / ${totalPages}`;

        const prevBtn = document.getElementById('security-prev-page');
        const nextBtn = document.getElementById('security-next-page');
        if (prevBtn) prevBtn.disabled = state.eventsPage <= 1;
        if (nextBtn) nextBtn.disabled = state.eventsPage >= totalPages;

        if (!state.currentEvents.length) {
            tbody.innerHTML = `<tr>
                <td colspan="8" class="text-center py-16 text-slate-400">
                    <div class="flex flex-col items-center justify-center gap-2">
                        <svg class="w-8 h-8 text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/></svg>
                        <span class="text-sm font-semibold">Aucun événement trouvé</span>
                        <span class="text-xs text-slate-500">Essayez d'ajuster ou de réinitialiser vos filtres.</span>
                    </div>
                </td>
            </tr>`;
            return;
        }

        tbody.innerHTML = state.currentEvents.map((event, index) => {
            const hasIP = event.ip && event.ip !== '--';
            const ipDisplay = hasIP 
                ? `<button class="action-copy-ip inline-flex items-center gap-1 font-mono text-[11px] px-2 py-0.5 rounded-md bg-white/5 hover:bg-indigo-500/20 text-cyan-300 hover:text-cyan-200 border border-white/5 transition-all group" data-ip="${JG.esc(event.ip)}" title="Cliquer pour copier l'IP">
                    <span>${JG.esc(event.ip)}</span>
                    <svg class="w-3 h-3 opacity-40 group-hover:opacity-100 transition-opacity" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                   </button>`
                : `<span class="text-slate-500 font-mono text-[11px]">--</span>`;

            const messageText = event.message || event.metadata || '--';

            return `<tr class="hover:bg-white/[0.02] transition-colors group">
                <td class="px-5 py-3 whitespace-nowrap text-slate-400 font-mono text-[11px]">${dateLabel(event.created_at)}</td>
                <td class="px-4 py-3 whitespace-nowrap">${severityBadge(event.severity)}</td>
                <td class="px-4 py-3 whitespace-nowrap">${categoryBadge(event.category)}</td>
                <td class="px-4 py-3 whitespace-nowrap font-medium text-slate-200">${JG.esc(event.actor || '--')}</td>
                <td class="px-4 py-3 whitespace-nowrap font-medium text-slate-300">${JG.esc(event.target || '--')}</td>
                <td class="px-4 py-3 whitespace-nowrap">${ipDisplay}</td>
                <td class="px-5 py-3 text-slate-300 text-xs max-w-xs xl:max-w-md truncate" title="${JG.esc(messageText)}">${JG.esc(messageText)}</td>
                <td class="px-4 py-3 text-right whitespace-nowrap">
                    <button class="action-view-detail inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-[11px] font-semibold bg-white/5 hover:bg-white/10 text-slate-300 hover:text-white border border-white/10 transition-all" data-index="${index}">
                        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>
                        <span>Détails</span>
                    </button>
                </td>
            </tr>`;
        }).join('');
    }

    function openEventDetail(event) {
        if (!event) return;
        document.getElementById('modal-event-time').textContent = dateLabel(event.created_at);
        document.getElementById('modal-event-severity').innerHTML = severityBadge(event.severity);
        document.getElementById('modal-event-category').innerHTML = categoryBadge(event.category);
        document.getElementById('modal-event-actor').textContent = event.actor || '--';
        document.getElementById('modal-event-target').textContent = event.target || '--';
        document.getElementById('modal-event-ip').textContent = event.ip || '--';

        let parsedJSON = event.metadata || '';
        try {
            parsedJSON = JSON.stringify(JSON.parse(event.metadata), null, 2);
        } catch (_) {
            parsedJSON = event.metadata || '{}';
        }
        document.getElementById('modal-event-json').textContent = parsedJSON;
        JG.openModal('security-detail-modal');
    }

    // ── 2. JOURNAL D'AUDIT APPLICATIF ──────────────────────────────────────────
    function auditActionBadge(action) {
        const act = String(action || '').toLowerCase();
        let cls = 'bg-slate-500/10 text-slate-300 border-slate-500/20';
        if (act.includes('created') || act.includes('create')) {
            cls = 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30';
        } else if (act.includes('deleted') || act.includes('delete') || act.includes('ban')) {
            cls = 'bg-rose-500/15 text-rose-300 border-rose-500/30';
        } else if (act.includes('updated') || act.includes('update') || act.includes('toggle')) {
            cls = 'bg-cyan-500/15 text-cyan-300 border-cyan-500/30';
        } else if (act.includes('login') || act.includes('auth')) {
            cls = 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30';
        }
        return `<span class="inline-flex items-center px-2 py-0.5 rounded-md text-[11px] font-mono font-semibold border ${cls}">${JG.esc(action || '--')}</span>`;
    }

    async function loadAuditLogs(page = 1) {
        state.auditPage = page;
        const tbody = document.getElementById('audit-table-body');
        if (!tbody) return;

        tbody.innerHTML = `<tr>
            <td colspan="5" class="text-center py-16 text-jg-text-muted">
                <div class="flex flex-col items-center justify-center gap-3">
                    <span class="spinner w-6 h-6 border-2 border-emerald-400 border-t-transparent rounded-full animate-spin"></span>
                    <span>Chargement du journal d'audit...</span>
                </div>
            </td>
        </tr>`;

        try {
            const params = new URLSearchParams({
                page: String(state.auditPage),
                limit: String(state.auditLimit),
            });
            if (state.auditSearch) params.set('search', state.auditSearch);

            const res = await JG.api(`/admin/api/logs?${params.toString()}`);
            if (!res?.success) {
                tbody.innerHTML = '<tr><td colspan="5" class="text-center py-12 text-rose-300">Erreur de chargement du journal d'audit</td></tr>';
                return;
            }

            const data = res.data || {};
            const logs = Array.isArray(data) ? data : (data.logs || []);
            const total = data.total || logs.length;
            state.auditTotalPages = data.total_pages || Math.max(1, Math.ceil(total / state.auditLimit));

            const summary = document.getElementById('audit-count-summary');
            if (summary) {
                summary.textContent = total > 0 
                    ? `Affichage de ${logs.length} sur ${total} action(s) enregistrée(s)`
                    : `Aucune action trouvée`;
            }

            const pageDisplay = document.getElementById('audit-page-display');
            if (pageDisplay) pageDisplay.textContent = `Page ${state.auditPage} / ${state.auditTotalPages}`;

            const prevBtn = document.getElementById('audit-prev-btn');
            const nextBtn = document.getElementById('audit-next-btn');
            if (prevBtn) prevBtn.disabled = state.auditPage <= 1;
            if (nextBtn) nextBtn.disabled = state.auditPage >= state.auditTotalPages;

            if (!logs.length) {
                tbody.innerHTML = `<tr>
                    <td colspan="5" class="text-center py-16 text-jg-text-muted">
                        <div class="flex flex-col items-center justify-center gap-2">
                            <span class="text-sm font-semibold">Aucune entrée d'audit</span>
                        </div>
                    </td>
                </tr>`;
                return;
            }

            tbody.innerHTML = logs.map((entry) => {
                const details = entry.details || entry.metadata || entry.extra || '--';
                return `<tr class="hover:bg-white/[0.02] transition-colors">
                    <td class="px-5 py-3 whitespace-nowrap text-slate-400 font-mono text-[11px]">${dateLabel(entry.created_at)}</td>
                    <td class="px-4 py-3 whitespace-nowrap font-medium text-slate-200">
                        <span class="font-mono text-xs text-indigo-300">@${JG.esc(entry.username || entry.actor || '--')}</span>
                    </td>
                    <td class="px-4 py-3 whitespace-nowrap">${auditActionBadge(entry.action)}</td>
                    <td class="px-4 py-3 whitespace-nowrap text-slate-300 font-mono text-xs">${JG.esc(entry.target || '--')}</td>
                    <td class="px-5 py-3 text-slate-400 text-xs max-w-xs xl:max-w-md truncate" title="${JG.esc(details)}">${JG.esc(details)}</td>
                </tr>`;
            }).join('');

        } catch (err) {
            console.error('Audit load error:', err);
            tbody.innerHTML = '<tr><td colspan="5" class="text-center py-12 text-rose-300">Erreur de communication</td></tr>';
        }
    }

    // ── 3. CONSOLE & LOGS SYSTÈME ──────────────────────────────────────────────
    async function loadSystemLogs(silent = false) {
        const terminal = document.getElementById('syslog-terminal');
        if (!silent && terminal) {
            terminal.innerHTML = '<div class="text-slate-500 animate-pulse">Chargement des logs système...</div>';
        }

        try {
            const params = new URLSearchParams({
                lines: String(state.maxLines),
            });
            if (state.selectedFile) params.set('file', state.selectedFile);

            const res = await fetch(`/admin/api/logs/system?${params.toString()}`);
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            const data = await res.json();
            if (!data.success) throw new Error(data.message || 'Erreur inconnue');

            state.files = data.files || [];
            state.selectedFile = data.selected_file || (state.files[0] ? state.files[0].name : '');
            state.lines = data.lines || [];

            renderLogFilesList();
            renderTerminalLines();

            const btnDownloadSelected = document.getElementById('btn-download-selected-file');
            if (btnDownloadSelected && state.selectedFile) {
                btnDownloadSelected.href = `/admin/api/logs/system/download?file=${encodeURIComponent(state.selectedFile)}`;
                btnDownloadSelected.classList.remove('hidden');
            }

            const currentViewing = document.getElementById('current-viewing-filename');
            if (currentViewing) {
                currentViewing.textContent = state.selectedFile || 'Console Système';
            }
        } catch (err) {
            console.error('System logs load error:', err);
            if (terminal) terminal.innerHTML = `<div class="text-rose-400">Erreur : ${JG.esc(err.message)}</div>`;
        }
    }

    function renderLogFilesList() {
        const container = document.getElementById('log-files-container');
        const countEl = document.getElementById('log-files-count');
        if (!container) return;

        if (countEl) countEl.textContent = `${state.files.length} fichier(s) disponible(s)`;

        if (!state.files.length) {
            container.innerHTML = '<div class="p-3 rounded-xl border border-white/5 bg-black/20 text-center text-xs text-jg-text-muted col-span-full">Aucun fichier journal trouvé.</div>';
            return;
        }

        container.innerHTML = state.files.map(file => {
            const isSelected = file.name === state.selectedFile;
            return `<div class="p-3 rounded-xl border transition-all flex items-center justify-between gap-3 ${isSelected ? 'border-cyan-500/50 bg-cyan-500/10' : 'border-white/5 bg-black/30 hover:border-white/20'}">
                <button type="button" class="btn-select-file flex-1 text-left min-w-0" data-filename="${JG.esc(file.name)}">
                    <div class="text-xs font-mono font-bold truncate ${isSelected ? 'text-cyan-300' : 'text-slate-200'}">${JG.esc(file.name)}</div>
                    <div class="text-[10px] text-slate-400 mt-0.5">${formatBytes(file.size || 0)} · Modifié le ${dateLabel(file.mod_time)}</div>
                </button>
                <a href="/admin/api/logs/system/download?file=${encodeURIComponent(file.name)}" class="p-2 rounded-lg text-slate-400 hover:text-white hover:bg-white/10 transition-colors flex-shrink-0" title="Télécharger">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
                </a>
            </div>`;
        }).join('');

        container.querySelectorAll('.btn-select-file').forEach(btn => {
            btn.onclick = () => {
                state.selectedFile = btn.dataset.filename;
                loadSystemLogs(false);
            };
        });
    }

    function renderTerminalLines() {
        const terminal = document.getElementById('syslog-terminal');
        if (!terminal) return;

        let filtered = state.lines;
        if (state.syslogSearch) {
            const q = state.syslogSearch.toLowerCase();
            filtered = state.lines.filter(l => l.toLowerCase().includes(q));
        }

        if (!filtered.length) {
            terminal.innerHTML = '<div class="text-slate-500 italic">Aucune ligne ne correspond aux critères.</div>';
            return;
        }

        terminal.innerHTML = filtered.map(line => {
            let colorCls = 'text-slate-300';
            if (line.includes('ERROR') || line.includes('error') || line.includes('FAIL')) {
                colorCls = 'text-rose-400 font-bold';
            } else if (line.includes('WARN') || line.includes('warning')) {
                colorCls = 'text-amber-300';
            } else if (line.includes('INFO') || line.includes('info')) {
                colorCls = 'text-sky-300';
            } else if (line.includes('DEBUG')) {
                colorCls = 'text-slate-500';
            }
            return `<div class="${colorCls} py-0.5 leading-relaxed font-mono whitespace-pre-wrap">${JG.esc(line)}</div>`;
        }).join('');

        terminal.scrollTop = terminal.scrollHeight;
    }

    function setupAutoRefresh() {
        if (state.autoRefreshInterval) clearInterval(state.autoRefreshInterval);
        if (state.autoRefresh && state.activeTab === 'system') {
            state.autoRefreshInterval = setInterval(() => {
                loadSystemLogs(true);
            }, 3000);
        }
    }

    // ── Gestion des Onglets ────────────────────────────────────────────────────
    function switchTab(tabName) {
        state.activeTab = tabName;
        document.querySelectorAll('.jg-glass-tabs button[data-sec-tab]').forEach(btn => {
            const active = btn.dataset.secTab === tabName;
            btn.classList.toggle('active', active);
        });

        document.querySelectorAll('.sec-tab-panel').forEach(panel => {
            const id = panel.id;
            const matches = id === `panel-sec-${tabName}`;
            panel.classList.toggle('hidden', !matches);
        });

        window.location.hash = tabName;

        if (tabName === 'overview') {
            loadOverview();
            loadEvents(state.eventsPage);
        } else if (tabName === 'audit') {
            loadAuditLogs(state.auditPage);
        } else if (tabName === 'system') {
            loadSystemLogs(false);
            setupAutoRefresh();
        }

        if (tabName !== 'system' && state.autoRefreshInterval) {
            clearInterval(state.autoRefreshInterval);
            state.autoRefreshInterval = null;
        }
    }

    // ── Initialisation ────────────────────────────────────────────────────────
    document.addEventListener('DOMContentLoaded', () => {
        // Tab buttons
        document.querySelectorAll('.jg-glass-tabs button[data-sec-tab]').forEach(btn => {
            btn.addEventListener('click', () => switchTab(btn.dataset.secTab));
        });

        // Security filters
        document.getElementById('security-category')?.addEventListener('change', () => loadEvents(1));
        document.getElementById('security-severity')?.addEventListener('change', () => loadEvents(1));
        document.getElementById('security-search')?.addEventListener('input', (e) => {
            clearTimeout(secSearchTimeout);
            const clearBtn = document.getElementById('security-search-clear');
            if (clearBtn) clearBtn.classList.toggle('hidden', !e.target.value);
            secSearchTimeout = setTimeout(() => loadEvents(1), 300);
        });
        document.getElementById('security-search-clear')?.addEventListener('click', () => {
            const input = document.getElementById('security-search');
            if (input) {
                input.value = '';
                document.getElementById('security-search-clear')?.classList.add('hidden');
                loadEvents(1);
            }
        });
        document.getElementById('security-reset-filters')?.addEventListener('click', () => {
            const search = document.getElementById('security-search');
            const cat = document.getElementById('security-category');
            const sev = document.getElementById('security-severity');
            if (search) search.value = '';
            if (cat) cat.value = '';
            if (sev) sev.value = '';
            document.getElementById('security-search-clear')?.classList.add('hidden');
            loadEvents(1);
        });

        // Top refresh button
        document.getElementById('security-refresh')?.addEventListener('click', () => {
            const icon = document.getElementById('security-refresh-icon');
            if (icon) icon.classList.add('rotate-180');
            setTimeout(() => { if (icon) icon.classList.remove('rotate-180'); }, 500);

            if (state.activeTab === 'overview') {
                loadOverview();
                loadEvents(state.eventsPage);
            } else if (state.activeTab === 'audit') {
                loadAuditLogs(state.auditPage);
            } else if (state.activeTab === 'system') {
                loadSystemLogs(false);
            }
        });

        // Pagination Security Events
        document.getElementById('security-prev-page')?.addEventListener('click', () => {
            if (state.eventsPage > 1) loadEvents(state.eventsPage - 1);
        });
        document.getElementById('security-next-page')?.addEventListener('click', () => {
            loadEvents(state.eventsPage + 1);
        });

        // Audit events & pagination
        document.getElementById('audit-search')?.addEventListener('input', (e) => {
            clearTimeout(auditSearchTimeout);
            state.auditSearch = e.target.value.trim();
            auditSearchTimeout = setTimeout(() => loadAuditLogs(1), 300);
        });
        document.getElementById('audit-refresh-btn')?.addEventListener('click', () => loadAuditLogs(state.auditPage));
        document.getElementById('audit-prev-btn')?.addEventListener('click', () => {
            if (state.auditPage > 1) loadAuditLogs(state.auditPage - 1);
        });
        document.getElementById('audit-next-btn')?.addEventListener('click', () => {
            if (state.auditPage < state.auditTotalPages) loadAuditLogs(state.auditPage + 1);
        });

        // System logs controls
        document.getElementById('syslog-lines-select')?.addEventListener('change', (e) => {
            state.maxLines = parseInt(e.target.value, 10) || 200;
            loadSystemLogs(false);
        });
        document.getElementById('syslog-search')?.addEventListener('input', (e) => {
            clearTimeout(syslogSearchTimeout);
            state.syslogSearch = e.target.value.trim();
            syslogSearchTimeout = setTimeout(() => renderTerminalLines(), 200);
        });
        document.getElementById('btn-refresh-syslog')?.addEventListener('click', () => loadSystemLogs(false));
        document.getElementById('btn-copy-syslog')?.addEventListener('click', () => {
            copyToClipboard(state.lines.join('\n'), 'Logs copiés dans le presse-papier');
        });
        document.getElementById('btn-toggle-autorefresh')?.addEventListener('click', () => {
            state.autoRefresh = !state.autoRefresh;
            const label = document.getElementById('autorefresh-label');
            const btn = document.getElementById('btn-toggle-autorefresh');
            if (state.autoRefresh) {
                if (label) label.textContent = 'Auto-refresh (3s)';
                if (btn) btn.className = 'jg-btn jg-btn-ghost h-9 px-3 text-xs font-bold flex items-center gap-1.5 border border-emerald-500/30 text-emerald-400';
                setupAutoRefresh();
            } else {
                if (label) label.textContent = 'Auto-refresh désactivé';
                if (btn) btn.className = 'jg-btn jg-btn-ghost h-9 px-3 text-xs font-bold flex items-center gap-1.5 border border-white/10 text-slate-400';
                if (state.autoRefreshInterval) {
                    clearInterval(state.autoRefreshInterval);
                    state.autoRefreshInterval = null;
                }
            }
        });

        // Delegation for copying IPs and opening event details
        document.addEventListener('click', (e) => {
            const copyIPBtn = e.target.closest('.action-copy-ip');
            if (copyIPBtn) {
                e.stopPropagation();
                copyToClipboard(copyIPBtn.dataset.ip, `IP ${copyIPBtn.dataset.ip} copiée !`);
                return;
            }

            const detailBtn = e.target.closest('.action-view-detail');
            if (detailBtn) {
                e.stopPropagation();
                const idx = parseInt(detailBtn.dataset.index, 10);
                if (!Number.isNaN(idx) && state.currentEvents[idx]) {
                    openEventDetail(state.currentEvents[idx]);
                }
            }
        });

        // Copy JSON in modal
        document.getElementById('modal-copy-json')?.addEventListener('click', () => {
            const content = document.getElementById('modal-event-json')?.textContent || '{}';
            copyToClipboard(content, 'JSON copié');
        });

        // Hash-based tab initial selection
        const hash = (window.location.hash || '').replace('#', '').toLowerCase();
        if (hash === 'audit') {
            switchTab('audit');
        } else if (hash === 'system' || hash === 'logs') {
            switchTab('system');
        } else {
            switchTab('overview');
        }
    });
})();
