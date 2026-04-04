(() => {
    const config = window.JGPageDashboard || {};
    const i18n = config.i18n || {};

    document.addEventListener('DOMContentLoaded', () => {
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

        if (!config.isAdmin) {
            return;
        }

        Promise.all([
            JG.api('/admin/api/users'),
            JG.api('/admin/api/invitations'),
        ]).then(([usersRes, invitationsRes]) => {
            if (!usersRes || !usersRes.success) {
                return;
            }

            const users = (usersRes.data && usersRes.data.users) ? usersRes.data.users : (Array.isArray(usersRes.data) ? usersRes.data : []);
            const invitations = invitationsRes && invitationsRes.success ? (invitationsRes.data || []) : [];
            document.getElementById('stat-users').textContent = users.length;
            document.getElementById('stat-active').textContent = users.filter((user) => user.is_active && !user.is_banned).length;
            document.getElementById('stat-banned').textContent = users.filter((user) => user.is_banned).length;
            document.getElementById('stat-invitations').textContent = invitations.length;

            const tbody = document.getElementById('recent-users-body');
            if (!tbody) {
                return;
            }

            tbody.innerHTML = '';
            users.slice(0, 5).forEach((user) => {
                const status = user.is_banned
                    ? `<span class="badge badge-danger">${JG.esc(i18n.statusBanned || 'Banned')}</span>`
                    : user.is_active
                        ? `<span class="badge badge-success">${JG.esc(i18n.statusActive || 'Active')}</span>`
                        : `<span class="badge badge-warning">${JG.esc(i18n.statusInactive || 'Inactive')}</span>`;

                tbody.innerHTML += `<tr>
        <td class="font-medium">${JG.esc(user.username)}</td>
        <td>${status}</td>
        <td class="text-slate-400">${JG.esc(user.invited_by || '—')}</td>
        <td class="text-slate-500 text-sm">${JG.esc(user.created_at || '—')}</td>
      </tr>`;
            });

            if (users.length === 0) {
                tbody.innerHTML = `<tr><td colspan="4" class="text-center text-slate-500 py-8">${JG.esc(i18n.noUsers || 'No users')}</td></tr>`;
            }
        });
    });
})();