// =============================================================================
// i18n — lightweight localisation module
// Supported locales: pt_BR (default), en_US
// Locale files: /locales/{lang}/messages.json
// Usage: t('key'), t('key_with_%s', 'value'), t('key_with_%d', 42)
// =============================================================================
var _i18n = {};
var _lang = 'pt_BR';

function t(key) {
    var str = _i18n[key] !== undefined ? _i18n[key] : key;
    for (var i = 1; i < arguments.length; i++) {
        str = str.replace(/%[sd]/, arguments[i]);
    }
    return str;
}

function applyI18n() {
    var htmlEl = document.getElementById('html-root') || document.documentElement;
    htmlEl.lang = _lang === 'pt_BR' ? 'pt-BR' : 'en-US';
    document.querySelectorAll('[data-i18n]').forEach(function(el) {
        var key = el.getAttribute('data-i18n');
        var args = el.getAttribute('data-i18n-args');
        if (args) {
            var parts = args.split('|');
            var fargs = [key].concat(parts);
            el.textContent = t.apply(null, fargs);
        } else {
            el.textContent = t(key);
        }
    });
    document.querySelectorAll('[data-i18n-placeholder]').forEach(function(el) {
        var key = el.getAttribute('data-i18n-placeholder');
        el.placeholder = t(key);
    });
    document.querySelectorAll('[data-i18n-title]').forEach(function(el) {
        var key = el.getAttribute('data-i18n-title');
        el.title = t(key);
    });
    document.querySelectorAll('[data-i18n-aria]').forEach(function(el) {
        var key = el.getAttribute('data-i18n-aria');
        el.setAttribute('aria-label', t(key));
    });
    // sync all lang selectors
    document.querySelectorAll('.zbx-lang-select').forEach(function(el) {
        el.value = _lang;
    });
}

function setLang(lang) {
    if (lang !== 'pt_BR' && lang !== 'en_US') lang = 'pt_BR';
    _lang = lang;
    try { localStorage.setItem('zbx-lang', lang); } catch(e) {}
    fetch('/locales/' + lang + '/messages.json?cb=' + Date.now())
        .then(function(r) { return r.json(); })
        .then(function(data) {
            _i18n = data;
            applyI18n();
        })
        .catch(function() { applyI18n(); });
}

// init: load saved lang or default pt_BR
(function initI18n() {
    var saved = 'pt_BR';
    try { saved = localStorage.getItem('zbx-lang') || 'pt_BR'; } catch(e) {}
    if (saved !== 'pt_BR' && saved !== 'en_US') saved = 'pt_BR';
    _lang = saved;
    fetch('/locales/' + saved + '/messages.json?cb=' + Date.now())
        .then(function(r) { return r.json(); })
        .then(function(data) {
            _i18n = data;
            applyI18n();
        })
        .catch(function() {});
    initTheme();
})();

function applyTheme(theme) {
    var body = document.body;
    if (!body) return;
    body.classList.toggle('theme-light', theme === 'light');
    body.classList.toggle('theme-dark', theme === 'dark');
    var toggle = document.getElementById('theme-toggle');
    if (toggle) {
        toggle.textContent = theme === 'light' ? '🌞' : '🌙';
        toggle.title = theme === 'light' ? 'Modo claro' : 'Modo escuro';
        toggle.setAttribute('aria-pressed', theme === 'dark' ? 'true' : 'false');
    }
}

function setTheme(theme) {
    if (theme !== 'light' && theme !== 'dark') theme = 'dark';
    try { localStorage.setItem('zbx-theme', theme); } catch(e) {}
    applyTheme(theme);
}

function initTheme() {
    var saved = null;
    try { saved = localStorage.getItem('zbx-theme'); } catch(e) { saved = null; }
    if (saved !== 'light' && saved !== 'dark') {
        saved = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }
    setTheme(saved);
}

// =============================================================================
// Toggle show/hide token with eye icon
const toggleToken = document.getElementById('toggle-token');
const tokenInput = document.getElementById('zabbix_token');
const eyeIcon = document.getElementById('eye-icon');
let isVisible = false;
toggleToken.addEventListener('click', function() {
    isVisible = !isVisible;
    tokenInput.type = isVisible ? 'text' : 'password';
    eyeIcon.innerHTML = isVisible
        ? '<circle cx="12" cy="12" r="3"/><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7-10-7-10-7z"/><line x1="1" y1="1" x2="23" y2="23" stroke="#888" stroke-width="2"/>'
        : '<circle cx="12" cy="12" r="3"/><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7-10-7-10-7z"/>';
});
toggleToken.addEventListener('keydown', function(e) {
    if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggleToken.click();
    }
});

// Wire header lang selector
(function() {
    var sel = document.getElementById('lang-select');
    if (sel) {
        sel.classList.add('zbx-lang-select');
        sel.addEventListener('change', function() { setLang(this.value); });
    }
    var themeBtn = document.getElementById('theme-toggle');
    if (themeBtn) {
        themeBtn.addEventListener('click', function() {
            var current = document.body.classList.contains('theme-light') ? 'light' : 'dark';
            setTheme(current === 'light' ? 'dark' : 'light');
        });
    }
})();

function setProgressUI(message, percent, badgeText) {
    const bar = document.getElementById('progress-bar');
    const progressEl = bar ? bar.querySelector('.progress') : null;
    const labelEl = document.getElementById('progress-text');
    const badgeEl = document.getElementById('progress-badge');
    if (bar) bar.style.display = 'block';
    if (progressEl) progressEl.style.width = Math.max(0, Math.min(100, percent)) + '%';
    if (labelEl) labelEl.textContent = message || t('generating');
    if (badgeEl) badgeEl.textContent = badgeText || 'Em andamento';
}

function showInlineMessage(title, message, kind) {
    const reportArea = document.getElementById('report-area');
    if (!reportArea) return;
    reportArea.className = 'report-area';
    reportArea.style.display = 'block';
    if (kind === 'error') {
        reportArea.classList.add('report-error-state');
        reportArea.innerHTML = '<div class="report-empty-icon">⚠️</div><h3>' + title + '</h3><p>' + message + '</p>';
    } else {
        reportArea.classList.add('report-empty-state');
        reportArea.innerHTML = '<div class="report-empty-icon">📊</div><h3>' + title + '</h3><p>' + message + '</p>';
    }
}

var _appConfig = { api_key_required: false, tls_verify: false };

function getAppApiKey() {
    var el = document.getElementById('app_api_key');
    if (el && el.value) {
        try { localStorage.setItem('zbx-app-api-key', el.value); } catch(e) {}
        return el.value;
    }
    try { return localStorage.getItem('zbx-app-api-key') || ''; } catch(e) { return ''; }
}

function apiHeaders(extra) {
    var h = { 'Content-Type': 'application/json' };
    var key = getAppApiKey();
    if (key) h['X-API-Key'] = key;
    if (extra) {
        Object.keys(extra).forEach(function(k) { h[k] = extra[k]; });
    }
    return h;
}

function loadAppConfig() {
    return fetch('/api/config')
        .then(function(r) { return r.json(); })
        .then(function(cfg) {
            _appConfig = cfg || _appConfig;
            if (_appConfig.api_key_required) {
                var row = document.getElementById('app-api-key-row');
                if (row) row.style.display = '';
                try {
                    var saved = localStorage.getItem('zbx-app-api-key');
                    var input = document.getElementById('app_api_key');
                    if (saved && input) input.value = saved;
                } catch(e) {}
            }
        })
        .catch(function() {});
}

function showHostPicker(hosts) {
    return new Promise(function(resolve) {
        var modal = document.getElementById('host-picker-modal');
        var list = document.getElementById('host-picker-list');
        var cancelBtn = document.getElementById('host-picker-cancel');
        if (!modal || !list) { resolve(null); return; }
        list.innerHTML = '';
        hosts.forEach(function(h) {
            var btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'host-picker-item';
            btn.innerHTML = '<strong>' + (h.name || h.hostid) + '</strong><span>ID ' + (h.hostid || '') + '</span>';
            btn.addEventListener('click', function() {
                modal.hidden = true;
                modal.setAttribute('aria-hidden', 'true');
                resolve(h);
            });
            list.appendChild(btn);
        });
        function onCancel() {
            modal.hidden = true;
            modal.setAttribute('aria-hidden', 'true');
            cancelBtn.removeEventListener('click', onCancel);
            resolve(null);
        }
        cancelBtn.addEventListener('click', onCancel);
        modal.hidden = false;
        modal.setAttribute('aria-hidden', 'false');
        try { applyI18n(); } catch(e) {}
    });
}

function startReportGeneration(url, token, hostFilter, days, hostFilterB, metricItemKeys) {
    var payload = {
        zabbix_url: url,
        zabbix_token: token,
        host_filter: hostFilter,
        host_filter_b: hostFilterB || '',
        days: days,
        metric_item_keys: metricItemKeys || {}
    };
    fetch('/api/start', {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify(payload)
    })
    .then(function(res) {
        if (res.status === 429) {
            document.getElementById('progress-bar').style.display = 'none';
            showInlineMessage(t('error_rate_limit'), t('error_rate_limit'), 'error');
            return null;
        }
        return res.json();
    })
    .then(function(data) {
        if (!data) return;
        if (data.task_id) {
            checkProgress(data.task_id, 0);
        } else {
            document.getElementById('progress-bar').style.display = 'none';
            var msg = (data && data.error) ? data.error : t('error_start_task');
            showInlineMessage(t('error_start_task'), msg, 'error');
        }
    })
    .catch(function() {
        document.getElementById('progress-bar').style.display = 'none';
        showInlineMessage(t('error_start_task'), t('error_start_task'), 'error');
    });
}

function collectMetricItemKeys() {
    var keys = {};
    var cpu = (document.getElementById('host_metric_cpu') && document.getElementById('host_metric_cpu').value || '').trim();
    var mem = (document.getElementById('host_metric_memory') && document.getElementById('host_metric_memory').value || '').trim();
    var net = (document.getElementById('host_metric_net') && document.getElementById('host_metric_net').value || '').trim();
    if (cpu) keys.cpu = cpu;
    if (mem) keys.memory = mem;
    if (net) keys.net = net;
    return keys;
}

function resolveHostFilter(url, token, hostFilter) {
    if (!hostFilter) return Promise.resolve('');
    return fetch('/api/hosts/resolve', {
        method: 'POST',
        headers: apiHeaders(),
        body: JSON.stringify({ zabbix_url: url, zabbix_token: token, host_filter: hostFilter })
    })
    .then(function(res) { return res.json(); })
    .then(function(data) {
        if (data.status === 'ambiguous' && data.hosts && data.hosts.length) {
            document.getElementById('progress-bar').style.display = 'none';
            return showHostPicker(data.hosts).then(function(selected) {
                if (!selected) return null;
                return selected.name || selected.hostid || hostFilter;
            });
        }
        if (data.status === 'not_found') {
            document.getElementById('progress-bar').style.display = 'none';
            showInlineMessage(t('host_picker.not_found'), t('host_picker.not_found'), 'error');
            return null;
        }
        if (data.error) {
            document.getElementById('progress-bar').style.display = 'none';
            showInlineMessage(t('error_start_task'), data.error, 'error');
            return null;
        }
        if (data.status === 'ok' && data.host) {
            return data.host.name || data.host.hostid || hostFilter;
        }
        return hostFilter;
    });
}

loadAppConfig();

// --- AJAX para submit, progress, report ---
document.getElementById('zabbix-form').addEventListener('submit', function(e) {
    e.preventDefault();
    const reportArea = document.getElementById('report-area');
    if (reportArea) {
        reportArea.className = 'report-area report-empty-state';
        reportArea.innerHTML = '<div class="report-empty-icon">⏳</div><h3>Preparando análise</h3><p>Estamos iniciando a coleta dos dados do Zabbix para gerar seu relatório.</p>';
        reportArea.style.display = 'block';
    }
    setProgressUI(t('generating'), 8, 'Iniciando');

    var url = document.getElementById('zabbix_url').value;
    var token = document.getElementById('zabbix_token').value;
    var hostFilter = (document.getElementById('host_filter').value || '').trim();
    var hostFilterB = (document.getElementById('host_filter_b') && document.getElementById('host_filter_b').value || '').trim();
    var days = parseInt(document.getElementById('alert_days').value, 10) || 90;
    var metricKeys = collectMetricItemKeys();

    if (_appConfig.api_key_required && !getAppApiKey()) {
        document.getElementById('progress-bar').style.display = 'none';
        showInlineMessage(t('error_api_key_required'), t('error_api_key_required'), 'error');
        return;
    }

    resolveHostFilter(url, token, hostFilter).then(function(resolvedA) {
        if (resolvedA === null) return;
        if (!hostFilterB) {
            startReportGeneration(url, token, resolvedA || '', days, '', metricKeys);
            return;
        }
        resolveHostFilter(url, token, hostFilterB).then(function(resolvedB) {
            if (resolvedB === null) return;
            setProgressUI(t('generating'), 8, 'Iniciando');
            startReportGeneration(url, token, resolvedA || '', days, resolvedB || '', metricKeys);
        });
    }).catch(function() {
        document.getElementById('progress-bar').style.display = 'none';
        showInlineMessage(t('error_start_task'), t('error_start_task'), 'error');
    });
});

// ---------------------------------------------------------------------------
// initReportTabs(root) — tab switching (works even when inline scripts fail)
// ---------------------------------------------------------------------------
function initReportTabs(root) {
    if (!root) return;
    window.showTab = function(id) {
        var scope = root.closest('.report-main') || root;
        scope.querySelectorAll('.tab-panel').forEach(function(p) { p.style.display = 'none'; });
        var el = scope.querySelector('#' + id) || document.getElementById(id);
        if (el) el.style.display = 'block';
        scope.querySelectorAll('.tab-btn').forEach(function(b) {
            b.classList.toggle('active', b.getAttribute('data-tab') === id);
        });
        try {
            var panel = el;
            if (panel) initGauges(panel);
            if (id === 'tab-host') {
                if (typeof window.zbxFlushHostCharts === 'function') {
                    window.zbxFlushHostCharts(panel);
                }
                if (typeof window.zbxResizeChartsIn === 'function') {
                    setTimeout(function() { window.zbxResizeChartsIn(panel); }, 30);
                }
            }
        } catch(e) {}
    };
    root.querySelectorAll('.tab-btn').forEach(function(b) {
        if (b._tabBound) return;
        b._tabBound = true;
        b.addEventListener('click', function() { window.showTab(this.getAttribute('data-tab')); });
    });
}

// ---------------------------------------------------------------------------
// enhanceReportUI(root) — KPI cards on summary tab + dark-theme helpers
// ---------------------------------------------------------------------------
function enhanceReportUI(root) {
    if (!root) return;
    var tab = root.querySelector('#tab-resumo');
    if (!tab || tab.querySelector('.summary-kpi-grid')) return;
    var rows = tab.querySelectorAll('.modern-table tbody tr');
    if (!rows.length) return;

    var grid = document.createElement('div');
    grid.className = 'summary-kpi-grid';
    rows.forEach(function(row) {
        var cells = row.querySelectorAll('td');
        if (cells.length < 2) return;
        var card = document.createElement('article');
        card.className = 'summary-kpi-card';
        var labelEl = cells[0].cloneNode(true);
        var valueEl = document.createElement('strong');
        valueEl.className = 'summary-kpi-value';
        valueEl.textContent = cells[1].textContent.trim();
        card.appendChild(labelEl);
        labelEl.className = 'summary-kpi-label';
        card.appendChild(valueEl);
        if (cells[2] && cells[2].textContent.trim()) {
            var detail = document.createElement('span');
            detail.className = 'summary-kpi-detail';
            detail.textContent = cells[2].textContent.trim();
            card.appendChild(detail);
        }
        grid.appendChild(card);
    });

    var tableWrap = tab.querySelector('.table-responsive');
    if (tableWrap && grid.children.length) {
        tab.insertBefore(grid, tableWrap);
        tableWrap.classList.add('summary-table-compact');
    }
}

// ---------------------------------------------------------------------------
// renderReport(html, titleHint)
// Shared function used by both the live generation flow and the DB load flow.
// Assembles the full report layout (header + export/print buttons + content)
// into #report-area and wires all button event listeners.
// ---------------------------------------------------------------------------
function renderReport(html, titleHint, createdAt) {
    const reportArea = document.getElementById('report-area');
    reportArea.className = 'report-area';
    reportArea.style.display = 'block';

    const container = document.createElement('div');
    container.className = 'report-layout';

    const header = document.createElement('div');
    header.className = 'report-frame-header';
    const geradoEm = createdAt ? new Date(createdAt).toLocaleString(t('locale_code')) : new Date().toLocaleString(t('locale_code'));
    header.innerHTML = `
        <div class="frame-meta">
            <div class="frame-title">${t('report_title')}</div>
            <div class="frame-sub">${t('generated_at')} ${geradoEm}</div>
        </div>`;

    const left = document.createElement('div');
    left.className = 'report-main';
    left.innerHTML = html;

    const right = document.createElement('aside');
    right.className = 'report-side';

    let sidebar = left.querySelector('.report-sidebar');
    const envMeta = left.querySelector('.report-env-meta');
    const tabs = left.querySelector('.tabs-container');
    if (!sidebar) {
        sidebar = document.createElement('aside');
        sidebar.className = 'report-sidebar';
    }
    if (envMeta && envMeta.parentNode !== sidebar) {
        sidebar.appendChild(envMeta);
    }
    if (tabs && tabs.parentNode !== sidebar) {
        sidebar.appendChild(tabs);
    }
    if (sidebar.parentNode && sidebar.parentNode !== right) {
        right.appendChild(sidebar);
    }

    // build action buttons reusing existing markup / CSS classes
    const actionGroup = document.createElement('div');
    actionGroup.className = 'action-group';
    actionGroup.innerHTML = `
        <button class="btn small icon-btn" data-action="new-report" data-i18n-aria="aria_new_report" data-i18n-title="aria_new_report" aria-label="${t('aria_new_report')}" title="${t('aria_new_report')}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <polyline points="14 2 14 8 20 8" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <line x1="12" y1="11" x2="12" y2="17" stroke="#fff" stroke-width="2" stroke-linecap="round"/>
                <line x1="9" y1="14" x2="15" y2="14" stroke="#fff" stroke-width="2" stroke-linecap="round"/>
            </svg>
        </button>
        <button class="btn small icon-btn" data-action="export" data-i18n-aria="aria_export_html" data-i18n-title="aria_export_html" aria-label="${t('aria_export_html')}" title="${t('aria_export_html')}">
            <svg width="16" height="16" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none">
                <path d="M6 2h7l4 4v12a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z" stroke="#fff" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round"/>
                <path d="M13 2v5h5" stroke="#fff" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round"/>
                <text x="7.5" y="15.2" font-size="5.2" font-family="Arial, sans-serif" fill="#fff">HTML</text>
            </svg>
        </button>
        <button class="btn small icon-btn" data-action="print" data-i18n-aria="aria_print_pdf" data-i18n-title="aria_print_pdf" aria-label="${t('aria_print_pdf')}" title="${t('aria_print_pdf')}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6 9V3h12v6" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><rect x="6" y="13" width="12" height="8" rx="2" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </button>`;

    // place action group inside the report frame header
    let frameActions = header.querySelector('.frame-actions');
    if (!frameActions) {
        frameActions = document.createElement('div');
        frameActions.className = 'frame-actions';
        header.appendChild(frameActions);
    }
    const inserted = actionGroup.cloneNode(true);
    frameActions.appendChild(inserted);
    try {
        const btns = inserted.querySelectorAll('button');
        if (btns[0]) btns[0].id = 'btn-new-report';
        if (btns[1]) btns[1].id = 'btn-export-html';
        if (btns[2]) btns[2].id = 'btn-print';
    } catch(e) {}

    // assemble container
    container.appendChild(header);
    const contentShell = document.createElement('div');
    contentShell.className = 'report-content-shell';
    const cols = document.createElement('div');
    cols.className = 'report-layout-cols';
    if (right.children && right.children.length > 0) {
        cols.appendChild(right);
    } else {
        cols.classList.add('report-layout-cols--full');
    }
    cols.appendChild(left);
    contentShell.appendChild(cols);
    container.appendChild(contentShell);

    reportArea.innerHTML = '';
    reportArea.appendChild(container);
    // translate any server-provided data-i18n placeholders inside the inserted report
    try { applyI18n(); } catch(e) {}
    setTimeout(function(){ try { applyI18n(); } catch(e) {} }, 0);

    // hide the input form when report is displayed
    const form = document.getElementById('zabbix-form');
    if (form) form.style.display = 'none';
    try {
        document.body.classList.remove('show-login');
        document.body.classList.add('report-active');
        document.querySelectorAll('.side-hero, .panel-header, .hero-copy').forEach(function(el) {
            el.style.display = 'none';
        });
    } catch(e) {}

    // execute any inline scripts included in the inserted HTML
    (function executeInsertedScripts(el) {
        Array.from(el.querySelectorAll('script')).forEach(oldScript => {
            const newScript = document.createElement('script');
            if (oldScript.src) { newScript.src = oldScript.src; newScript.async = false; }
            else { newScript.text = oldScript.textContent; }
            oldScript.parentNode.replaceChild(newScript, oldScript);
        });
    })(left);

    // initialize doughnut gauges
    initGauges(left);
    // initialize table search / sort / pagination
    initTableEnhancements(left);
    initHostAlertFilters(left);
    initReportTabs(container);
    enhanceReportUI(left);

    var hostTab = container.querySelector('#tab-host[data-auto-open="1"]');
    if (hostTab && typeof window.showTab === 'function') {
        setTimeout(function() { window.showTab('tab-host'); }, 50);
    }

    // helper: extract the Ambiente name from report text for use in filenames
    function extractAmbienteName(leftEl) {
        try {
            const text = (leftEl.innerText || leftEl.textContent || '').replace(/\u00A0/g, ' ');
            const m = text.match(/Ambiente:\s*([^\r\n]+)/i);
            if (m && m[1]) {
                let v = m[1].trim();
                v = v.split(/Vers\u00e3|Versao|Vers\u00c3o|Vers\.|Vers\:|\sResumo|\sProcessos|\sTop|\sItems|\sTemplates/i)[0].trim();
                v = v.replace(/^https?:\/\//i, '').replace(/\/$/, '');
                v = v.split(/\s+/)[0];
                return v;
            }
        } catch(e) {}
        return '';
    }

    // build a standalone full-document HTML string for export
    function buildFullDocumentHTML_fromContainer(containerEl, cssText, title) {
        const clone = containerEl.cloneNode(true);
        ['.action-group', '.frame-actions', '#btn-print', '#btn-export-html'].forEach(sel => {
            clone.querySelectorAll(sel).forEach(n => n.parentNode && n.parentNode.removeChild(n));
        });
        clone.querySelectorAll('.tab-panel').forEach(p => { p.style.display = 'block'; });
        clone.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        // expand paginated tables: show all rows, strip controls for clean static export
        clone.querySelectorAll('.modern-table[data-dt-enhanced]').forEach(t => {
            t.querySelectorAll('tbody tr').forEach(r => r.style.removeProperty('display'));
            t.querySelectorAll('.dt-no-results-row').forEach(r => r.parentNode && r.parentNode.removeChild(r));
        });
        clone.querySelectorAll('.dt-toolbar, .dt-pagination').forEach(el => el.parentNode && el.parentNode.removeChild(el));
        clone.querySelectorAll('.dt-sort-icon').forEach(ic => ic.parentNode && ic.parentNode.removeChild(ic));
        clone.querySelectorAll('.dt-sortable').forEach(th => th.classList.remove('dt-sortable', 'dt-sort-asc', 'dt-sort-desc'));
        const rSide = clone.querySelector('.report-side');
        const rMain = clone.querySelector('.report-main');
        if (rSide && rMain) {
            Array.from(rSide.children)
                .filter(ch => !(ch.classList && ch.classList.contains('action-group')) && !ch.classList.contains('frame-actions'))
                .forEach(ch => { try { rMain.appendChild(ch.cloneNode(true)); } catch(e) {} });
            rSide.parentNode && rSide.parentNode.removeChild(rSide);
        }
        // wrap in the same .container.full-width > .report-area structure used in the
        // live page so the exported HTML has the same margins, padding and backgrounds
        const bodyInner = `<div class="container full-width"><div class="page-header"><h1>ZBX-Easy</h1></div><div class="report-area">${clone.outerHTML}</div></div>`;
        const head = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${title || t('report_title')}</title>` +
            (cssText ? `<style>${cssText}</style>` : '') + `</head><body>`;
        const chartsInit = `<script src="https://cdn.jsdelivr.net/npm/chart.js"></` + `script>` +
            `<script>window.addEventListener('load',function(){try{` +
            `var ttEl=null;` +
            `function getTooltip(){if(!ttEl){ttEl=document.createElement('div');ttEl.style.cssText='position:fixed;background:rgba(0,0,0,0.82);color:#fff;padding:5px 11px;border-radius:5px;font-size:12.5px;pointer-events:none;z-index:99999;white-space:nowrap;opacity:0;transition:opacity 0.12s';document.body.appendChild(ttEl);}return ttEl;}` +
            `Array.from(document.querySelectorAll('canvas[data-total]')).forEach(function(canvas){try{` +
            `var tot=parseInt(canvas.getAttribute('data-total'))||0,` +
            `uns=parseInt(canvas.getAttribute('data-unsupported'))||0,` +
            `sup=Math.max(tot-uns,0);` +
            `var extTT=function(ctx){var el=getTooltip();var tm=ctx.tooltip;if(!tm||tm.opacity===0){el.style.opacity='0';return;}var lines=[];(tm.body||[]).forEach(function(b){lines=lines.concat(b.lines);});` +
            `el.innerHTML=lines.map(function(l){var m=l.match(/^(.+):\\s*(\\d+)/);if(m){var p=tot>0?((parseInt(m[2])/tot)*100).toFixed(2):'0.00';return m[1]+': <strong>'+m[2]+'</strong> ('+p+'%)';}return l;}).join('<br>');` +
            `var r=ctx.chart.canvas.getBoundingClientRect();el.style.left=Math.min(r.left+tm.caretX+14,window.innerWidth-220)+'px';el.style.top=(r.top+tm.caretY-14)+'px';el.style.opacity='1';};` +
            `new Chart(canvas.getContext('2d'),{type:'doughnut',data:{labels:[canvas.getAttribute('data-unsupported-label')||'${t('label_unsupported')}',canvas.getAttribute('data-supported-label')||'${t('label_supported')}'],datasets:[{data:[uns,sup],backgroundColor:[canvas.getAttribute('data-color-unsupported')||'#ff7a7a',canvas.getAttribute('data-color-supported')||'#66c2a5']}]},options:{responsive:true,maintainAspectRatio:false,cutout:'60%',plugins:{legend:{display:false},tooltip:{enabled:false,external:extTT}}}});` +
            `canvas.addEventListener('mouseleave',function(){var el=document.getElementById('cj-gauge-tooltip')||ttEl;if(el)el.style.opacity='0';});` +
            `}catch(e){}});` +
            `if(window.zbxFlushHostCharts){window.zbxFlushHostCharts(document);}` +
            `if(window.zbxResizeChartsIn){setTimeout(function(){window.zbxResizeChartsIn(document);},60);}` +
            `}catch(e){}});</` + `script>`;
        return head + bodyInner + chartsInit + `</body></html>`;
    }

    // determine filename-safe title (prefer extracted Ambiente, fall back to titleHint)
    const ambienteName = extractAmbienteName(left) || titleHint || t('default_environment');
    const ambienteSafe = ('' + ambienteName).replace(/[^0-9A-Za-z-_\. ]+/g, '_').slice(0, 80);
    const documentTitleEscaped = (t('report_filename_prefix') + ambienteName).replace(/"/g, '');

    // wire Novo Relatório button — returns to the generation form
    const btnNewReport = document.getElementById('btn-new-report');
    if (btnNewReport) btnNewReport.addEventListener('click', function() {
        const ra = document.getElementById('report-area');
        if (ra) { ra.style.display = 'none'; ra.innerHTML = ''; }
        const pb = document.getElementById('progress-bar');
        if (pb) pb.style.display = 'none';
        const form = document.getElementById('zabbix-form');
        if (form) form.style.display = '';
        try {
            document.body.classList.add('show-login');
            document.body.classList.remove('report-active');
            document.querySelectorAll('.side-hero, .panel-header, .hero-copy').forEach(function(el) {
                el.style.display = '';
            });
        } catch(e) {}
        window.scrollTo(0, 0);
    });

    // wire Print button
    const btnPrint = document.getElementById('btn-print');
    if (btnPrint) btnPrint.addEventListener('click', function() {
        try {
            const panels = Array.from(document.querySelectorAll('.tab-panel'));
            const prevDisplays = panels.map(p => p.style.display);
            panels.forEach(p => p.style.display = 'block');

            const tabBtns = Array.from(document.querySelectorAll('.tab-btn'));
            const prevActive = tabBtns.map(b => b.classList.contains('active'));
            tabBtns.forEach(b => b.classList.remove('active'));

            const actionEls = Array.from(document.querySelectorAll('.action-group, .frame-actions'));
            const prevActionDisplay = actionEls.map(el => el.style.display || '');
            actionEls.forEach(el => el.style.display = 'none');

            const rRight = document.querySelector('.report-side');
            const rMain  = document.querySelector('.report-main');
            let movedClones = [], rightWasHidden = false;
            if (rRight && rMain) {
                Array.from(rRight.children)
                    .filter(ch => !(ch.classList && ch.classList.contains('action-group')) && !ch.classList.contains('frame-actions'))
                    .forEach(ch => { try { const c = ch.cloneNode(true); rMain.appendChild(c); movedClones.push(c); } catch(e) {} });
                if (rRight.style.display !== 'none') { rightWasHidden = true; rRight.style.display = 'none'; }
            }

            const restore = function() {
                try {
                    panels.forEach((p, i) => p.style.display = prevDisplays[i] || '');
                    tabBtns.forEach((b, i) => { if (prevActive[i]) b.classList.add('active'); else b.classList.remove('active'); });
                    actionEls.forEach((el, i) => el.style.display = prevActionDisplay[i] || '');
                    movedClones.forEach(c => c.parentNode && c.parentNode.removeChild(c));
                    if (rRight && rightWasHidden) rRight.style.display = '';
                } catch(e) {}
                window.removeEventListener('afterprint', restore);
            };
            window.addEventListener('afterprint', restore);
            setTimeout(restore, 2000);
            window.print();
        } catch(err) { alert(t('error_print') + err); }
    });

    // wire Export HTML button
    const btnExport = document.getElementById('btn-export-html');
    if (btnExport) btnExport.addEventListener('click', function() {
        try {
            Promise.all([
                fetch('/static/style.css?v=3').then(r => r.text()).catch(() => ''),
                fetch('/static/custom.css?v=3').then(r => r.text()).catch(() => '')
            ]).then(function(cssParts) {
                const cssText = cssParts.filter(Boolean).join('\n');
                const fullHtml = buildFullDocumentHTML_fromContainer(container, cssText, documentTitleEscaped);
                const blob = new Blob([fullHtml], { type: 'text/html' });
                const url  = URL.createObjectURL(blob);
                const a    = document.createElement('a');
                a.href     = url;
                a.download = t('report_filename_prefix') + ambienteSafe + '.html';
                document.body.appendChild(a);
                a.click();
                a.remove();
                URL.revokeObjectURL(url);
            });
        } catch(err) { alert(t('error_export') + err); }
    });

    // keyboard accessibility for all action buttons
    ['btn-new-report', 'btn-print', 'btn-export-html'].forEach(id => {
        const el = document.getElementById(id);
        if (!el) return;
        el.setAttribute('tabindex', '0');
        el.addEventListener('keydown', function(e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); this.click(); } });
    });
}

function checkProgress(taskId, progress) {
    fetch('/api/progress/' + taskId)
        .then(res => res.json())
        .then(data => {
            var pm = '';
            if (data.progress_msg) {
                pm = data.progress_msg || '';
                if (pm && _i18n[pm] !== undefined) {
                    pm = t(pm);
                }
            }
            if (data.status === 'done') {
                setProgressUI(pm || 'Relatório concluído', 100, 'Concluído');
                fetch('/api/report/' + taskId)
                    .then(res => res.text())
                    .then(html => {
                        document.getElementById('progress-bar').style.display = 'none';
                        renderReport(html, '');
                    });
            } else if (data.status === 'processing') {
                progress = Math.min(progress + 18, 92);
                setProgressUI(pm || t('generating'), progress, 'Processando');
                setTimeout(function() { checkProgress(taskId, progress); }, 800);
            } else if (data.status === 'error') {
                document.getElementById('progress-bar').style.display = 'none';
                showInlineMessage(t('error_process_task'), data.report || t('error_process_task'), 'error');
                try { applyI18n(); } catch(e) {}
            } else {
                document.getElementById('progress-bar').style.display = 'none';
                showInlineMessage(t('error_process_task'), t('error_process_task'), 'error');
            }
        });
}

// Ao carregar a página, verifica se o banco está configurado.
// Se DB_HOST não estiver definido no servidor, o card "Relatórios Salvos" é ocultado automaticamente.
(function checkDbStatus() {
    fetch('/api/db-status')
        .then(res => res.json())
        .then(data => {
            if (!data.db_enabled) {
                var dbControls = document.querySelector('.db-controls');
                if (dbControls) dbControls.style.display = 'none';
            }
        })
        .catch(function() { /* ignora erro — mantém card visível como fallback seguro */ });
})();

// Load list of reports from server DB and populate selector
function loadReportList() {
    fetch('/api/reports')
        .then(res => res.json())
        .then(data => {
            const sel = document.getElementById('reportSelect');
            sel.innerHTML = '<option value="">' + t('option_select') + '</option>';
            if (data && data.reports) {
                data.reports.forEach(r => {
                    const opt = document.createElement('option');
                    opt.value = r.id;
                    opt.dataset.createdAt = r.created_at || '';
                    const d = new Date(r.created_at);
                    const label = (r.zabbix_url || r.name || (t('report_prefix') + r.id))
                        .replace(/^https?:\/\//, '')  // remove http:// ou https://
                        .replace(/\/$/, '');           // remove trailing slash
                    opt.text = label + ' \u2014 ' + d.toLocaleString(t('locale_code'));
                    sel.appendChild(opt);
                });
            }
        }).catch(function() { /* ignora erro silenciosamente no carregamento automático */ });
}



// Carrega a lista automaticamente ao abrir a página
document.addEventListener('DOMContentLoaded', function() {
    loadReportList();
});

// Load selected report from DB and render inline (same layout + export/print buttons)
document.getElementById('btn-open-db').addEventListener('click', function() {
    const sel = document.getElementById('reportSelect');
    if (!sel) return alert(t('alert_no_selector'));
    const id = sel.value;
    if (!id) return alert(t('alert_select_report'));
    // ?raw=1 causes Go handler to return only the HTML fragment so renderReport can assemble the layout
    fetch('/api/reportdb/' + id + '?raw=1')
        .then(res => {
            if (!res.ok) throw new Error(t('error_report_not_found'));
            return res.text();
        })
        .then(html => {
            // pass option label as titleHint fallback for filenames
            const selOpt = sel.options[sel.selectedIndex];
            const optText = selOpt ? selOpt.text : '';
            const createdAt = selOpt ? selOpt.dataset.createdAt : '';
            renderReport(html, optText, createdAt);
        })
        .catch(err => alert(t('error_open_report') + err));
});

// Helper: reload the reports list into the selector
function reloadReportList() {
    fetch('/api/reports')
        .then(res => res.json())
        .then(data => {
            const sel = document.getElementById('reportSelect');
            sel.innerHTML = '<option value="">' + t('option_select') + '</option>';
            if (data && data.reports) {
                data.reports.forEach(r => {
                    const opt = document.createElement('option');
                    opt.value = r.id;
                    opt.dataset.createdAt = r.created_at || '';
                    const d = new Date(r.created_at);
                    const label = (r.zabbix_url || r.name || (t('report_prefix') + r.id))
                        .replace(/^https?:\/\//, '')
                        .replace(/\/$/, '');
                    opt.text = label + ' \u2014 ' + d.toLocaleString(t('locale_code'));
                    sel.appendChild(opt);
                });
            }
        }).catch(err => { alert(t('error_reload_list') + err); });
}

// Delete selected report
document.getElementById('btn-delete-db').addEventListener('click', function() {
    const sel = document.getElementById('reportSelect');
    const id = sel ? sel.value : '';
    if (!id) return alert(t('alert_select_to_delete'));
    const label = sel.options[sel.selectedIndex] ? sel.options[sel.selectedIndex].text : id;
    if (!confirm(t('confirm_delete_report', label))) return;
    fetch('/api/reportdb/' + id, { method: 'DELETE', headers: apiHeaders() })
        .then(res => res.json())
        .then(data => {
            if (data.error) { alert(t('error_server') + data.error); return; }
            reloadReportList();
        })
        .catch(err => alert(t('error_delete') + err));
});

// Delete all reports
document.getElementById('btn-delete-all-db').addEventListener('click', function() {
    if (!confirm(t('confirm_delete_all'))) return;
    fetch('/api/reports', { method: 'DELETE', headers: apiHeaders() })
        .then(res => res.json())
        .then(data => {
            if (data.error) { alert(t('error_server') + data.error); return; }
            const n = data.deleted !== undefined ? data.deleted : '?';
            reloadReportList();
            alert(t('deleted_count', n));
        })
        .catch(err => alert(t('error_delete') + err));
});

// Initialize doughnut gauges inside a given container
function initGauges(container) {
    if (typeof Chart === 'undefined') return; // Chart.js not loaded

    // Shared external tooltip rendered in <body> — never clipped by canvas bounds
    function getGaugeTooltipEl() {
        let el = document.getElementById('cj-gauge-tooltip');
        if (!el) {
            el = document.createElement('div');
            el.id = 'cj-gauge-tooltip';
            el.style.cssText = [
                'position:fixed',
                'background:rgba(0,0,0,0.82)',
                'color:#fff',
                'padding:5px 11px',
                'border-radius:5px',
                'font-size:12.5px',
                'font-family:inherit',
                'pointer-events:none',
                'z-index:99999',
                'white-space:nowrap',
                'opacity:0',
                'transition:opacity 0.12s'
            ].join(';');
            document.body.appendChild(el);
        }
        return el;
    }

    function makeExternalTooltip(total) {
        return function(context) {
            var el = getGaugeTooltipEl();
            var tm = context.tooltip;
            if (!tm || tm.opacity === 0) { el.style.opacity = '0'; return; }
            if (tm.body) {
                var lines = [];
                tm.body.forEach(function(b) { lines = lines.concat(b.lines); });
                el.innerHTML = lines.map(function(l) {
                    // rebuild with percentage using total in closure
                    var match = l.match(/^(.+):\s*(\d+)/);
                    if (match) {
                        var label = match[1];
                        var v = parseInt(match[2]);
                        var p = total > 0 ? ((v / total) * 100).toFixed(2) : '0.00';
                        return label + ': <strong>' + v + '</strong> (' + p + '%)';
                    }
                    return l;
                }).join('<br>');
            }
            var rect = context.chart.canvas.getBoundingClientRect();
            var x = rect.left + tm.caretX + 14;
            var y = rect.top  + tm.caretY - 14;
            // keep tooltip inside viewport horizontally
            var vpw = window.innerWidth;
            el.style.left = Math.min(x, vpw - 220) + 'px';
            el.style.top  = y + 'px';
            el.style.opacity = '1';
        };
    }

    const canvases = Array.from(container.querySelectorAll('canvas[data-total]'));
    canvases.forEach(canvas => {
        try {
            const total = parseInt(canvas.getAttribute('data-total')) || 0;
            const unsupported = parseInt(canvas.getAttribute('data-unsupported')) || 0;
            const supported = Math.max(total - unsupported, 0);
            const ctx = canvas.getContext('2d');
            if (canvas._chartInstance) {
                try { canvas._chartInstance.destroy(); } catch(e) {}
            }
            const unsupportedLabel = canvas.getAttribute('data-unsupported-label') || t('label_unsupported');
            const supportedLabel = canvas.getAttribute('data-supported-label') || t('label_supported');
            const colorUnsupported = canvas.getAttribute('data-color-unsupported') || '#ff7a7a';
            const colorSupported = canvas.getAttribute('data-color-supported') || '#66c2a5';
            const chart = new Chart(ctx, {
                type: 'doughnut',
                data: {
                    labels: [unsupportedLabel, supportedLabel],
                    datasets: [{ data: [unsupported, supported], backgroundColor: [colorUnsupported, colorSupported], hoverOffset: 6 }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    cutout: '60%',
                    plugins: {
                        legend: { display: false },
                        tooltip: {
                            enabled: false,
                            external: makeExternalTooltip(total)
                        }
                    }
                }
            });
            // hide tooltip when mouse leaves the canvas
            canvas.addEventListener('mouseleave', function() {
                var el = document.getElementById('cj-gauge-tooltip');
                if (el) el.style.opacity = '0';
            });
            canvas._chartInstance = chart;
        } catch (err) {
            console.error('Failed to init gauge', err);
        }
    });
}

// ---------------------------------------------------------------------------
// initTableEnhancements(container)
// Adds per-table search, column sorting and pagination to all .modern-table
// elements inside [container] that have more than MIN_ROWS data rows.
// PDF/print: @media print CSS forces all rows visible automatically.
// HTML export: buildFullDocumentHTML_fromContainer strips controls and expands rows.
// ---------------------------------------------------------------------------
function initTableEnhancements(container) {
    var MIN_ROWS   = 10;
    var PAGE_SIZES = [10, 25, 50, 100];
    var DEFAULT_PS = 25;

    container.querySelectorAll('.modern-table').forEach(function(table) {
        var tbody = table.querySelector('tbody');
        if (!tbody) return;
        var allRows = Array.from(tbody.querySelectorAll('tr'));
        if (allRows.length <= MIN_ROWS) return;

        // — state —
        var page = 1, ps = DEFAULT_PS, sortCol = -1, sortDir = 1, query = '';
        var filtered = allRows.slice();

        var wrap   = table.closest ? (table.closest('.table-responsive') || table.parentNode) : table.parentNode;
        var parent = wrap.parentNode;
        if (!parent) return;

        // — toolbar —
        var toolbar = document.createElement('div');
        toolbar.className = 'dt-toolbar';
        toolbar.innerHTML =
            '<div class="dt-search">' +
              '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">' +
                '<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>' +
              '</svg>' +
              '<input type="text" class="dt-search-input" placeholder="' + t('placeholder_search') + '">' +
            '</div>' +
            '<div class="dt-controls">' +
              '<span class="dt-info"></span>' +
              '<select class="dt-page-size">' +
                PAGE_SIZES.map(function(s) {
                    return '<option value="' + s + '"' + (s === DEFAULT_PS ? ' selected' : '') + '>' + s + ' ' + t('per_page_suffix') + '</option>';
                }).join('') +
              '</select>' +
            '</div>';
        parent.insertBefore(toolbar, wrap);

        // — pagination bar —
        var pagBar = document.createElement('div');
        pagBar.className = 'dt-pagination';
        parent.insertBefore(pagBar, wrap.nextSibling);

        // — sortable headers —
        var headers = Array.from(table.querySelectorAll('thead th'));
        headers.forEach(function(th, idx) {
            th.classList.add('dt-sortable');
            var icon = document.createElement('span');
            icon.className = 'dt-sort-icon';
            icon.textContent = '\u2195';
            th.appendChild(icon);
            th.addEventListener('click', function() {
                sortDir = (sortCol === idx) ? -sortDir : 1;
                sortCol = idx;
                headers.forEach(function(h) {
                    h.classList.remove('dt-sort-asc', 'dt-sort-desc');
                    var ic = h.querySelector('.dt-sort-icon');
                    if (ic) ic.textContent = '\u2195';
                });
                th.classList.add(sortDir === 1 ? 'dt-sort-asc' : 'dt-sort-desc');
                icon.textContent = sortDir === 1 ? '\u2191' : '\u2193';
                run();
            });
        });

        function cellText(row, col) {
            var cells = row.querySelectorAll('td');
            return cells[col] ? (cells[col].innerText || cells[col].textContent || '').trim() : '';
        }

        function run() {
            filtered = allRows.filter(function(r) {
                if (!query) return true;
                return (r.innerText || r.textContent || '').toLowerCase().indexOf(query) !== -1;
            });
            if (sortCol >= 0) {
                filtered.sort(function(a, b) {
                    var ta = cellText(a, sortCol).toLowerCase();
                    var tb = cellText(b, sortCol).toLowerCase();
                    var na = parseFloat(ta.replace(/[^0-9.-]/g, ''));
                    var nb = parseFloat(tb.replace(/[^0-9.-]/g, ''));
                    if (!isNaN(na) && !isNaN(nb)) return (na - nb) * sortDir;
                    return ta.localeCompare(tb, t('locale_code'), {sensitivity: 'base', numeric: true}) * sortDir;
                });
            }
            page = 1;
            draw();
        }

        function draw() {
            // Ordenacão das tabelas
            filtered.forEach(function(r) { tbody.appendChild(r); });

            var total = filtered.length;
            var pages = Math.max(1, Math.ceil(total / ps));
            page = Math.min(page, pages);
            var start = (page - 1) * ps;
            var end   = Math.min(start + ps, total);

            allRows.forEach(function(r) { r.style.display = 'none'; });
            filtered.slice(start, end).forEach(function(r) { r.style.display = ''; });

            // no-results row
            var noRes = tbody.querySelector('.dt-no-results-row');
            if (total === 0) {
                if (!noRes) {
                    noRes = document.createElement('tr');
                    noRes.className = 'dt-no-results-row';
                    var td = document.createElement('td');
                    td.colSpan = headers.length || 99;
                    td.className = 'dt-no-results';
                    td.textContent = t('no_results');
                    noRes.appendChild(td);
                    tbody.appendChild(noRes);
                }
                noRes.style.display = '';
            } else if (noRes) {
                noRes.style.display = 'none';
            }

            // info text
            var infoEl = toolbar.querySelector('.dt-info');
            if (infoEl) {
                infoEl.textContent = total < allRows.length
                    ? t('table_range_filtered', start + 1, end, total, allRows.length)
                    : t('table_range', start + 1, end, total);
            }

            // pagination controls
            pagBar.innerHTML = '';
            if (pages <= 1) return;

            function mkBtn(lbl, pg, isActive, isDis) {
                var b = document.createElement('button');
                b.className = 'dt-btn' + (isActive ? ' active' : '');
                b.innerHTML = lbl;
                b.disabled = isDis;
                if (!isDis && !isActive) b.addEventListener('click', function() { page = pg; draw(); });
                pagBar.appendChild(b);
            }
            function mkDots() {
                var sp = document.createElement('span');
                sp.className = 'dt-ellipsis';
                sp.textContent = '\u2026';
                pagBar.appendChild(sp);
            }

            mkBtn('&#8249;', page - 1, false, page === 1);
            var delta = 2, rng = [], prev = null;
            for (var i = 1; i <= pages; i++) {
                if (i === 1 || i === pages || (i >= page - delta && i <= page + delta)) rng.push(i);
            }
            rng.forEach(function(pg) {
                if (prev !== null) {
                    if (pg - prev === 2) mkBtn(prev + 1, prev + 1, prev + 1 === page, false);
                    else if (pg - prev > 2) mkDots();
                }
                mkBtn(pg, pg, pg === page, false);
                prev = pg;
            });
            mkBtn('&#8250;', page + 1, false, page === pages);
        }

        // — events —
        var searchInput = toolbar.querySelector('.dt-search-input');
        var debTimer;
        searchInput.addEventListener('input', function(e) {
            clearTimeout(debTimer);
            debTimer = setTimeout(function() {
                query = e.target.value.trim().toLowerCase();
                run();
            }, 220);
        });
        toolbar.querySelector('.dt-page-size').addEventListener('change', function(e) {
            ps = parseInt(e.target.value, 10);
            page = 1;
            draw();
        });

        table.setAttribute('data-dt-enhanced', '1');
        run();
    });
}

function initHostAlertFilters(container) {
    var panel = container.querySelector('#host-focus-panel');
    if (!panel) return;

    var table = panel.querySelector('#host-alerts-table');
    var select = panel.querySelector('#host-alert-type-select');
    var rows = table ? Array.from(table.querySelectorAll('tbody tr')) : [];
    var filterBtns = panel.querySelectorAll('[data-alert-filter]');
    var exportBtn = panel.querySelector('#host-alerts-export-csv');

    function applyFilter(type) {
        var active = type || 'all';
        rows.forEach(function(row) {
            var rowType = row.getAttribute('data-alert-type') || 'other';
            row.style.display = (active === 'all' || rowType === active) ? '' : 'none';
        });
        filterBtns.forEach(function(btn) {
            btn.classList.toggle('is-active', btn.getAttribute('data-alert-filter') === active);
        });
        if (select) select.value = active;
    }

    filterBtns.forEach(function(btn) {
        btn.addEventListener('click', function() {
            applyFilter(btn.getAttribute('data-alert-filter') || 'all');
        });
    });
    if (select) {
        select.addEventListener('change', function() {
            applyFilter(select.value || 'all');
        });
    }

    if (exportBtn && table) {
        exportBtn.addEventListener('click', function() {
            var visible = rows.filter(function(r) { return r.style.display !== 'none'; });
            var lines = ['Data e hora,Duração,Tipo,Descrição'];
            visible.forEach(function(row) {
                var cells = row.querySelectorAll('td');
                if (cells.length < 4) return;
                var vals = [];
                for (var i = 0; i < 4; i++) {
                    var text = (cells[i].innerText || cells[i].textContent || '').trim().replace(/"/g, '""');
                    vals.push('"' + text + '"');
                }
                lines.push(vals.join(','));
            });
            var blob = new Blob(['\ufeff' + lines.join('\n')], { type: 'text/csv;charset=utf-8;' });
            var link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = 'host-alerts.csv';
            link.click();
            URL.revokeObjectURL(link.href);
        });
    }
}

// ---------------------------------------------------------------------------
// Host analysis charts — shared theme for professional, readable visuals
// ---------------------------------------------------------------------------
(function() {
    window.zbxQueueHostChart = window.zbxQueueHostChart || [];
    window._zbx_host_charts_flushed = false;

    function isLightTheme() {
        return document.body.classList.contains('theme-light');
    }

    function chartTheme() {
        if (isLightTheme()) {
            return {
                TEXT: '#64748b',
                GRID: 'rgba(100,116,139,0.18)',
                TOOLTIP_BG: 'rgba(255,255,255,0.98)',
                TOOLTIP_TITLE: '#0f172a',
                TOOLTIP_BODY: '#334155',
                TOOLTIP_BORDER: 'rgba(148,163,184,0.35)',
                LEGEND: '#475569'
            };
        }
        return {
            TEXT: '#94a3b8',
            GRID: 'rgba(148,163,184,0.10)',
            TOOLTIP_BG: 'rgba(15,23,42,0.94)',
            TOOLTIP_TITLE: '#f8fafc',
            TOOLTIP_BODY: '#cbd5e1',
            TOOLTIP_BORDER: 'rgba(148,163,184,0.22)',
            LEGEND: '#cbd5e1'
        };
    }

    function lbl(key, fallback) {
        try { return (typeof t === 'function' && t(key)) || fallback; } catch(e) { return fallback; }
    }

    function tooltipTheme() {
        var theme = chartTheme();
        return {
            backgroundColor: theme.TOOLTIP_BG,
            titleColor: theme.TOOLTIP_TITLE,
            bodyColor: theme.TOOLTIP_BODY,
            borderColor: theme.TOOLTIP_BORDER,
            borderWidth: 1,
            padding: 12,
            cornerRadius: 10,
            displayColors: true,
            boxPadding: 4
        };
    }

    function legendTheme() {
        var theme = chartTheme();
        return {
            display: true,
            position: 'top',
            align: 'end',
            labels: {
                color: theme.LEGEND,
                boxWidth: 10,
                boxHeight: 10,
                padding: 14,
                usePointStyle: true,
                pointStyle: 'rectRounded',
                font: { size: 11, weight: '600' }
            }
        };
    }

    function axisTicks(maxTicks) {
        var theme = chartTheme();
        return {
            color: theme.TEXT,
            maxRotation: 0,
            minRotation: 0,
            autoSkip: true,
            maxTicksLimit: maxTicks || 7,
            font: { size: 11 }
        };
    }

    var metricStyles = {
        cpu:    { line: '#60a5fa', fill: 'rgba(96,165,250,0.14)',  marker: '#f87171' },
        memory: { line: '#a78bfa', fill: 'rgba(167,139,250,0.14)', marker: '#fb923c' },
        net:    { line: '#34d399', fill: 'rgba(52,211,153,0.14)',  marker: '#f472b6' }
    };

    window.zbxResizeChartsIn = function(container) {
        if (!container) return;
        container.querySelectorAll('canvas').forEach(function(canvas) {
            if (canvas._chartInstance && typeof canvas._chartInstance.resize === 'function') {
                try { canvas._chartInstance.resize(); } catch(e) {}
            }
        });
    };

    window.zbxFlushHostCharts = function(container) {
        if (!container || typeof Chart === 'undefined') return;
        var queue = window.zbxQueueHostChart || [];
        if (!queue.length) return;
        window.zbxQueueHostChart = [];
        queue.forEach(function(cfg) {
            if (!cfg || !cfg.id) return;
            if (cfg.type === 'alert') {
                window.zbxInitHostAlertChart(cfg.id, cfg.labels, cfg.problems, cfg.resolved);
            } else if (cfg.type === 'metric') {
                window.zbxInitHostMetricChart(cfg.id, cfg.metric, cfg.labels, cfg.values, cfg.timestamps, cfg.alerts);
            }
        });
        window._zbx_host_charts_flushed = true;
        setTimeout(function() { window.zbxResizeChartsIn(container); }, 20);
    };

    window.zbxInitHostAlertChart = function(canvasId, labels, problems, resolved) {
        var canvas = document.getElementById(canvasId);
        if (!canvas || typeof Chart === 'undefined') return;
        if (canvas._chartInstance) { try { canvas._chartInstance.destroy(); } catch(e) {} }

        var theme = chartTheme();
        var barCount = labels ? labels.length : 0;
        var categoryPct = barCount <= 6 ? 0.92 : 0.82;
        var barPct = barCount <= 6 ? 0.92 : 0.85;

        canvas._chartInstance = new Chart(canvas.getContext('2d'), {
            type: 'bar',
            data: {
                labels: labels,
                datasets: [
                    {
                        label: lbl('host_focus.problems', 'Problemas'),
                        data: problems,
                        backgroundColor: 'rgba(251,146,60,0.88)',
                        hoverBackgroundColor: 'rgba(251,146,60,1)',
                        borderRadius: 8,
                        borderSkipped: false,
                        categoryPercentage: categoryPct,
                        barPercentage: barPct
                    },
                    {
                        label: lbl('host_focus.resolved', 'Resolvidos'),
                        data: resolved,
                        backgroundColor: 'rgba(52,211,153,0.88)',
                        hoverBackgroundColor: 'rgba(52,211,153,1)',
                        borderRadius: 8,
                        borderSkipped: false,
                        categoryPercentage: categoryPct,
                        barPercentage: barPct
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                layout: { padding: { top: 4, right: 8, bottom: 0, left: 4 } },
                plugins: {
                    legend: legendTheme(),
                    tooltip: tooltipTheme()
                },
                scales: {
                    x: {
                        offset: true,
                        grid: { display: false, drawBorder: false },
                        ticks: axisTicks(12)
                    },
                    y: {
                        beginAtZero: true,
                        ticks: { color: theme.TEXT, precision: 0, font: { size: 11 } },
                        grid: { color: theme.GRID, drawBorder: false },
                        title: {
                            display: true,
                            text: lbl('chart.events_count', 'Quantidade de eventos'),
                            color: theme.TEXT,
                            font: { size: 11, weight: '500' }
                        }
                    }
                }
            }
        });

        window._zbx_charts = window._zbx_charts || {};
        window._zbx_charts[canvasId] = canvas._chartInstance;

        requestAnimationFrame(function() {
            try { canvas._chartInstance.resize(); } catch(e) {}
        });
    };

    window.zbxInitHostMetricChart = function(canvasId, metricKey, labels, data, timestamps, alerts) {
        var canvas = document.getElementById(canvasId);
        if (!canvas || typeof Chart === 'undefined') return;
        if (canvas._chartInstance) { try { canvas._chartInstance.destroy(); } catch(e) {} }

        var style = metricStyles[metricKey] || metricStyles.cpu;
        var theme = chartTheme();
        var metricLabel = lbl('chart.metric.' + metricKey, metricKey);
        var alertLabel = lbl('chart.alert_markers', 'Alertas');

        var alertMarkers = new Array(labels.length).fill(null);
        var alertLookup = new Array(labels.length).fill(null);
        if (alerts && alerts.length > 0 && timestamps && timestamps.length > 0) {
            for (var a = 0; a < alerts.length; a++) {
                var clk = alerts[a].Clock || alerts[a].clock || 0;
                var bestIdx = -1, bestDiff = Infinity;
                for (var j = 0; j < timestamps.length; j++) {
                    var diff = Math.abs(timestamps[j] - clk);
                    if (diff < bestDiff) { bestDiff = diff; bestIdx = j; }
                }
                if (bestIdx >= 0 && data[bestIdx] !== null && data[bestIdx] !== undefined) {
                    alertMarkers[bestIdx] = data[bestIdx];
                    alertLookup[bestIdx] = alerts[a];
                }
            }
        }

        var datasets = [{
            label: metricLabel,
            data: data,
            borderColor: style.line,
            backgroundColor: style.fill,
            fill: true,
            tension: 0.35,
            pointRadius: 0,
            pointHoverRadius: 4,
            borderWidth: 2.5,
            spanGaps: true
        }];

        var hasAlerts = alertMarkers.some(function(v) { return v !== null; });
        if (hasAlerts) {
            datasets.push({
                label: alertLabel,
                data: alertMarkers,
                type: 'scatter',
                showLine: false,
                pointStyle: 'circle',
                pointRadius: 5,
                pointHoverRadius: 7,
                backgroundColor: style.marker,
                borderColor: '#fff',
                borderWidth: 1.5
            });
            window._zbx_event_lookup = window._zbx_event_lookup || {};
            window._zbx_event_lookup[canvasId] = alertLookup;
        }

        canvas._chartInstance = new Chart(canvas.getContext('2d'), {
            type: 'line',
            data: { labels: labels, datasets: datasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'nearest', intersect: false },
                layout: { padding: { top: 4, right: 8, bottom: 0, left: 4 } },
                plugins: {
                    legend: legendTheme(),
                    tooltip: Object.assign({}, tooltipTheme(), {
                        callbacks: {
                            label: function(context) {
                                try {
                                    if (context.dataset.label === alertLabel) {
                                        var ev = (window._zbx_event_lookup && window._zbx_event_lookup[canvasId] && window._zbx_event_lookup[canvasId][context.dataIndex]) || null;
                                        if (ev) {
                                            var name = ev.Name || ev.name || lbl('chart.alert_unknown', 'Alerta');
                                            var tclk = ev.Clock || ev.clock || 0;
                                            return name + ' · ' + new Date(tclk * 1000).toLocaleString();
                                        }
                                    }
                                } catch(e) {}
                                return context.dataset.label + ': ' + context.formattedValue;
                            }
                        }
                    })
                },
                scales: {
                    x: {
                        grid: { display: false },
                        ticks: axisTicks(6)
                    },
                    y: {
                        beginAtZero: false,
                        ticks: { color: theme.TEXT, font: { size: 11 } },
                        grid: { color: theme.GRID, drawBorder: false }
                    }
                }
            }
        });

        window._zbx_charts = window._zbx_charts || {};
        window._zbx_charts[canvasId] = canvas._chartInstance;

        requestAnimationFrame(function() {
            try { canvas._chartInstance.resize(); } catch(e) {}
        });
    };

    if (!window.zbx_downloadChart) {
        window.zbx_downloadChart = function(id, filename) {
            window._zbx_charts = window._zbx_charts || {};
            var c = window._zbx_charts[id];
            if (!c) return;
            var url = c.toBase64Image();
            var a = document.createElement('a');
            a.href = url;
            a.download = filename || (id + '.png');
            document.body.appendChild(a);
            a.click();
            a.remove();
        };
    }

    window.zbx_downloadAllCharts = function(prefix) {
        window._zbx_charts = window._zbx_charts || {};
        Object.keys(window._zbx_charts).forEach(function(id) {
            if (!prefix || id.indexOf(prefix) === 0) {
                window.zbx_downloadChart(id, id + '.png');
            }
        });
    };
})();
