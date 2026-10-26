// sCodeViewer - Ultra-fast minimalist client
(() => {
  'use strict';

  // State
  const state = {
    projects: [],
    activeProject: null,
    branches: [],
    activeBranch: 'HEAD',
    commits: [],
    selectedCommitHash: null,
    selectedCommitDetail: null,
    commitFilter: 'all', // 'all', 'merges', 'ai', 'human'
    searchQuery: '',
    diffViewMode: 'unified', // 'unified' or 'split'
    theme: 'system', // 'system', 'dark', 'light'

    // Updates tab state
    updatesPeriod: '7d',
    updatesFilterAI: 'all',
    updatesData: null,
    selectedAuthor: null
  };

  // DOM Elements
  const el = {
    // Header
    projectSelect: document.getElementById('projectSelect'),
    addProjectBtn: document.getElementById('addProjectBtn'),
    branchSelect: document.getElementById('branchSelect'),
    themeToggleBtn: document.getElementById('themeToggleBtn'),
    themeIcon: document.getElementById('themeIcon'),
    themeLabel: document.getElementById('themeLabel'),
    navTabs: document.querySelectorAll('.nav-tabs .tab-btn'),
    reviewPane: document.getElementById('reviewPane'),
    updatesPane: document.getElementById('updatesPane'),

    // Commits Sidebar
    commitSearchInput: document.getElementById('commitSearchInput'),
    commitsList: document.getElementById('commitsList'),
    chipBtns: document.querySelectorAll('.filter-chips .chip-btn'),

    // Commit Detail & Diff
    commitDetailHeader: document.getElementById('commitDetailHeader'),
    detailTitle: document.getElementById('detailTitle'),
    detailBadges: document.getElementById('detailBadges'),
    detailAvatar: document.getElementById('detailAvatar'),
    detailAuthor: document.getElementById('detailAuthor'),
    detailDate: document.getElementById('detailDate'),
    detailHash: document.getElementById('detailHash'),
    detailParents: document.getElementById('detailParents'),
    detailMessageBody: document.getElementById('detailMessageBody'),
    diffToolbar: document.getElementById('diffToolbar'),
    diffTotalFiles: document.getElementById('diffTotalFiles'),
    diffTotalAdds: document.getElementById('diffTotalAdds'),
    diffTotalDels: document.getElementById('diffTotalDels'),
    diffFilesContainer: document.getElementById('diffFilesContainer'),
    diffEmptyState: document.getElementById('diffEmptyState'),
    modeUnifiedBtn: document.getElementById('modeUnifiedBtn'),
    modeSplitBtn: document.getElementById('modeSplitBtn'),

    // Updates Tab
    sliceBtns: document.querySelectorAll('.time-slices-bar .slice-btn'),
    customDateRange: document.getElementById('customDateRange'),
    dateSince: document.getElementById('dateSince'),
    dateUntil: document.getElementById('dateUntil'),
    applyCustomDateBtn: document.getElementById('applyCustomDateBtn'),
    metricTotalCommits: document.getElementById('metricTotalCommits'),
    metricMerges: document.getElementById('metricMerges'),
    metricAICommits: document.getElementById('metricAICommits'),
    metricHumanCommits: document.getElementById('metricHumanCommits'),
    metricAIRatio: document.getElementById('metricAIRatio'),
    metricAdditions: document.getElementById('metricAdditions'),
    metricDeletions: document.getElementById('metricDeletions'),
    metricNetLines: document.getElementById('metricNetLines'),
    metricPeriodLabel: document.getElementById('metricPeriodLabel'),
    metricBranch: document.getElementById('metricBranch'),
    authorsList: document.getElementById('authorsList'),
    timelineFeed: document.getElementById('timelineFeed'),
    feedCount: document.getElementById('feedCount'),
    updatesFilterAll: document.getElementById('updatesFilterAll'),
    updatesFilterAI: document.getElementById('updatesFilterAI'),
    updatesFilterHuman: document.getElementById('updatesFilterHuman'),

    // Modal
    addProjectModal: document.getElementById('addProjectModal'),
    projectPathInput: document.getElementById('projectPathInput'),
    recentProjectsContainer: document.getElementById('recentProjectsContainer'),
    confirmAddProjectBtn: document.getElementById('confirmAddProjectBtn'),
    cancelModalBtn: document.getElementById('cancelModalBtn'),
    closeModalBtn: document.getElementById('closeModalBtn')
  };

  // Utility functions
  function getInitials(name) {
    if (!name) return '?';
    const parts = name.trim().split(/\s+/);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }

  function getAvatarColor(name) {
    let hash = 0;
    for (let i = 0; i < (name || '').length; i++) {
      hash = name.charCodeAt(i) + ((hash << 5) - hash);
    }
    const colors = ['#0969da', '#8250df', '#1a7f37', '#cf222e', '#bf8700', '#2ea043', '#0550ae'];
    return colors[Math.abs(hash) % colors.length];
  }

  function formatDate(isoStr) {
    if (!isoStr) return '';
    const date = new Date(isoStr);
    const now = new Date();
    const diffMs = now - date;
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHour = Math.floor(diffMin / 60);
    const diffDay = Math.floor(diffHour / 24);

    if (diffDay === 0) {
      if (diffHour === 0) {
        if (diffMin <= 1) return 'только что';
        return `${diffMin} мин. назад`;
      }
      return `${diffHour} ч. назад`;
    }
    if (diffDay === 1) return 'вчера';
    if (diffDay < 7) return `${diffDay} дн. назад`;

    return date.toLocaleDateString('ru-RU', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  }

  function escapeHtml(str) {
    if (!str) return '';
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  // Theme Management
  function applyTheme(theme) {
    state.theme = theme;
    const isDarkOS = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
    let effective = theme;

    if (theme === 'system') {
      effective = isDarkOS ? 'dark' : 'light';
      el.themeIcon.textContent = '🌓';
      el.themeLabel.textContent = 'Авто';
    } else if (theme === 'dark') {
      el.themeIcon.textContent = '🌙';
      el.themeLabel.textContent = 'Тёмная';
    } else {
      el.themeIcon.textContent = '☀️';
      el.themeLabel.textContent = 'Светлая';
    }

    if (effective === 'dark') {
      document.documentElement.setAttribute('data-theme', 'dark');
    } else {
      document.documentElement.removeAttribute('data-theme');
    }

    localStorage.setItem('scodeviewer_theme', theme);
  }

  function cycleTheme() {
    const next = state.theme === 'system' ? 'dark' : state.theme === 'dark' ? 'light' : 'system';
    applyTheme(next);
    fetch('/api/theme', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: next })
    }).catch(() => {});
  }

  // System theme change listener
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (state.theme === 'system') {
        applyTheme('system');
      }
    });
  }

  // API calls
  async function fetchProjects() {
    try {
      const res = await fetch('/api/projects');
      const data = await res.json();
      state.projects = data.projects || [];
      state.activeProject = data.activePath || null;
      renderProjectSelect();
      if (data.themePreference) {
        applyTheme(data.themePreference);
      }
      if (state.activeProject) {
        await loadBranches();
        await loadCommits();
      } else {
        showEmptyProjectState();
      }
    } catch (err) {
      console.error('Failed to fetch projects:', err);
    }
  }

  function renderProjectSelect() {
    el.projectSelect.innerHTML = '';
    if (state.projects.length === 0) {
      const opt = document.createElement('option');
      opt.value = '';
      opt.textContent = '(Нет проектов)';
      el.projectSelect.appendChild(opt);
      return;
    }

    state.projects.forEach(p => {
      const opt = document.createElement('option');
      opt.value = p.path;
      opt.textContent = p.name;
      if (p.path === state.activeProject) {
        opt.selected = true;
      }
      el.projectSelect.appendChild(opt);
    });
  }

  async function selectProject(path) {
    if (!path) return;
    try {
      const res = await fetch('/api/projects/select', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: path })
      });
      if (res.ok) {
        state.activeProject = path;
        renderProjectSelect();
        await loadBranches();
        await loadCommits();
        if (!el.updatesPane.classList.contains('hidden')) {
          loadUpdates();
        }
      }
    } catch (err) {
      console.error('Failed to select project:', err);
    }
  }

  async function addProject(path) {
    if (!path || !path.trim()) return;
    try {
      const res = await fetch('/api/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: path.trim() })
      });
      const data = await res.json();
      if (res.ok) {
        closeModal();
        await fetchProjects();
      } else {
        alert(data.error || 'Ошибка при добавлении проекта');
      }
    } catch (err) {
      alert('Ошибка подключения к серверу');
    }
  }

  async function removeProject(id) {
    if (!confirm('Удалить проект из списка?')) return;
    try {
      await fetch(`/api/projects?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
      await fetchProjects();
    } catch (err) {
      console.error(err);
    }
  }

  async function loadBranches() {
    try {
      const res = await fetch('/api/branches');
      state.branches = await res.json();
      el.branchSelect.innerHTML = '';

      let currentFound = false;
      state.branches.forEach(b => {
        const opt = document.createElement('option');
        opt.value = b.name;
        opt.textContent = (b.isCurrent ? '★ ' : '') + b.name;
        if (b.isCurrent) {
          opt.selected = true;
          state.activeBranch = b.name;
          currentFound = true;
        }
        el.branchSelect.appendChild(opt);
      });

      if (!currentFound && state.branches.length > 0) {
        state.activeBranch = state.branches[0].name;
      }
    } catch (err) {
      console.error('Failed to load branches:', err);
    }
  }

  async function loadCommits() {
    el.commitsList.innerHTML = '<div class="empty-state"><p>Загрузка коммитов...</p></div>';
    let url = `/api/commits?branch=${encodeURIComponent(state.activeBranch)}`;

    if (state.commitFilter === 'merges') {
      url += '&merges=true';
    } else if (state.commitFilter === 'ai') {
      url += '&ai=ai_only';
    } else if (state.commitFilter === 'human') {
      url += '&ai=human_only';
    }

    try {
      const res = await fetch(url);
      state.commits = await res.json();
      renderCommitsList();
      if (state.commits.length > 0) {
        // Select first commit automatically
        selectCommit(state.commits[0].hash);
      } else {
        el.diffToolbar.style.display = 'none';
        el.commitDetailHeader.style.display = 'none';
        el.diffFilesContainer.innerHTML = '<div class="empty-state"><p>Нет коммитов для отображения</p></div>';
      }
    } catch (err) {
      el.commitsList.innerHTML = `<div class="empty-state"><p>Ошибка: ${escapeHtml(err.message)}</p></div>`;
    }
  }

  function renderCommitsList() {
    let list = state.commits;
    const q = state.searchQuery.toLowerCase().trim();
    if (q) {
      list = list.filter(c =>
        c.message.toLowerCase().includes(q) ||
        c.authorName.toLowerCase().includes(q) ||
        c.shortHash.toLowerCase().includes(q)
      );
    }

    if (list.length === 0) {
      el.commitsList.innerHTML = '<div class="empty-state"><p>Коммиты не найдены</p></div>';
      return;
    }

    el.commitsList.innerHTML = '';
    list.forEach(c => {
      const item = document.createElement('div');
      item.className = 'commit-item' + (c.hash === state.selectedCommitHash ? ' active' : '');
      item.dataset.hash = c.hash;

      const avatarBg = getAvatarColor(c.authorName);
      const initials = getInitials(c.authorName);

      let badgesHtml = '';
      if (c.isMerge) {
        badgesHtml += '<span class="badge badge-merge">🔀 Merge</span>';
      }
      if (c.isAI) {
        badgesHtml += `<span class="badge badge-ai">🤖 ${escapeHtml(c.aiTag || 'AI')}</span>`;
      } else {
        badgesHtml += '<span class="badge badge-human">👤 Человек</span>';
      }

      item.innerHTML = `
        <div class="commit-item-top">
          <div class="commit-author-group">
            <div class="author-avatar" style="background-color: ${avatarBg}; color: #fff;">${initials}</div>
            <span class="author-name" title="${escapeHtml(c.authorName)} <${escapeHtml(c.authorEmail)}>">${escapeHtml(c.authorName)}</span>
          </div>
          <span class="commit-date">${formatDate(c.date)}</span>
        </div>
        <div class="commit-msg">${escapeHtml(c.message)}</div>
        <div class="commit-item-bottom">
          <div class="commit-badges">${badgesHtml}</div>
          <div style="display: flex; align-items: center; gap: 8px;">
            <div class="diff-stat-summary">
              <span class="stat-add">+${c.additions}</span>
              <span class="stat-del">-${c.deletions}</span>
            </div>
            <span class="badge-hash">${c.shortHash}</span>
          </div>
        </div>
      `;

      item.addEventListener('click', () => selectCommit(c.hash));
      el.commitsList.appendChild(item);
    });
  }

  async function selectCommit(hash) {
    state.selectedCommitHash = hash;

    // Update active highlight in sidebar
    document.querySelectorAll('.commit-item').forEach(it => {
      it.classList.toggle('active', it.dataset.hash === hash);
    });

    el.diffFilesContainer.innerHTML = '<div class="empty-state"><p>Загрузка диффа...</p></div>';

    try {
      const res = await fetch(`/api/commit?hash=${encodeURIComponent(hash)}`);
      if (!res.ok) throw new Error('Не удалось загрузить коммит');
      const detail = await res.json();
      state.selectedCommitDetail = detail;
      renderCommitDetail(detail);
    } catch (err) {
      el.diffFilesContainer.innerHTML = `<div class="empty-state"><p>Ошибка: ${escapeHtml(err.message)}</p></div>`;
    }
  }

  function renderCommitDetail(detail) {
    el.commitDetailHeader.style.display = 'flex';
    el.diffToolbar.style.display = 'flex';

    el.detailTitle.textContent = detail.message;

    // Badges
    let badgesHtml = '';
    if (detail.isMerge) {
      badgesHtml += '<span class="badge badge-merge" style="font-size: 11px;">🔀 Merge Commit</span>';
    }
    if (detail.isAI) {
      badgesHtml += `<span class="badge badge-ai" style="font-size: 11px;">🤖 ${escapeHtml(detail.aiTag || 'AI')}</span>`;
    } else {
      badgesHtml += '<span class="badge badge-human" style="font-size: 11px;">👤 Разработчик</span>';
    }
    el.detailBadges.innerHTML = badgesHtml;

    el.detailAvatar.textContent = getInitials(detail.authorName);
    el.detailAvatar.style.backgroundColor = getAvatarColor(detail.authorName);
    el.detailAvatar.style.color = '#fff';
    el.detailAuthor.textContent = `${detail.authorName} <${detail.authorEmail}>`;
    el.detailDate.textContent = formatDate(detail.date) + ' (' + new Date(detail.date).toLocaleString('ru-RU') + ')';
    el.detailHash.textContent = detail.hash;

    if (detail.parentHashes && detail.parentHashes.length > 0) {
      el.detailParents.innerHTML = detail.parentHashes.map(p =>
        `<span style="cursor: pointer; text-decoration: underline;" title="Открыть родительский коммит" data-parent="${p}">${p.slice(0, 7)}</span>`
      ).join(', ');

      el.detailParents.querySelectorAll('span[data-parent]').forEach(s => {
        s.addEventListener('click', (e) => {
          selectCommit(e.target.dataset.parent);
        });
      });
    } else {
      el.detailParents.textContent = '— (корневой коммит)';
    }

    if (detail.fullMessage && detail.fullMessage !== detail.message) {
      el.detailMessageBody.textContent = detail.fullMessage;
      el.detailMessageBody.style.display = 'block';
    } else {
      el.detailMessageBody.style.display = 'none';
    }

    // Diff Stats
    el.diffTotalFiles.textContent = detail.files ? detail.files.length : 0;
    el.diffTotalAdds.textContent = `+${detail.additions}`;
    el.diffTotalDels.textContent = `-${detail.deletions}`;

    // Render Files Diff
    renderDiffFiles(detail.files || []);
  }

  function renderDiffFiles(files) {
    if (files.length === 0) {
      el.diffFilesContainer.innerHTML = '<div class="empty-state"><p>В этом коммите нет изменений файлов</p></div>';
      return;
    }

    el.diffFilesContainer.innerHTML = '';
    files.forEach(file => {
      const card = document.createElement('div');
      card.className = 'diff-file-card';

      let statusBadge = '';
      if (file.status === 'added') statusBadge = '<span class="file-status-badge status-added">ADDED</span>';
      else if (file.status === 'deleted') statusBadge = '<span class="file-status-badge status-deleted">DELETED</span>';
      else if (file.status === 'renamed') statusBadge = '<span class="file-status-badge status-modified">RENAMED</span>';
      else statusBadge = '<span class="file-status-badge status-modified">MODIFIED</span>';

      const filePath = file.newPath || file.oldPath;

      card.innerHTML = `
        <div class="file-header">
          <div class="file-title">
            <span>📄</span>
            <span>${escapeHtml(filePath)}</span>
            ${statusBadge}
          </div>
          <div class="diff-stat-summary">
            <span class="stat-add">+${file.additions}</span>
            <span class="stat-del">-${file.deletions}</span>
          </div>
        </div>
        <div class="file-body">
          ${renderFileDiffContent(file)}
        </div>
      `;

      // Toggle collapse on header click
      const header = card.querySelector('.file-header');
      const body = card.querySelector('.file-body');
      header.addEventListener('click', () => {
        body.style.display = body.style.display === 'none' ? 'block' : 'none';
      });

      el.diffFilesContainer.appendChild(card);
    });
  }

  function renderFileDiffContent(file) {
    if (file.isBinary) {
      return '<div style="padding: 16px; font-style: italic; color: var(--text-secondary);">Бинарный файл не отображается</div>';
    }

    if (!file.hunks || file.hunks.length === 0) {
      return '<div style="padding: 16px; color: var(--text-secondary);">Нет изменений в содержимом</div>';
    }

    if (state.diffViewMode === 'split') {
      return renderSplitDiffTable(file.hunks);
    }
    return renderUnifiedDiffTable(file.hunks);
  }

  function renderUnifiedDiffTable(hunks) {
    let rows = '';
    hunks.forEach(h => {
      rows += `
        <tr class="diff-line-hunk">
          <td colspan="3">${escapeHtml(h.header)}</td>
        </tr>
      `;

      (h.lines || []).forEach(line => {
        let rowClass = '';
        let sign = ' ';
        let numClassOld = 'diff-num';
        let numClassNew = 'diff-num';

        if (line.type === 'add') {
          rowClass = 'diff-line-add';
          sign = '+';
          numClassNew += ' diff-num-add';
        } else if (line.type === 'del') {
          rowClass = 'diff-line-del';
          sign = '-';
          numClassOld += ' diff-num-del';
        }

        const oldNo = line.oldLineNo ? line.oldLineNo : '';
        const newNo = line.newLineNo ? line.newLineNo : '';

        rows += `
          <tr class="${rowClass}">
            <td class="${numClassOld}">${oldNo}</td>
            <td class="${numClassNew}">${newNo}</td>
            <td class="diff-content"><span class="diff-sign">${sign}</span>${escapeHtml(line.content)}</td>
          </tr>
        `;
      });
    });

    return `<table class="diff-table">${rows}</table>`;
  }

  function renderSplitDiffTable(hunks) {
    let rows = '';

    hunks.forEach(h => {
      rows += `
        <tr class="diff-line-hunk">
          <td colspan="4">${escapeHtml(h.header)}</td>
        </tr>
      `;

      // Group additions and deletions for side-by-side view
      const lines = h.lines || [];
      let i = 0;
      while (i < lines.length) {
        const line = lines[i];

        if (line.type === 'context') {
          rows += `
            <tr>
              <td class="diff-num">${line.oldLineNo}</td>
              <td class="diff-content" style="border-right: 1px solid var(--border-color);">${escapeHtml(line.content)}</td>
              <td class="diff-num">${line.newLineNo}</td>
              <td class="diff-content">${escapeHtml(line.content)}</td>
            </tr>
          `;
          i++;
        } else if (line.type === 'del') {
          let delLine = line;
          let addLine = null;

          // Check if followed by add
          if (i + 1 < lines.length && lines[i + 1].type === 'add') {
            addLine = lines[i + 1];
            i += 2;
          } else {
            i++;
          }

          rows += `
            <tr>
              <td class="diff-num diff-num-del">${delLine.oldLineNo}</td>
              <td class="diff-content diff-line-del" style="border-right: 1px solid var(--border-color);"><span class="diff-sign">-</span>${escapeHtml(delLine.content)}</td>
              <td class="diff-num ${addLine ? 'diff-num-add' : ''}">${addLine ? addLine.newLineNo : ''}</td>
              <td class="diff-content ${addLine ? 'diff-line-add' : ''}">${addLine ? '<span class="diff-sign">+</span>' + escapeHtml(addLine.content) : ''}</td>
            </tr>
          `;
        } else if (line.type === 'add') {
          rows += `
            <tr>
              <td class="diff-num"></td>
              <td class="diff-content" style="border-right: 1px solid var(--border-color);"></td>
              <td class="diff-num diff-num-add">${line.newLineNo}</td>
              <td class="diff-content diff-line-add"><span class="diff-sign">+</span>${escapeHtml(line.content)}</td>
            </tr>
          `;
          i++;
        }
      }
    });

    return `<table class="diff-table split-diff-table">${rows}</table>`;
  }

  // Updates & Activity Tab
  async function loadUpdates() {
    if (!state.activeProject) return;

    let url = `/api/updates?period=${state.updatesPeriod}&branch=${encodeURIComponent(state.activeBranch)}`;
    if (state.updatesFilterAI !== 'all') {
      url += `&ai=${state.updatesFilterAI}`;
    }
    if (state.selectedAuthor) {
      url += `&author=${encodeURIComponent(state.selectedAuthor)}`;
    }
    if (state.updatesPeriod === 'custom') {
      if (el.dateSince.value) {
        url += `&since=${new Date(el.dateSince.value).toISOString()}`;
      }
      if (el.dateUntil.value) {
        url += `&until=${new Date(el.dateUntil.value + 'T23:59:59').toISOString()}`;
      }
    }

    try {
      el.timelineFeed.innerHTML = '<div class="empty-state"><p>Загрузка среза активности...</p></div>';
      const res = await fetch(url);
      const data = await res.json();
      state.updatesData = data;
      renderUpdatesView(data);
    } catch (err) {
      console.error('Failed to load updates:', err);
    }
  }

  function renderUpdatesView(data) {
    // Metrics
    el.metricTotalCommits.textContent = data.totalCommits || 0;
    el.metricMerges.textContent = `${data.totalMerges || 0} мержей`;

    el.metricAICommits.textContent = data.aiCommitsCount || 0;
    el.metricHumanCommits.textContent = data.humanCommitsCount || 0;

    const total = (data.aiCommitsCount || 0) + (data.humanCommitsCount || 0);
    const aiPct = total > 0 ? Math.round((data.aiCommitsCount / total) * 100) : 0;
    el.metricAIRatio.textContent = `${aiPct}% активности от ИИ`;

    el.metricAdditions.textContent = `+${data.totalAdditions || 0}`;
    el.metricDeletions.textContent = `-${data.totalDeletions || 0}`;
    const net = (data.totalAdditions || 0) - (data.totalDeletions || 0);
    el.metricNetLines.textContent = `${net >= 0 ? '+' : ''}${net} net строк`;

    el.metricPeriodLabel.textContent = data.periodLabel || state.updatesPeriod;
    el.metricBranch.textContent = `Ветка: ${data.branch || state.activeBranch}`;

    // Authors Breakdown
    renderAuthorsList(data.authors || []);

    // Timeline Feed
    renderTimelineFeed(data.commits || []);
  }

  function renderAuthorsList(authors) {
    el.authorsList.innerHTML = '';
    if (authors.length === 0) {
      el.authorsList.innerHTML = '<div style="font-size: 12px; color: var(--text-muted); padding: 8px;">Нет участников за период</div>';
      return;
    }

    // Sort by commits desc
    authors.sort((a, b) => b.commitCount - a.commitCount);

    authors.forEach(a => {
      const item = document.createElement('div');
      item.className = 'author-stat-item' + (state.selectedAuthor === a.name ? ' active' : '');

      const avatarBg = getAvatarColor(a.name);
      const initials = getInitials(a.name);

      let tagBadge = '';
      if (a.isAI) {
        tagBadge = `<span class="badge badge-ai" style="font-size: 10px;">🤖 ${escapeHtml(a.aiTag || 'AI')}</span>`;
      } else {
        tagBadge = '<span class="badge badge-human" style="font-size: 10px;">👤 Человек</span>';
      }

      item.innerHTML = `
        <div class="author-stat-left">
          <div class="author-avatar" style="background-color: ${avatarBg}; color: #fff;">${initials}</div>
          <div style="display: flex; flex-direction: column;">
            <div style="display: flex; align-items: center; gap: 6px;">
              <span style="font-weight: 600; font-size: 12px;">${escapeHtml(a.name)}</span>
              ${tagBadge}
            </div>
            <span style="font-size: 11px; color: var(--text-muted);">${escapeHtml(a.email || '')}</span>
          </div>
        </div>
        <div style="display: flex; flex-direction: column; align-items: flex-end; font-family: var(--font-mono); font-size: 11px;">
          <strong>${a.commitCount} коммитов</strong>
          <span style="font-size: 10px;"><span class="stat-add">+${a.additions}</span> <span class="stat-del">-${a.deletions}</span></span>
        </div>
      `;

      item.addEventListener('click', () => {
        if (state.selectedAuthor === a.name) {
          state.selectedAuthor = null;
        } else {
          state.selectedAuthor = a.name;
        }
        loadUpdates();
      });

      el.authorsList.appendChild(item);
    });
  }

  function renderTimelineFeed(commits) {
    el.timelineFeed.innerHTML = '';
    el.feedCount.textContent = `Показано событий: ${commits.length}`;

    if (commits.length === 0) {
      el.timelineFeed.innerHTML = '<div class="empty-state"><p>За этот период коммитов не было</p></div>';
      return;
    }

    commits.forEach(c => {
      const card = document.createElement('div');
      card.className = 'timeline-item';

      const avatarBg = getAvatarColor(c.authorName);
      const initials = getInitials(c.authorName);

      let badgesHtml = '';
      if (c.isMerge) {
        badgesHtml += '<span class="badge badge-merge">🔀 Merge</span>';
      }
      if (c.isAI) {
        badgesHtml += `<span class="badge badge-ai">🤖 ${escapeHtml(c.aiTag || 'AI')}</span>`;
      } else {
        badgesHtml += '<span class="badge badge-human">👤 Разработчик</span>';
      }

      card.innerHTML = `
        <div style="display: flex; align-items: center; justify-content: space-between;">
          <div style="display: flex; align-items: center; gap: 8px;">
            <div class="author-avatar" style="background-color: ${avatarBg}; color: #fff;">${initials}</div>
            <strong style="font-size: 12.5px;">${escapeHtml(c.authorName)}</strong>
            ${badgesHtml}
          </div>
          <span style="font-size: 11.5px; color: var(--text-muted);">${formatDate(c.date)}</span>
        </div>
        <div style="font-size: 13px; font-weight: 500; color: var(--text-primary); margin: 3px 0;">
          ${escapeHtml(c.message)}
        </div>
        <div style="display: flex; align-items: center; justify-content: space-between; font-size: 11px; color: var(--text-muted);">
          <span style="font-family: var(--font-mono);">${c.shortHash}</span>
          <div style="display: flex; align-items: center; gap: 10px;">
            <span>Файлов: ${c.filesChanged}</span>
            <div class="diff-stat-summary">
              <span class="stat-add">+${c.additions}</span>
              <span class="stat-del">-${c.deletions}</span>
            </div>
            <span style="color: var(--accent-color); font-weight: 500;">Смотреть дифф →</span>
          </div>
        </div>
      `;

      card.addEventListener('click', () => {
        // Switch to Review Tab and open commit!
        switchTab('review');
        selectCommit(c.hash);
      });

      el.timelineFeed.appendChild(card);
    });
  }

  function showEmptyProjectState() {
    el.commitsList.innerHTML = `
      <div class="empty-state">
        <span style="font-size: 32px;">📁</span>
        <p>Добавьте папку с Git-репозиторием для начала работы</p>
        <button class="btn btn-primary" onclick="document.getElementById('addProjectBtn').click()">+ Добавить папку</button>
      </div>
    `;
    el.diffFilesContainer.innerHTML = '';
  }

  // Modal handlers
  function openModal() {
    el.addProjectModal.style.display = 'flex';
    renderRecentProjects();
    el.projectPathInput.focus();
  }

  function closeModal() {
    el.addProjectModal.style.display = 'none';
    el.projectPathInput.value = '';
  }

  function renderRecentProjects() {
    el.recentProjectsContainer.innerHTML = '';
    if (state.projects.length === 0) {
      el.recentProjectsContainer.innerHTML = '<span style="font-size: 12px; color: var(--text-muted);">Список пуст</span>';
      return;
    }

    state.projects.forEach(p => {
      const row = document.createElement('div');
      row.className = 'recent-project-row';
      row.innerHTML = `
        <div style="display: flex; flex-direction: column; overflow: hidden; cursor: pointer; flex: 1;">
          <strong style="font-size: 12.5px;">${escapeHtml(p.name)}</strong>
          <span style="font-size: 11px; color: var(--text-muted); text-overflow: ellipsis; overflow: hidden; white-space: nowrap;">${escapeHtml(p.path)}</span>
        </div>
        <button class="btn-icon" title="Удалить" data-del="${p.id}" style="color: var(--diff-del-text);">✕</button>
      `;

      row.querySelector('div').addEventListener('click', () => {
        closeModal();
        selectProject(p.path);
      });

      row.querySelector('[data-del]').addEventListener('click', (e) => {
        e.stopPropagation();
        removeProject(p.id);
      });

      el.recentProjectsContainer.appendChild(row);
    });
  }

  // Tab switcher
  function switchTab(tab) {
    el.navTabs.forEach(t => t.classList.toggle('active', t.dataset.tab === tab));
    if (tab === 'review') {
      el.reviewPane.classList.remove('hidden');
      el.updatesPane.classList.add('hidden');
    } else {
      el.reviewPane.classList.add('hidden');
      el.updatesPane.classList.remove('hidden');
      loadUpdates();
    }
  }

  // Event Listeners Initialization
  function initEvents() {
    // Theme toggle
    el.themeToggleBtn.addEventListener('click', cycleTheme);

    // Project selection
    el.projectSelect.addEventListener('change', (e) => selectProject(e.target.value));
    el.addProjectBtn.addEventListener('click', openModal);

    // Branch selection
    el.branchSelect.addEventListener('change', (e) => {
      state.activeBranch = e.target.value;
      loadCommits();
      if (!el.updatesPane.classList.contains('hidden')) {
        loadUpdates();
      }
    });

    // Nav tabs
    el.navTabs.forEach(btn => {
      btn.addEventListener('click', () => switchTab(btn.dataset.tab));
    });

    // Search filter
    el.commitSearchInput.addEventListener('input', (e) => {
      state.searchQuery = e.target.value;
      renderCommitsList();
    });

    // Sidebar filter chips
    el.chipBtns.forEach(btn => {
      btn.addEventListener('click', () => {
        el.chipBtns.forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        state.commitFilter = btn.dataset.filter;
        loadCommits();
      });
    });

    // Diff mode (Unified / Split)
    el.modeUnifiedBtn.addEventListener('click', () => {
      el.modeUnifiedBtn.classList.add('active');
      el.modeSplitBtn.classList.remove('active');
      state.diffViewMode = 'unified';
      if (state.selectedCommitDetail) {
        renderDiffFiles(state.selectedCommitDetail.files || []);
      }
    });

    el.modeSplitBtn.addEventListener('click', () => {
      el.modeSplitBtn.classList.add('active');
      el.modeUnifiedBtn.classList.remove('active');
      state.diffViewMode = 'split';
      if (state.selectedCommitDetail) {
        renderDiffFiles(state.selectedCommitDetail.files || []);
      }
    });

    // Modal
    el.closeModalBtn.addEventListener('click', closeModal);
    el.cancelModalBtn.addEventListener('click', closeModal);
    el.confirmAddProjectBtn.addEventListener('click', () => {
      addProject(el.projectPathInput.value);
    });
    el.projectPathInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') addProject(el.projectPathInput.value);
    });

    // Updates time slice buttons
    el.sliceBtns.forEach(btn => {
      btn.addEventListener('click', () => {
        el.sliceBtns.forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        const period = btn.dataset.period;
        state.updatesPeriod = period;

        if (period === 'custom') {
          el.customDateRange.style.display = 'flex';
        } else {
          el.customDateRange.style.display = 'none';
          loadUpdates();
        }
      });
    });

    el.applyCustomDateBtn.addEventListener('click', loadUpdates);

    // Updates AI filters
    const updateAIFilter = (filter, activeBtn) => {
      [el.updatesFilterAll, el.updatesFilterAI, el.updatesFilterHuman].forEach(b => b.classList.remove('active'));
      activeBtn.classList.add('active');
      state.updatesFilterAI = filter;
      loadUpdates();
    };

    el.updatesFilterAll.addEventListener('click', () => updateAIFilter('all', el.updatesFilterAll));
    el.updatesFilterAI.addEventListener('click', () => updateAIFilter('ai_only', el.updatesFilterAI));
    el.updatesFilterHuman.addEventListener('click', () => updateAIFilter('human_only', el.updatesFilterHuman));
  }

  // Init App
  function init() {
    // Read saved theme
    const savedTheme = localStorage.getItem('scodeviewer_theme') || 'system';
    applyTheme(savedTheme);

    initEvents();
    fetchProjects();
  }

  init();
})();
