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

// --- AJAX para submit, progress, report ---
document.getElementById('zabbix-form').addEventListener('submit', function(e) {
    e.preventDefault();
    document.getElementById('report-area').style.display = 'none';
    document.getElementById('progress-bar').style.display = 'block';
    document.querySelector('.progress').style.width = '0%';
    document.getElementById('progress-text').textContent = 'Gerando relat\u00f3rio...';

    var url = document.getElementById('zabbix_url').value;
    var token = document.getElementById('zabbix_token').value;

    fetch('/api/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ zabbix_url: url, zabbix_token: token })
    })
    .then(res => res.json())
    .then(data => {
        if (data.task_id) {
            checkProgress(data.task_id, 0);
        } else {
            document.getElementById('progress-bar').style.display = 'none';
            document.getElementById('report-area').innerHTML = '<div style="color:red;">Erro ao iniciar tarefa.</div>';
            document.getElementById('report-area').style.display = 'block';
        }
    });
});

// ---------------------------------------------------------------------------
// renderReport(html, titleHint)
// Shared function used by both the live generation flow and the DB load flow.
// Assembles the full report layout (header + export/print buttons + content)
// into #report-area and wires all button event listeners.
// ---------------------------------------------------------------------------
function renderReport(html, titleHint, createdAt) {
    const reportArea = document.getElementById('report-area');
    reportArea.style.display = 'block';

    const container = document.createElement('div');
    container.className = 'report-layout';

    const header = document.createElement('div');
    header.className = 'report-frame-header';
    const geradoEm = createdAt ? new Date(createdAt).toLocaleString() : new Date().toLocaleString();
    header.innerHTML = `
        <div class="frame-meta">
            <div class="frame-title">Relat\u00f3rio Zabbix</div>
            <div class="frame-sub">Gerado em: ${geradoEm}</div>
        </div>`;

    const left = document.createElement('div');
    left.className = 'report-main';
    left.innerHTML = html;

    const right = document.createElement('aside');
    right.className = 'report-side';

    // build action buttons reusing existing markup / CSS classes
    const actionGroup = document.createElement('div');
    actionGroup.className = 'action-group';
    actionGroup.innerHTML = `
        <button class="btn small icon-btn" data-action="export" aria-label="Exportar HTML" title="Exportar HTML">
            <svg width="16" height="16" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none">
                <path d="M6 2h7l4 4v12a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1z" stroke="#fff" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round"/>
                <path d="M13 2v5h5" stroke="#fff" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round"/>
                <text x="7.5" y="15.2" font-size="5.2" font-family="Arial, sans-serif" fill="#fff">HTML</text>
            </svg>
        </button>
        <button class="btn small icon-btn" data-action="print" aria-label="Gerar PDF" title="Gerar PDF">
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
        if (btns[0]) btns[0].id = 'btn-export-html';
        if (btns[1]) btns[1].id = 'btn-print';
    } catch(e) {}

    // assemble container
    container.appendChild(header);
    const cols = document.createElement('div');
    cols.className = 'report-layout-cols';
    cols.appendChild(left);
    if (right.children && right.children.length > 0) {
        cols.appendChild(right);
    }
    container.appendChild(cols);

    reportArea.innerHTML = '';
    reportArea.appendChild(container);

    // hide the input form when report is displayed
    const form = document.getElementById('zabbix-form');
    if (form) form.style.display = 'none';
    try { document.body.classList.remove('show-login'); } catch(e) {}

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
        const head = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${title || 'Relat\u00f3rio Zabbix'}</title>` +
            (cssText ? `<style>${cssText}</style>` : '') + `</head><body>`;
        const chartsInit = `<script src="https://cdn.jsdelivr.net/npm/chart.js"></` + `script>` +
            `<script>window.addEventListener('load',function(){try{` +
            `Array.from(document.querySelectorAll('canvas[data-total]')).forEach(function(canvas){try{` +
            `var tot=parseInt(canvas.getAttribute('data-total'))||0,` +
            `uns=parseInt(canvas.getAttribute('data-unsupported'))||0,` +
            `sup=Math.max(tot-uns,0);` +
            `new Chart(canvas.getContext('2d'),{type:'doughnut',data:{labels:[canvas.getAttribute('data-unsupported-label')||'N\u00e3o Suportados',canvas.getAttribute('data-supported-label')||'Suportados'],datasets:[{data:[uns,sup],backgroundColor:[canvas.getAttribute('data-color-unsupported')||'#ff7a7a',canvas.getAttribute('data-color-supported')||'#66c2a5']}]},options:{responsive:true,maintainAspectRatio:false,cutout:'60%',plugins:{legend:{display:false}}}});` +
            `}catch(e){}});` +
            `}catch(e){}});</` + `script>`;
        return head + bodyInner + chartsInit + `</body></html>`;
    }

    // determine filename-safe title (prefer extracted Ambiente, fall back to titleHint)
    const ambienteName = extractAmbienteName(left) || titleHint || 'Ambiente';
    const ambienteSafe = ('' + ambienteName).replace(/[^0-9A-Za-z-_\. ]+/g, '_').slice(0, 80);
    const documentTitleEscaped = (`Relat\u00f3rio Zabbix - ${ambienteName}`).replace(/"/g, '');

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
        } catch(err) { alert('Erro ao imprimir: ' + err); }
    });

    // wire Export HTML button
    const btnExport = document.getElementById('btn-export-html');
    if (btnExport) btnExport.addEventListener('click', function() {
        try {
            fetch('/static/style.css').then(r => r.text()).catch(() => '').then(cssText => {
                const fullHtml = buildFullDocumentHTML_fromContainer(container, cssText, documentTitleEscaped);
                const blob = new Blob([fullHtml], { type: 'text/html' });
                const url  = URL.createObjectURL(blob);
                const a    = document.createElement('a');
                a.href     = url;
                a.download = `Relat\u00f3rio Zabbix - ${ambienteSafe}.html`;
                document.body.appendChild(a);
                a.click();
                a.remove();
                URL.revokeObjectURL(url);
            });
        } catch(err) { alert('Erro ao exportar: ' + err); }
    });

    // keyboard accessibility for both buttons
    ['btn-print', 'btn-export-html'].forEach(id => {
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
            if (data.progress_msg) {
                document.getElementById('progress-text').textContent = data.progress_msg;
            }
            if (data.status === 'done') {
                document.querySelector('.progress').style.width = '100%';
                fetch('/api/report/' + taskId)
                    .then(res => res.text())
                    .then(html => {
                        document.getElementById('progress-bar').style.display = 'none';
                        renderReport(html, '');
                    });
            } else if (data.status === 'processing') {
                progress = Math.min(progress + 20, 90);
                document.querySelector('.progress').style.width = progress + '%';
                setTimeout(function() { checkProgress(taskId, progress); }, 800);
            } else if (data.status === 'error') {
                document.getElementById('progress-bar').style.display = 'none';
                document.getElementById('report-area').innerHTML = data.report || '<div style="color:red;">Erro ao processar tarefa.</div>';
                document.getElementById('report-area').style.display = 'block';
            } else {
                document.getElementById('progress-bar').style.display = 'none';
                document.getElementById('report-area').innerHTML = '<div style="color:red;">Erro ao processar tarefa.</div>';
                document.getElementById('report-area').style.display = 'block';
            }
        });
}

// Load list of reports from server DB and populate selector
document.getElementById('btn-load-db').addEventListener('click', function() {
    fetch('/api/reports')
        .then(res => res.json())
        .then(data => {
            const sel = document.getElementById('reportSelect');
            sel.innerHTML = '<option value="">-- selecione --</option>';
            if (data && data.reports) {
                data.reports.forEach(r => {
                    const opt = document.createElement('option');
                    opt.value = r.id;
                    opt.dataset.createdAt = r.created_at || '';
                    const d = new Date(r.created_at);
                    const label = (r.zabbix_url || r.name || ('Relatório ' + r.id))
                        .replace(/^https?:\/\//, '')  // remove http:// ou https://
                        .replace(/\/$/, '');           // remove trailing slash
                    opt.text = label + ' \u2014 ' + d.toLocaleString();
                    sel.appendChild(opt);
                });
            }
        }).catch(err => { alert('Erro ao carregar lista: ' + err); });
});

// Load selected report from DB and render inline (same layout + export/print buttons)
document.getElementById('btn-open-db').addEventListener('click', function() {
    const sel = document.getElementById('reportSelect');
    if (!sel) return alert('Nenhum seletor encontrado');
    const id = sel.value;
    if (!id) return alert('Selecione um relat\u00f3rio');
    // ?raw=1 causes Go handler to return only the HTML fragment so renderReport can assemble the layout
    fetch('/api/reportdb/' + id + '?raw=1')
        .then(res => {
            if (!res.ok) throw new Error('Relat\u00f3rio n\u00e3o encontrado');
            return res.text();
        })
        .then(html => {
            // pass option label as titleHint fallback for filenames
            const selOpt = sel.options[sel.selectedIndex];
            const optText = selOpt ? selOpt.text : '';
            const createdAt = selOpt ? selOpt.dataset.createdAt : '';
            renderReport(html, optText, createdAt);
        })
        .catch(err => alert('Erro ao abrir relat\u00f3rio: ' + err));
});

// Initialize doughnut gauges inside a given container
function initGauges(container) {
    if (typeof Chart === 'undefined') return; // Chart.js not loaded
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
            const unsupportedLabel = canvas.getAttribute('data-unsupported-label') || 'N\u00e3o Suportados';
            const supportedLabel = canvas.getAttribute('data-supported-label') || 'Suportados';
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
                        tooltip: { callbacks: { label: function(ctx) { let v = ctx.parsed; let p = total > 0 ? ((v / total) * 100).toFixed(2) : '0.00'; return ctx.label + ': ' + v + ' (' + p + '%)'; } } }
                    }
                }
            });
            canvas._chartInstance = chart;
        } catch (err) {
            console.error('Failed to init gauge', err);
        }
    });
}
