(() => {
    const config = window.JGPageDashboard || {};
    const i18n = config.i18n || {};

    let registrationsChart = null;
    let invitationsChart = null;

    document.addEventListener('DOMContentLoaded', () => {
        initSidebarToggle();
        
        if (!config.isAdmin) {
            return;
        }

        refreshDashboard();
    });

    function initSidebarToggle() {
        const toggle = document.getElementById('sidebar-toggle');
        if (toggle) {
            toggle.addEventListener('click', () => {
                const sidebar = document.getElementById('sidebar');
                if (sidebar) {
                    sidebar.classList.toggle('open');
                    sidebar.classList.toggle('collapsed');
                }
                const main = document.querySelector('.jg-main');
                if (main) {
                    main.classList.toggle('expanded');
                }
            });
        }
    }

    function refreshDashboard() {
        Promise.all([
            JG.api('/admin/api/users'),
            JG.api('/admin/api/invitations'),
            JG.api('/admin/api/users/dashboard/stats'),
        ]).then(([usersRes, invitationsRes, statsRes]) => {
            if (usersRes && usersRes.success) {
                const users = (usersRes.data && usersRes.data.users) ? usersRes.data.users : (Array.isArray(usersRes.data) ? usersRes.data : []);
                const meta = (usersRes.data && usersRes.data.meta) ? usersRes.data.meta : null;
                updateUsersStats(users, meta);
                renderRecentUsers(users);
            }

            const invitations = (invitationsRes && invitationsRes.success && Array.isArray(invitationsRes.data)) ? invitationsRes.data : [];

            if (statsRes && statsRes.success) {
                const stats = statsRes.data || {};
                const statInvitationsEl = document.getElementById('stat-invitations');

                // Prefer backend aggregate stats when available.
                if (statInvitationsEl) {
                    statInvitationsEl.textContent = stats.invitations ? stats.invitations.total : invitations.length;
                }

                renderHealthStatus(stats.health || {});
                renderRegistrationsChart(stats.registrations || []);
                renderInvitationsChart(stats.invitations || {});
            } else {
                const statInvitationsEl = document.getElementById('stat-invitations');
                if (statInvitationsEl && invitations.length > 0) {
                    statInvitationsEl.textContent = invitations.length;
                }
            }
        }).catch(err => {
            console.error('Dashboard load error:', err);
            ['stat-users', 'stat-active', 'stat-invitations', 'stat-banned'].forEach(id => {
                const el = document.getElementById(id);
                if (el) el.textContent = '—';
            });
        });
    }

    function updateUsersStats(users, meta) {
        const total = (meta && typeof meta.total_global === 'number') ? meta.total_global : ((meta && typeof meta.total === 'number') ? meta.total : users.length);
        document.getElementById('stat-users').textContent = total;
        document.getElementById('stat-active').textContent = users.filter((u) => u.is_active && !u.is_banned).length;
        document.getElementById('stat-banned').textContent = users.filter((u) => u.is_banned).length;
    }

    function parseDate(val) {
        if (!val) return null;
        let s = String(val).trim();
        if (s.includes(' ') && !s.includes('T')) {
            s = s.replace(' ', 'T');
        }
        const d = new Date(s);
        return Number.isNaN(d.getTime()) ? null : d;
    }

    function formatRegistrationDate(val) {
        const d = parseDate(val);
        if (!d) return '<span class="text-slate-500">—</span>';

        const dateStr = d.toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' });
        const timeStr = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });

        const diffMs = Date.now() - d.getTime();
        const diffSec = Math.floor(diffMs / 1000);
        const diffMin = Math.floor(diffSec / 60);
        const diffHours = Math.floor(diffMin / 60);
        const diffDays = Math.floor(diffHours / 24);

        let relBadge = '';
        if (diffDays === 0) {
            if (diffHours === 0) {
                relBadge = diffMin <= 1 ? "À l'instant" : `Il y a ${diffMin} min`;
            } else {
                relBadge = `Il y a ${diffHours}h`;
            }
        } else if (diffDays === 1) {
            relBadge = 'Hier';
        } else if (diffDays < 7) {
            relBadge = `Il y a ${diffDays}j`;
        }

        const relHtml = relBadge
            ? `<span class="inline-block text-[10px] font-semibold text-purple-300 bg-purple-500/10 px-1.5 py-0.5 rounded border border-purple-500/20 whitespace-nowrap">${relBadge}</span>`
            : '';

        return `<div class="flex flex-col gap-0.5">
            <div class="flex items-center gap-1.5 text-xs font-medium text-slate-200">
                <svg class="w-3.5 h-3.5 text-slate-400 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>
                <span>${dateStr}</span>
                ${relHtml}
            </div>
            <span class="text-[11px] text-slate-400 pl-5 font-mono">${timeStr}</span>
        </div>`;
    }

    function renderRecentUsers(users) {
        const tbody = document.getElementById('recent-users-body');
        if (!tbody) return;

        tbody.innerHTML = '';
        const recent = users.slice(0, 5);
        
        if (recent.length === 0) {
            tbody.innerHTML = `<tr><td colspan="5" class="text-center text-slate-500 py-12">${JG.esc(i18n.noUsers || 'Aucun utilisateur pour le moment')}</td></tr>`;
            return;
        }

        recent.forEach((user) => {
            const status = user.is_banned
                ? `<span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-rose-500/10 border border-rose-500/25 text-rose-400"><span class="w-1.5 h-1.5 rounded-full bg-rose-400"></span>${JG.esc(i18n.statusBanned || 'Banni')}</span>`
                : user.is_active
                    ? `<span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-emerald-500/10 border border-emerald-500/25 text-emerald-400"><span class="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>${JG.esc(i18n.statusActive || 'Actif')}</span>`
                    : `<span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-amber-500/10 border border-amber-500/25 text-amber-400"><span class="w-1.5 h-1.5 rounded-full bg-amber-400"></span>${JG.esc(i18n.statusInactive || 'Inactif')}</span>`;

            const displayName = user.display_name || user.jellyfin_name || user.username || ('#' + user.id);
            const secondaryText = user.email || (user.username && user.username !== displayName ? '@' + user.username : '');
            
            let avatarHtml = `<div class="w-9 h-9 rounded-xl bg-gradient-to-br from-purple-500/20 to-indigo-500/20 border border-purple-500/30 flex items-center justify-center font-black text-purple-300 text-xs shadow-sm flex-shrink-0">${JG.esc(displayName.charAt(0).toUpperCase())}</div>`;
            if (user.jellyfin_id && user.jellyfin_primary_image_tag) {
                const avatarUrl = `/admin/api/users/${user.id}/avatar?tag=${user.jellyfin_primary_image_tag}`;
                avatarHtml = `<img src="${avatarUrl}" class="w-9 h-9 rounded-xl object-cover border border-white/10 shadow-sm flex-shrink-0" alt="${JG.esc(displayName)}" onerror="this.style.display='none';if(this.nextElementSibling)this.nextElementSibling.style.display='flex';">`
                           + `<div class="w-9 h-9 rounded-xl bg-gradient-to-br from-purple-500/20 to-indigo-500/20 border border-purple-500/30 items-center justify-center font-black text-purple-300 text-xs shadow-sm flex-shrink-0 hidden">${JG.esc(displayName.charAt(0).toUpperCase())}</div>`;
            }

            const inviterHtml = user.invited_by
                ? `<span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-purple-500/10 border border-purple-500/20 text-xs font-medium text-purple-300"><svg class="w-3.5 h-3.5 text-purple-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 5v2m0 4v2m0 4v2M5 5a2 2 0 00-2 2v3a2 2 0 110 4v3a2 2 0 002 2h14a2 2 0 002-2v-3a2 2 0 110-4V7a2 2 0 00-2-2H5z" /></svg>${JG.esc(user.invited_by)}</span>`
                : `<span class="text-xs text-slate-500 italic">Direct (Admin)</span>`;

            tbody.innerHTML += `<tr class="hover:bg-white/[0.03] transition-colors">
                <td class="px-6 py-4">
                    <div class="flex items-center gap-3">
                        ${avatarHtml}
                        <div class="flex flex-col min-w-0">
                            <span class="font-bold text-sm text-slate-100 truncate">${JG.esc(displayName)}</span>
                            ${secondaryText ? `<span class="text-xs text-slate-400 truncate">${JG.esc(secondaryText)}</span>` : ''}
                        </div>
                    </div>
                </td>
                <td class="px-6 py-4">${status}</td>
                <td class="px-6 py-4">${inviterHtml}</td>
                <td class="px-6 py-4">${formatRegistrationDate(user.created_at)}</td>
                <td class="px-6 py-4 text-right">
                    <a href="/admin/users?search=${encodeURIComponent(user.username || displayName)}" class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 text-xs font-medium text-slate-300 hover:text-white border border-white/5 transition-all" title="Gérer ce compte">
                        <span>Gérer</span>
                        <svg class="w-3.5 h-3.5 text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/></svg>
                    </a>
                </td>
            </tr>`;
        });
    }

    function renderHealthStatus(health) {
        const toBoolStatus = (value) => {
            if (typeof value === 'boolean') return value;
            if (typeof value === 'number') return value > 0;
            if (typeof value === 'string') {
                const normalized = value.trim().toLowerCase();
                if (['true', 'ok', 'up', 'healthy', 'online', '1', 'enabled'].includes(normalized)) return true;
                if (['false', 'ko', 'down', 'unhealthy', 'offline', '0', 'disabled', 'error'].includes(normalized)) return false;
            }
            return null;
        };

        const updateLED = (id, status) => {
            const el = document.getElementById(id);
            if (!el) return;
            el.className = 'w-3 h-3 rounded-full transition-all duration-700';
            if (status === true) {
                el.classList.add('bg-emerald-500', 'shadow-[0_0_10px_rgba(16,185,129,0.6)]');
            } else if (status === false) {
                el.classList.add('bg-rose-500', 'shadow-[0_0_10px_rgba(244,63,94,0.6)]');
            } else {
                el.classList.add('bg-slate-500', 'opacity-30');
            }
        };

        const normalizedHealth = {
            database: toBoolStatus(health.database ?? health.db ?? health.DB),
            jellyfin: toBoolStatus(health.jellyfin ?? health.jf ?? health.JF),
            authentik: toBoolStatus(health.authentik ?? health.auth ?? health.AK),
        };

        updateLED('health-db', normalizedHealth.database);
        updateLED('health-jellyfin', normalizedHealth.jellyfin);
        updateLED('health-authentik', normalizedHealth.authentik);
    }

    function renderRegistrationsChart(data) {
        const ctx = document.getElementById('registrationsChart');
        if (!ctx) return;

        // Fill missing days over the last 30 days.
        const labels = [];
        const values = [];
        const today = new Date();
        
        const dataMap = {};
        data.forEach(d => dataMap[d.day] = d.count);

        for (let i = 29; i >= 0; i--) {
            const d = new Date();
            d.setDate(today.getDate() - i);
            const dateStr = d.toISOString().split('T')[0];
            labels.push(new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }).format(d));
            values.push(dataMap[dateStr] || 0);
        }

        if (registrationsChart) registrationsChart.destroy();

        registrationsChart = new Chart(ctx, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: i18n.chartRegistrationsLabel || 'Signups',
                    data: values,
                    borderColor: '#22d3ee', // Cyan 400
                    backgroundColor: 'rgba(34, 211, 238, 0.1)',
                    borderWidth: 3,
                    fill: true,
                    tension: 0.4,
                    pointRadius: 0,
                    pointHoverRadius: 6,
                    pointHoverBackgroundColor: '#22d3ee',
                    pointHoverBorderColor: '#fff',
                    pointHoverBorderWidth: 2,
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        mode: 'index',
                        intersect: false,
                        backgroundColor: '#1e293b',
                        titleColor: '#94a3b8',
                        bodyColor: '#f1f5f9',
                        borderColor: '#334155',
                        borderWidth: 1,
                        padding: 12,
                        cornerRadius: 8,
                    }
                },
                scales: {
                    x: {
                        display: true,
                        grid: { display: false },
                        ticks: {
                            color: '#64748b',
                            font: { size: 10 },
                            maxRotation: 0,
                            autoSkip: true,
                            maxTicksLimit: 7
                        }
                    },
                    y: {
                        display: true,
                        beginAtZero: true,
                        grid: {
                            color: 'rgba(148, 163, 184, 0.05)',
                        },
                        ticks: {
                            color: '#64748b',
                            font: { size: 10 },
                            stepSize: 1,
                            precision: 0
                        }
                    }
                }
            }
        });
    }

    function renderInvitationsChart(stats) {
        const ctx = document.getElementById('invitationsChart');
        if (!ctx) return;

        const data = [
            stats.active || 0,
            stats.used || 0,
            stats.expired || 0
        ];

        if (invitationsChart) invitationsChart.destroy();

        invitationsChart = new Chart(ctx, {
            type: 'doughnut',
            data: {
                labels: [
                    i18n.inviteActive || 'Active',
                    i18n.inviteUsed || 'Used',
                    i18n.inviteExpired || 'Expired'
                ],
                datasets: [{
                    data: data,
                    backgroundColor: [
                        '#10b981', // Emerald 500
                        '#0ea5e9', // Cyan 500 (ou Indigo 500)
                        '#f43f5e'  // Rose 500
                    ],
                    borderWidth: 0,
                    hoverOffset: 15,
                    cutout: '75%'
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: {
                        position: 'bottom',
                        labels: {
                            color: '#94a3b8',
                            usePointStyle: true,
                            pointStyle: 'circle',
                            padding: 20,
                            font: { size: 11, weight: '600' }
                        }
                    },
                    tooltip: {
                        backgroundColor: '#1e293b',
                        padding: 12,
                        cornerRadius: 8,
                        displayColors: false
                    }
                }
            }
        });
    }
})();