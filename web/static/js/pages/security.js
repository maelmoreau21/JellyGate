(() => {
    'use strict';

    const uiLocale = window.JGConfig?.locale || undefined;
    let currentPage = 1;
    const pageLimit = 50;
    let currentEvents = [];

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

    function copyToClipboard(text, msg) {
        if (!text) return;
        if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(text).then(() => {
                JG.toast(msg || 'Copié dans le presse-papier', 'success');
            }).catch(() => {
                fallbackCopy(text, msg);
            });
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

        // Health badge update
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
        currentPage = page;
        const category = document.getElementById('security-category')?.value || '';
        const severity = document.getElementById('security-severity')?.value || '';
        const search = document.getElementById('security-search')?.value || '';

        const params = new URLSearchParams({
            page: String(currentPage),
            limit: String(pageLimit),
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

        currentEvents = res.data?.events || [];
        const meta = res.data?.meta || {};
        const total = meta.total || 0;
        const totalPages = meta.total_pages || 1;

        // Update Pagination controls
        const summary = document.getElementById('security-count-summary');
        if (summary) {
            summary.textContent = total > 0 
                ? `Affichage de ${currentEvents.length} sur ${total} événement(s)`
                : `Aucun événement correspondant aux critères`;
        }

        const pageNum = document.getElementById('security-page-num');
        if (pageNum) {
            pageNum.textContent = `Page ${currentPage} / ${totalPages}`;
        }

        const prevBtn = document.getElementById('security-prev-page');
        const nextBtn = document.getElementById('security-next-page');
        if (prevBtn) prevBtn.disabled = currentPage <= 1;
        if (nextBtn) nextBtn.disabled = currentPage >= totalPages;

        if (!currentEvents.length) {
            tbody.innerHTML = `<tr>
                <td colspan="8" class="text-center py-16 text-slate-400">
                    <div class="flex flex-col items-center justify-center gap-2">
                        <svg class="w-8 h-8 text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/></svg>
                        <span class="text-sm font-semibold">Aucun événement trouvé</span>
                        <span class="text-xs text-slate-500">Essayez d'ajuster ou de réinitialiser vos filtres de recherche.</span>
                    </div>
                </td>
            </tr>`;
            return;
        }

        tbody.innerHTML = currentEvents.map((event, index) => {
            const hasIP = event.ip && event.ip !== '--';
            const ipDisplay = hasIP 
                ? `<button class="action-copy-ip inline-flex items-center gap-1 font-mono text-[11px] px-2 py-0.5 rounded-md bg-white/5 hover:bg-indigo-500/20 text-cyan-300 hover:text-cyan-200 border border-white/5 transition-all group" data-ip="${JG.esc(event.ip)}" title="Cliquer pour copier l'IP">
                    <span>${JG.esc(event.ip)}</span>
                    <svg class="w-3 h-3 opacity-40 group-hover:opacity-100 transition-opacity" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                   </button>`
                : `<span class="text-slate-500 font-mono text-[11px]">--</span>`;

            const messageText = event.message || event.metadata || '--';

            return `<tr class="hover:bg-white/[0.02] transition-colors group">
                <td class="px-5 py-3.5 whitespace-nowrap text-slate-300 font-mono text-[11px]">
                    ${JG.esc(dateLabel(event.created_at))}
                </td>
                <td class="px-4 py-3.5 whitespace-nowrap">
                    ${severityBadge(event.severity)}
                </td>
                <td class="px-4 py-3.5 whitespace-nowrap">
                    <div class="flex items-center gap-2">
                        ${categoryBadge(event.category)}
                        <code class="text-[11px] text-purple-300 bg-purple-950/40 border border-purple-500/20 px-1.5 py-0.5 rounded">${JG.esc(event.event_type || '')}</code>
                    </div>
                </td>
                <td class="px-4 py-3.5 whitespace-nowrap">
                    ${event.actor ? `<span class="px-2 py-0.5 rounded bg-white/5 text-slate-200 font-mono text-[11px]">${JG.esc(event.actor)}</span>` : '<span class="text-slate-500">--</span>'}
                </td>
                <td class="px-4 py-3.5 whitespace-nowrap">
                    ${event.target ? `<span class="px-2 py-0.5 rounded bg-white/5 text-slate-300 font-mono text-[11px]">${JG.esc(event.target)}</span>` : '<span class="text-slate-500">--</span>'}
                </td>
                <td class="px-4 py-3.5 whitespace-nowrap">
                    ${ipDisplay}
                </td>
                <td class="px-5 py-3.5 max-w-xs truncate text-slate-300 text-xs" title="${JG.esc(messageText)}">
                    ${JG.esc(messageText)}
                </td>
                <td class="px-4 py-3.5 text-right whitespace-nowrap">
                    <button class="action-inspect-event jg-btn-icon jg-btn-icon-accent p-1.5 rounded-lg hover:bg-white/10 text-indigo-400 hover:text-indigo-300 transition-colors" data-index="${index}" title="Inspecter l'événement">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>
                    </button>
                </td>
            </tr>`;
        }).join('');
    }

    function showEventDetails(event) {
        if (!event) return;

        document.getElementById('modal-event-id').textContent = `ID #${event.id || '--'}`;
        document.getElementById('modal-event-severity').innerHTML = severityBadge(event.severity);
        document.getElementById('modal-event-category').textContent = `${event.category || '--'} (${event.event_type || '--'})`;
        document.getElementById('modal-event-date').textContent = dateLabel(event.created_at);
        document.getElementById('modal-event-ip').textContent = event.ip || '--';
        document.getElementById('modal-event-actor').textContent = event.actor || '--';
        document.getElementById('modal-event-target').textContent = event.target || '--';
        document.getElementById('modal-event-message').textContent = event.message || '--';

        let formattedJson = '{}';
        if (event.metadata) {
            try {
                const parsed = typeof event.metadata === 'string' ? JSON.parse(event.metadata) : event.metadata;
                formattedJson = JSON.stringify(parsed, null, 2);
            } catch (_) {
                formattedJson = String(event.metadata);
            }
        }
        document.getElementById('modal-event-metadata').textContent = formattedJson;

        const copyJsonBtn = document.getElementById('modal-copy-json');
        if (copyJsonBtn) {
            copyJsonBtn.onclick = () => copyToClipboard(formattedJson, 'JSON copié');
        }

        JG.openModal('security-detail-modal');
    }

    function debounce(fn, wait) {
        let timer = 0;
        return (...args) => {
            window.clearTimeout(timer);
            timer = window.setTimeout(() => fn(...args), wait);
        };
    }

    document.addEventListener('DOMContentLoaded', () => {
        const refreshBtn = document.getElementById('security-refresh');
        const refreshIcon = document.getElementById('security-refresh-icon');

        const doRefresh = async () => {
            if (refreshIcon) refreshIcon.classList.add('animate-spin');
            if (refreshBtn) refreshBtn.disabled = true;
            try {
                await Promise.all([loadOverview(), loadEvents(1)]);
            } finally {
                if (refreshIcon) refreshIcon.classList.remove('animate-spin');
                if (refreshBtn) refreshBtn.disabled = false;
            }
        };

        refreshBtn?.addEventListener('click', doRefresh);

        // Search & Filter Listeners
        const searchInput = document.getElementById('security-search');
        const searchClear = document.getElementById('security-search-clear');
        const categorySelect = document.getElementById('security-category');
        const severitySelect = document.getElementById('security-severity');
        const resetFiltersBtn = document.getElementById('security-reset-filters');

        searchInput?.addEventListener('input', (e) => {
            if (searchClear) {
                if (e.target.value.trim() !== '') {
                    searchClear.classList.remove('hidden');
                } else {
                    searchClear.classList.add('hidden');
                }
            }
            debounce(() => loadEvents(1), 250)();
        });

        searchClear?.addEventListener('click', () => {
            if (searchInput) searchInput.value = '';
            searchClear.classList.add('hidden');
            loadEvents(1);
        });

        categorySelect?.addEventListener('change', () => loadEvents(1));
        severitySelect?.addEventListener('change', () => loadEvents(1));

        resetFiltersBtn?.addEventListener('click', () => {
            if (searchInput) searchInput.value = '';
            if (searchClear) searchClear.classList.add('hidden');
            if (categorySelect) categorySelect.value = '';
            if (severitySelect) severitySelect.value = '';
            loadEvents(1);
        });

        // Pagination buttons
        document.getElementById('security-prev-page')?.addEventListener('click', () => {
            if (currentPage > 1) {
                loadEvents(currentPage - 1);
            }
        });

        document.getElementById('security-next-page')?.addEventListener('click', () => {
            loadEvents(currentPage + 1);
        });

        // Delegated table clicks (Copy IP, Inspect Event, Modal Close)
        document.body.addEventListener('click', (e) => {
            const closeTrigger = e.target.closest('[data-modal-close]');
            if (closeTrigger) {
                const modalId = closeTrigger.getAttribute('data-modal-close');
                if (modalId) JG.closeModal(modalId);
                return;
            }

            const copyIpBtn = e.target.closest('.action-copy-ip');
            if (copyIpBtn) {
                const ip = copyIpBtn.getAttribute('data-ip');
                copyToClipboard(ip, `IP ${ip} copiée`);
                return;
            }

            const inspectBtn = e.target.closest('.action-inspect-event');
            if (inspectBtn) {
                const index = parseInt(inspectBtn.getAttribute('data-index'), 10);
                if (!isNaN(index) && currentEvents[index]) {
                    showEventDetails(currentEvents[index]);
                }
                return;
            }
        });

        // Initial load
        doRefresh();
    });
})();

