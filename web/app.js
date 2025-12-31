const state = {
  tasks: [],
  polling: null,
  isAuthenticated: false,
};

// Token management
const getAuthToken = () => {
  return sessionStorage.getItem('clicron_token');
};

const setAuthToken = (token) => {
  sessionStorage.setItem('clicron_token', token);
};

const clearAuthToken = () => {
  sessionStorage.removeItem('clicron_token');
};

// Wrapper for fetch that adds authentication
async function apiFetch(url, options = {}) {
  const token = getAuthToken();
  const headers = options.headers || {};

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  return fetch(url, { ...options, headers });
}

// Authentication
async function checkAuth() {
  const token = getAuthToken();
  if (!token) {
    showAuthModal();
    return false;
  }

  try {
    const resp = await apiFetch('/v1/tasks');
    if (resp.status === 401) {
      clearAuthToken();
      showAuthModal();
      return false;
    }
    state.isAuthenticated = true;
    hideAuthModal();
    initializeApp();  // Initialize on successful auth
    return true;
  } catch (err) {
    console.error(err);
    showAuthModal();
    return false;
  }
}

function showAuthModal() {
  const authModal = document.getElementById('auth-modal');
  const backdrop = document.getElementById('modal-backdrop');
  backdrop.classList.remove('hidden');
  authModal.classList.remove('hidden');
}

function hideAuthModal() {
  const authModal = document.getElementById('auth-modal');
  const backdrop = document.getElementById('modal-backdrop');
  authModal.classList.add('hidden');
  if (!taskModal.classList.contains('hidden') || !logModal.classList.contains('hidden')) {
    return;
  }
  backdrop.classList.add('hidden');
}

async function handleLogin(token) {
  setAuthToken(token);
  const success = await checkAuth();  // checkAuth will call initializeApp
  if (!success) {
    const errorDiv = document.getElementById('auth-error');
    errorDiv.textContent = 'Invalid token';
    errorDiv.classList.remove('hidden');
  }
}

function initializeApp() {
  // Clear existing polling to avoid duplicates
  if (state.polling) {
    clearInterval(state.polling);
  }
  loadTasks();
  state.polling = setInterval(loadTasks, 5000);
}

const backdrop = document.getElementById('modal-backdrop');
const taskModal = document.getElementById('task-modal');
const logModal = document.getElementById('log-modal');

document.getElementById('refresh-btn').addEventListener('click', () => loadTasks());
document.getElementById('new-task-btn').addEventListener('click', () => openTaskForm());

backdrop.addEventListener('click', () => {
  closeModals();
  if (state.isAuthenticated) {
    hideAuthModal();
  }
});

// Auth form handler
document.getElementById('auth-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const tokenInput = document.getElementById('auth-token-input');
  const errorDiv = document.getElementById('auth-error');
  errorDiv.classList.add('hidden');
  await handleLogin(tokenInput.value.trim());
});

// Check auth on load
checkAuth();

async function loadTasks() {
  try {
    const resp = await apiFetch('/v1/tasks');
    if (!resp.ok) throw new Error('Unable to load tasks');
    state.tasks = await resp.json();
    renderTasks();
  } catch (err) {
    console.error(err);
  }
}

function renderTasks() {
  const tbody = document.querySelector('#tasks-table tbody');
  tbody.innerHTML = '';
  state.tasks.forEach((task) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${escapeHtml(task.name || '')}</td>
      <td><code>${escapeHtml(task.command)}</code></td>
      <td>${escapeHtml(task.cron)}</td>
      <td>${task.working_dir ? `<code>${escapeHtml(task.working_dir)}</code>` : ''}</td>
      <td>${renderStatus(task.status)}</td>
      <td>${formatDate(task.last_run_at)}</td>
      <td>${formatDate(task.next_run_at)}</td>
      <td class="actions"></td>
    `;
    const actions = tr.querySelector('.actions');
    actions.appendChild(actionButton('Run', () => runTask(task.id)));
    actions.appendChild(actionButton(task.status === 'paused' ? 'Resume' : 'Pause', () => toggleTask(task)));
    actions.appendChild(actionButton('Edit', () => openTaskForm(task), 'secondary'));
    actions.appendChild(actionButton('Runs', () => openRunsModal(task), 'secondary'));
    actions.appendChild(actionButton('Delete', () => deleteTask(task.id), 'danger'));
    tbody.appendChild(tr);
  });
}

function renderStatus(status) {
  return `<span class="status-pill status-${status}">${escapeHtml(status)}</span>`;
}

function actionButton(label, handler, style = '') {
  const btn = document.createElement('button');
  btn.textContent = label;
  if (style) btn.classList.add(style);
  btn.addEventListener('click', handler);
  return btn;
}

function openTaskForm(task = null) {
  const isEdit = !!task;
  taskModal.innerHTML = '';
  const form = document.createElement('form');
  form.innerHTML = `
    <h2>${isEdit ? 'Edit Task' : 'New Task'}</h2>
    <label>Name (optional)</label>
    <input type="text" name="name" value="${escapeAttribute(task?.name || '')}">
    <label>Command</label>
    <textarea name="command" required>${escapeHtml(task?.command || '')}</textarea>
    <label>Cron (min hour dom mon dow)</label>
    <input type="text" name="cron" value="${escapeAttribute(task?.cron || '')}" required>
    <label>Timeout (seconds, 0 = no timeout)</label>
    <input type="number" name="timeout_s" min="0" value="${task?.timeout_s ?? 0}">
    <label>Working Directory (optional)</label>
    <input type="text" name="working_dir" placeholder="Defaults to server's current working directory" value="${escapeAttribute(task?.working_dir || '')}">
    <label><input type="checkbox" name="paused" ${task?.status === 'paused' ? 'checked' : ''}> Paused</label>
    <div class="cron-preview"></div>
    <div class="form-actions">
      <button type="button" class="secondary">Cancel</button>
      <button type="submit">${isEdit ? 'Save' : 'Create'}</button>
    </div>
  `;

  const cronInput = form.querySelector('input[name="cron"]');
  const previewBox = form.querySelector('.cron-preview');

  const updatePreview = debounce(async () => {
    const expr = cronInput.value.trim();
    if (!expr) {
      previewBox.textContent = '';
      return;
    }
    try {
      const resp = await apiFetch('/v1/cron/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ expr }),
      });
      const data = await resp.json();
      if (!data.valid) {
        previewBox.textContent = data.message || 'Invalid cron expression';
        previewBox.classList.add('error');
      } else {
        previewBox.classList.remove('error');
        previewBox.innerHTML = `Next runs:<br>${data.next_times.map((t) => `<code>${t}</code>`).join('<br>')}`;
      }
    } catch (err) {
      previewBox.textContent = 'Preview error';
    }
  }, 400);

  cronInput.addEventListener('input', updatePreview);
  updatePreview();

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    const formData = new FormData(form);
    const payload = {
      name: formData.get('name') || undefined,
      command: formData.get('command')?.toString() || '',
      cron: formData.get('cron')?.toString() || '',
      timeout_s: Number(formData.get('timeout_s') || 0),
      working_dir: formData.get('working_dir') ? formData.get('working_dir').toString() : undefined,
      paused: formData.get('paused') !== null,
    };
    if (!payload.command.trim() || !payload.cron.trim()) {
      alert('Command and cron are required');
      return;
    }
    try {
      const url = isEdit ? `/v1/tasks/${task.id}` : '/v1/tasks';
      const method = isEdit ? 'PATCH' : 'POST';
      const resp = await apiFetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}));
        throw new Error(err?.error?.message || 'Request failed');
      }
      closeModals();
      await loadTasks();
    } catch (err) {
      alert(err.message);
    }
  });

  form.querySelector('button.secondary').addEventListener('click', closeModals);

  taskModal.appendChild(form);
  showModal(taskModal);
}

async function runTask(taskID) {
  try {
    const resp = await apiFetch(`/v1/tasks/${taskID}/run`, { method: 'POST' });
    if (resp.status === 409) {
      alert('Task is already running');
      return;
    }
    if (!resp.ok) throw new Error('Failed to start task');
    await loadTasks();
  } catch (err) {
    alert(err.message);
  }
}

async function toggleTask(task) {
  try {
    const payload = { paused: task.status !== 'paused' };
    const resp = await apiFetch(`/v1/tasks/${task.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!resp.ok) throw new Error('Failed to update task');
    await loadTasks();
  } catch (err) {
    alert(err.message);
  }
}

async function deleteTask(taskID) {
  if (!confirm('Delete this task? This keeps run history.')) return;
  try {
    const resp = await apiFetch(`/v1/tasks/${taskID}`, { method: 'DELETE' });
    if (!resp.ok) throw new Error('Failed to delete task');
    await loadTasks();
  } catch (err) {
    alert(err.message);
  }
}

async function openRunsModal(task) {
  try {
    const resp = await apiFetch(`/v1/tasks/${task.id}/runs?limit=20`);
    if (!resp.ok) throw new Error('Failed to load runs');
    const runs = await resp.json();

    taskModal.innerHTML = '';
    const container = document.createElement('div');
    const workDirText = task.working_dir ? `<code>${escapeHtml(task.working_dir)}</code>` : '(server cwd)';
    container.innerHTML = `<h2>Runs for ${escapeHtml(task.name || task.command)}</h2><div class="task-meta">Work Dir: ${workDirText}</div>`;
    const table = document.createElement('table');
    table.innerHTML = `
      <thead>
        <tr><th>Status</th><th>Scheduled</th><th>Started</th><th>Ended</th><th>Exit</th><th></th></tr>
      </thead>
      <tbody></tbody>
    `;
    const tbody = table.querySelector('tbody');
    runs.forEach((run) => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>${renderStatus(run.status)}</td>
        <td>${formatDate(run.scheduled_at)}</td>
        <td>${formatDate(run.started_at)}</td>
        <td>${formatDate(run.ended_at)}</td>
        <td>${run.exit_code ?? ''}</td>
        <td></td>
      `;
      const cell = tr.querySelector('td:last-child');
      const viewBtn = actionButton('Log', () => openLogViewer(run.id), 'secondary');
      cell.appendChild(viewBtn);
      tbody.appendChild(tr);
    });
    container.appendChild(table);

    const closeBtn = document.createElement('button');
    closeBtn.textContent = 'Close';
    closeBtn.classList.add('secondary');
    closeBtn.addEventListener('click', closeModals);
    container.appendChild(closeBtn);

    taskModal.appendChild(container);
    showModal(taskModal);
  } catch (err) {
    alert(err.message);
  }
}

async function openLogViewer(runID) {
  try {
    const resp = await apiFetch(`/v1/runs/${runID}/log?tail=200`);
    if (!resp.ok) throw new Error('Failed to load log');
    const text = await resp.text();
    logModal.innerHTML = '';
    const wrapper = document.createElement('div');
    wrapper.innerHTML = `
      <h2>Run Log</h2>
      <pre class="log-viewer">${escapeHtml(text)}</pre>
      <div class="form-actions">
        <button class="secondary">Close</button>
      </div>
    `;
    wrapper.querySelector('button').addEventListener('click', closeModals);
    logModal.appendChild(wrapper);
    showModal(logModal);
  } catch (err) {
    alert(err.message);
  }
}

function showModal(modal) {
  backdrop.classList.remove('hidden');
  modal.classList.remove('hidden');
}

function closeModals() {
  backdrop.classList.add('hidden');
  taskModal.classList.add('hidden');
  logModal.classList.add('hidden');
}

function formatDate(value) {
  if (!value) return '';
  const date = new Date(value);
  return isNaN(date.getTime()) ? value : date.toLocaleString();
}

function escapeHtml(value) {
  const str = value == null ? '' : String(value);
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function escapeAttribute(value) {
  const str = value == null ? '' : String(value);
  return escapeHtml(str).replace(/`/g, '&#96;');
}

function debounce(fn, delay) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn.apply(null, args), delay);
  };
}
