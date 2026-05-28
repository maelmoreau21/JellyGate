(() => {
    const config = window.JGPageScheduler || {};
    const i18n = config.i18n || {};
    const taskTypeDescriptions = config.taskTypeDescriptions || {};
    let tasks = [];

    const t = (key, fallback) => i18n[key] || fallback || key;

    function confirmAction(title, message, options) {
        if (typeof JG.confirm === 'function') {
            return JG.confirm(title, message, options);
        }
        return Promise.resolve(false);
    }

    function showQuickTaskStatus(message, type = 'info') {
        const box = document.getElementById('quick-task-status');
        if (!box) return;
        box.classList.remove('hidden', 'border-sky-500/30', 'bg-sky-500/10', 'text-sky-200', 'border-emerald-500/30', 'bg-emerald-500/10', 'text-emerald-200', 'border-rose-500/30', 'bg-rose-500/10', 'text-rose-200');
        if (type === 'success') {
            box.classList.add('border-emerald-500/30', 'bg-emerald-500/10', 'text-emerald-200');
        } else if (type === 'error') {
            box.classList.add('border-rose-500/30', 'bg-rose-500/10', 'text-rose-200');
        } else {
            box.classList.add('border-sky-500/30', 'bg-sky-500/10', 'text-sky-200');
        }
        box.textContent = message;
    }

    function taskMessage(template, label, taskName) {
        return String(template || '')
            .split('{label}').join(label)
            .split('{task}').join(taskName || label);
    }

    function findTaskByType(taskType) {
        const normalized = String(taskType || '').trim().toLowerCase();
        if (!normalized) return null;
        return tasks.find((task) => String(task.task_type || '').trim().toLowerCase() === normalized && !!task.enabled)
            || tasks.find((task) => String(task.task_type || '').trim().toLowerCase() === normalized)
            || null;
    }

    async function runQuickTask(taskType, label) {
        let target = findTaskByType(taskType);
        if (!target) {
            await loadTasks();
            target = findTaskByType(taskType);
        }
        if (!target) {
            showQuickTaskStatus(taskMessage(t('quickTaskMissing', '{label}: no scheduled task exists.'), label), 'error');
            return;
        }

        showQuickTaskStatus(taskMessage(t('quickTaskRunning', '{label}: running {task}...'), label, String(target.name || target.id)), 'info');
        const res = await JG.api(`/admin/api/automation/tasks/${target.id}/run`, { method: 'POST' });
        if (!res || !res.success) {
            showQuickTaskStatus((res && res.message) || taskMessage(t('quickTaskFailed', '{label}: run failed.'), label, String(target.name || target.id)), 'error');
            return;
        }
        showQuickTaskStatus(taskMessage(t('quickTaskSuccess', '{label}: done.'), label, String(target.name || target.id)), 'success');
        await loadTasks();
    }

    function updateTaskPreview() {
        const name = (document.getElementById('task-name')?.value || '').trim();
        const type = document.getElementById('task-type')?.value || '';
        const hour = document.getElementById('task-hour')?.value;
        const minute = document.getElementById('task-minute')?.value;
        const enabled = !!document.getElementById('task-enabled')?.checked;

        const previewName = document.getElementById('automation-task-preview-name');
        const previewType = document.getElementById('automation-task-preview-type');
        const previewSchedule = document.getElementById('automation-task-preview-schedule');
        const previewState = document.getElementById('automation-task-preview-state');
        const previewNote = document.getElementById('automation-task-preview-note');

        if (previewName) previewName.textContent = name || t('taskPreviewEmpty', 'No task selected');
        if (previewType) previewType.textContent = type || '-';
        if (previewSchedule) previewSchedule.textContent = hour === '' || minute === '' ? '--:--' : `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
        if (previewState) previewState.textContent = enabled ? t('taskEnabled', 'Enabled') : t('taskDisabled', 'Disabled');
        if (previewNote) previewNote.textContent = taskTypeDescriptions[type] || t('taskPreviewEmpty', 'No task selected');
    }

    function renderTasks() {
        const tbody = document.getElementById('tasks-body');
        if (!tbody) return;
        if (!tasks.length) {
            tbody.innerHTML = `<tr><td colspan="6" class="text-center text-slate-500 py-8">${JG.esc(t('noTasks', 'No scheduled tasks.'))}</td></tr>`;
            return;
        }
        const taskTypeLabels = {
            sync_users: t('taskTypeSyncUsers', 'Sync users'),
            sync_ldap_users: t('taskTypeSyncLdapUsers', 'Sync LDAP users'),
            cleanup_resets: t('taskTypeCleanupResets', 'Clean reset links'),
        };
        const typeCell = (task) => {
            const taskType = String(task.task_type || '').trim();
            const label = taskTypeLabels[taskType] || taskType || '-';
            return `<div class="flex flex-wrap items-center gap-1.5"><span class="text-xs font-semibold text-jg-text">${JG.esc(label)}</span></div><code class="mt-1 block text-[10px] text-jg-text-muted/70">${JG.esc(taskType)}</code>`;
        };
        tbody.innerHTML = tasks.map((task) => `<tr class="hover:bg-white/[0.02] transition-colors border-b border-jg-border last:border-none">
            <td class="px-6 py-4 font-bold text-jg-text">${JG.esc(task.name || '')}</td>
            <td class="px-6 py-4">${typeCell(task)}</td>
            <td class="px-6 py-4 text-jg-text font-medium">${String(task.hour).padStart(2, '0')}:${String(task.minute).padStart(2, '0')} ${task.enabled ? `<span class="bg-emerald-500/10 text-emerald-500 text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider ml-2">${JG.esc(t('statusOn', 'On'))}</span>` : `<span class="bg-white/5 text-jg-text-muted text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider ml-2">${JG.esc(t('statusOff', 'Off'))}</span>`}</td>
            <td class="px-6 py-4"><code class="text-xs text-jg-text-muted opacity-60">${JG.esc(task.payload || '-')}</code></td>
            <td class="px-6 py-4 text-sm text-jg-text-muted">${JG.esc(task.last_run_at || '-')}</td>
            <td class="px-6 py-4 text-right">
                <div class="flex justify-end gap-2">
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-run" data-id="${task.id}">${JG.esc(t('runNow', 'Run now'))}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-edit" data-id="${task.id}">${JG.esc(t('editLabel', 'Edit'))}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-ghost hover:bg-white/10" data-action="task-toggle" data-id="${task.id}">${task.enabled ? JG.esc(t('disable', 'Disable')) : JG.esc(t('enable', 'Enable'))}</button>
                    <button class="jg-btn jg-btn-sm jg-btn-danger/80 hover:bg-jg-danger transition-colors" data-action="task-delete" data-id="${task.id}">${JG.esc(t('deleteLabel', 'Delete'))}</button>
                </div>
            </td>
        </tr>`).join('');
    }

    async function loadTasks() {
        const tbody = document.getElementById('tasks-body');
        if (!tbody) return;
        const res = await JG.api('/admin/api/automation/tasks');
        if (!res || !res.success) {
            JG.toast((res && res.message) || t('errorTasks', 'Unable to load scheduled tasks.'), 'error');
            return;
        }
        tasks = Array.isArray(res.data) ? res.data : [];
        renderTasks();
    }

    function resetTaskForm() {
        const form = document.getElementById('task-create-form');
        if (form) form.reset();
        const id = document.getElementById('task-id');
        const enabled = document.getElementById('task-enabled');
        if (id) id.value = '';
        if (enabled) enabled.checked = true;
        updateTaskPreview();
    }

    document.addEventListener('DOMContentLoaded', () => {
        if (!document.getElementById('tasks-body')) return;

        document.addEventListener('click', (event) => {
            const closeTarget = event.target.closest('.modal-close-btn, .modal-backdrop');
            if (closeTarget) {
                const modalId = closeTarget.getAttribute('data-modal');
                if (modalId) JG.closeModal(modalId);
            }
        });

        document.getElementById('btn-open-task-modal')?.addEventListener('click', () => {
            resetTaskForm();
            JG.openModal('modal-task-form');
        });

        document.getElementById('task-create-form')?.addEventListener('submit', async (event) => {
            event.preventDefault();
            const id = document.getElementById('task-id').value;
            const payload = {
                name: document.getElementById('task-name').value.trim(),
                task_type: document.getElementById('task-type').value,
                enabled: document.getElementById('task-enabled').checked,
                hour: parseInt(document.getElementById('task-hour').value || '0', 10),
                minute: parseInt(document.getElementById('task-minute').value || '0', 10),
                payload: document.getElementById('task-payload').value.trim(),
            };

            const res = await JG.api(id ? `/admin/api/automation/tasks/${id}` : '/admin/api/automation/tasks', {
                method: id ? 'PATCH' : 'POST',
                body: JSON.stringify(payload),
            });
            if (!res || !res.success) {
                JG.toast((res && res.message) || (id ? t('taskUpdateFailed', 'Unable to update task.') : t('taskCreateFailed', 'Unable to create task.')), 'error');
                return;
            }
            JG.toast(id ? t('taskUpdated', 'Task updated.') : t('taskCreated', 'Task created.'), 'success');
            resetTaskForm();
            JG.closeModal('modal-task-form');
            await loadTasks();
        });

        document.getElementById('tasks-body')?.addEventListener('click', async (event) => {
            const button = event.target.closest('button');
            if (!button) return;
            const id = button.dataset.id;
            const action = button.dataset.action;
            const task = tasks.find((entry) => String(entry.id) === String(id));

            if (action === 'task-delete') {
                const agreed = await confirmAction(t('deleteLabel', 'Delete'), t('taskDeleteConfirm', 'Delete this task?'), { danger: true });
                if (!agreed) return;
                const res = await JG.api(`/admin/api/automation/tasks/${id}`, { method: 'DELETE' });
                if (!res || !res.success) {
                    JG.toast((res && res.message) || t('taskDeleteFailed', 'Unable to delete task.'), 'error');
                    return;
                }
                JG.toast(t('taskDeleted', 'Task deleted.'), 'success');
                await loadTasks();
                return;
            }

            if (action === 'task-run') {
                const res = await JG.api(`/admin/api/automation/tasks/${id}/run`, { method: 'POST' });
                if (!res || !res.success) {
                    JG.toast((res && res.message) || t('taskRunFailed', 'Task run failed.'), 'error');
                    return;
                }
                JG.toast(t('taskRunSuccess', 'Task run started.'), 'success');
                await loadTasks();
                return;
            }

            if (action === 'task-edit' && task) {
                document.getElementById('task-id').value = task.id;
                document.getElementById('task-name').value = task.name || '';
                document.getElementById('task-type').value = task.task_type || 'sync_users';
                document.getElementById('task-hour').value = task.hour;
                document.getElementById('task-minute').value = task.minute;
                document.getElementById('task-payload').value = task.payload || '';
                document.getElementById('task-enabled').checked = !!task.enabled;
                updateTaskPreview();
                JG.openModal('modal-task-form');
                return;
            }

            if (action === 'task-toggle' && task) {
                const res = await JG.api(`/admin/api/automation/tasks/${id}`, {
                    method: 'PATCH',
                    body: JSON.stringify({
                        name: task.name,
                        task_type: task.task_type,
                        enabled: !task.enabled,
                        hour: task.hour,
                        minute: task.minute,
                        payload: task.payload || '',
                    }),
                });
                if (!res || !res.success) {
                    JG.toast((res && res.message) || t('taskUpdateFailed', 'Unable to update task.'), 'error');
                    return;
                }
                await loadTasks();
            }
        });

        document.getElementById('btn-task-quick-sync-users')?.addEventListener('click', () => runQuickTask('sync_users', t('manualSyncUsers', 'Sync users')));
        document.getElementById('btn-task-quick-sync-ldap')?.addEventListener('click', () => runQuickTask('sync_ldap_users', t('manualSyncLdap', 'Sync LDAP users')));
        document.getElementById('btn-task-quick-cleanup')?.addEventListener('click', () => runQuickTask('cleanup_resets', t('manualCleanupResets', 'Clean reset links')));

        ['task-name', 'task-type', 'task-hour', 'task-minute', 'task-payload', 'task-enabled'].forEach((id) => {
            const element = document.getElementById(id);
            if (!element) return;
            element.addEventListener('input', updateTaskPreview);
            element.addEventListener('change', updateTaskPreview);
        });

        updateTaskPreview();
        loadTasks();
    });
})();
