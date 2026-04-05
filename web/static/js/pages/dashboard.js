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
            JG.api('/admin/api/dashboard/stats'),
        ]).then(([usersRes, invitationsRes, statsRes]) => {
            if (usersRes && usersRes.success) {
                const users = (usersRes.data && usersRes.data.users) ? usersRes.data.users : (Array.isArray(usersRes.data) ? usersRes.data : []);
                updateUsersStats(users);
                renderRecentUsers(users);
            }

            if (invitationsRes && invitationsRes.success && statsRes && statsRes.success) {
                const invitations = invitationsRes.data || [];
                const stats = statsRes.data || {};
                
                // On privilégie les stats agrégées du backend pour le compteur global
                document.getElementById('stat-invitations').textContent = stats.invitations ? stats.invitations.total : invitations.length;
                
                renderHealthStatus(stats.health || {});
                renderRegistrationsChart(stats.registrations || []);
                renderInvitationsChart(stats.invitations || {});
            }
        }).catch(err => {
            console.error('Erreur chargement dashboard:', err);
        });
    }

    function updateUsersStats(users) {
        document.getElementById('stat-users').textContent = users.length;
        document.getElementById('stat-active').textContent = users.filter((u) => u.is_active && !u.is_banned).length;
        document.getElementById('stat-banned').textContent = users.filter((u) => u.is_banned).length;
    }

    function renderRecentUsers(users) {
        const tbody = document.getElementById('recent-users-body');
        if (!tbody) return;

        tbody.innerHTML = '';
        const recent = users.slice(0, 5);
        
        if (recent.length === 0) {
            tbody.innerHTML = `<tr><td colspan="4" class="text-center text-slate-500 py-12">${JG.esc(i18n.noUsers || 'No users')}</td></tr>`;
            return;
        }

        recent.forEach((user) => {
            const status = user.is_banned
                ? `<span class="badge badge-danger">${JG.esc(i18n.statusBanned || 'Banned')}</span>`
                : user.is_active
                    ? `<span class="badge badge-success">${JG.esc(i18n.statusActive || 'Active')}</span>`
                    : `<span class="badge badge-warning">${JG.esc(i18n.statusInactive || 'Inactive')}</span>`;

            tbody.innerHTML += `<tr>
                <td class="px-6 py-4 font-medium text-jg-text">${JG.esc(user.username)}</td>
                <td class="px-6 py-4">${status}</td>
                <td class="px-6 py-4 text-jg-text-muted">${JG.esc(user.invited_by || '—')}</td>
                <td class="px-6 py-4 text-jg-text-muted text-xs">${JG.esc(user.created_at || '—')}</td>
            </tr>`;
        });
    }

    function renderHealthStatus(health) {
        const updateLED = (id, status) => {
            const el = document.getElementById(id);
            if (!el) return;
            el.className = 'w-2.5 h-2.5 rounded-full transition-all duration-700';
            if (status === true) {
                el.classList.add('bg-emerald-500', 'shadow-[0_0_10px_rgba(16,185,129,0.6)]');
            } else if (status === false) {
                el.classList.add('bg-rose-500', 'shadow-[0_0_10px_rgba(244,63,94,0.6)]');
            } else {
                el.classList.add('bg-slate-500', 'opacity-30');
            }
        };

        updateLED('health-db', health.database);
        updateLED('health-jellyfin', health.jellyfin);
        updateLED('health-ldap', health.ldap);
    }

    function renderRegistrationsChart(data) {
        const ctx = document.getElementById('registrationsChart');
        if (!ctx) return;

        // Préparation des données (remplissage des jours manquants sur les 30 derniers jours)
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
                    label: i18n.chartRegistrationsLabel || 'Inscriptions',
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