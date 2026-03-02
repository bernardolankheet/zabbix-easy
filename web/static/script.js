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
        <button class="btn small icon-btn" data-action="new-report" aria-label="Novo Relatório" title="Novo Relatório">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <polyline points="14 2 14 8 20 8" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <line x1="12" y1="11" x2="12" y2="17" stroke="#fff" stroke-width="2" stroke-linecap="round"/>
                <line x1="9" y1="14" x2="15" y2="14" stroke="#fff" stroke-width="2" stroke-linecap="round"/>
            </svg>
        </button>
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
        if (btns[0]) btns[0].id = 'btn-new-report';
        if (btns[1]) btns[1].id = 'btn-export-html';
        if (btns[2]) btns[2].id = 'btn-print';
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
    // initialize table search / sort / pagination
    initTableEnhancements(left);

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
        const head = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${title || 'Relat\u00f3rio Zabbix'}</title>` +
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
            `new Chart(canvas.getContext('2d'),{type:'doughnut',data:{labels:[canvas.getAttribute('data-unsupported-label')||'N\u00e3o Suportados',canvas.getAttribute('data-supported-label')||'Suportados'],datasets:[{data:[uns,sup],backgroundColor:[canvas.getAttribute('data-color-unsupported')||'#ff7a7a',canvas.getAttribute('data-color-supported')||'#66c2a5']}]},options:{responsive:true,maintainAspectRatio:false,cutout:'60%',plugins:{legend:{display:false},tooltip:{enabled:false,external:extTT}}}});` +
            `canvas.addEventListener('mouseleave',function(){var el=document.getElementById('cj-gauge-tooltip')||ttEl;if(el)el.style.opacity='0';});` +
            `}catch(e){}});` +
            `}catch(e){}});</` + `script>`;
        return head + bodyInner + chartsInit + `</body></html>`;
    }

    // determine filename-safe title (prefer extracted Ambiente, fall back to titleHint)
    const ambienteName = extractAmbienteName(left) || titleHint || 'Ambiente';
    const ambienteSafe = ('' + ambienteName).replace(/[^0-9A-Za-z-_\. ]+/g, '_').slice(0, 80);
    const documentTitleEscaped = (`Relat\u00f3rio Zabbix - ${ambienteName}`).replace(/"/g, '');

    // wire Novo Relatório button — returns to the generation form
    const btnNewReport = document.getElementById('btn-new-report');
    if (btnNewReport) btnNewReport.addEventListener('click', function() {
        const ra = document.getElementById('report-area');
        if (ra) { ra.style.display = 'none'; ra.innerHTML = ''; }
        const pb = document.getElementById('progress-bar');
        if (pb) pb.style.display = 'none';
        const form = document.getElementById('zabbix-form');
        if (form) form.style.display = '';
        try { document.body.classList.add('show-login'); } catch(e) {}
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

// Helper: reload the reports list into the selector
function reloadReportList() {
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
                    const label = (r.zabbix_url || r.name || ('Relat\u00f3rio ' + r.id))
                        .replace(/^https?:\/\//, '')
                        .replace(/\/$/, '');
                    opt.text = label + ' \u2014 ' + d.toLocaleString();
                    sel.appendChild(opt);
                });
            }
        }).catch(err => { alert('Erro ao recarregar lista: ' + err); });
}

// Delete selected report
document.getElementById('btn-delete-db').addEventListener('click', function() {
    const sel = document.getElementById('reportSelect');
    const id = sel ? sel.value : '';
    if (!id) return alert('Selecione um relat\u00f3rio para excluir.');
    const label = sel.options[sel.selectedIndex] ? sel.options[sel.selectedIndex].text : id;
    if (!confirm('Excluir o relat\u00f3rio "' + label + '"?\nEssa a\u00e7\u00e3o n\u00e3o pode ser desfeita.')) return;
    fetch('/api/reportdb/' + id, { method: 'DELETE' })
        .then(res => res.json())
        .then(data => {
            if (data.error) { alert('Erro: ' + data.error); return; }
            reloadReportList();
        })
        .catch(err => alert('Erro ao excluir: ' + err));
});

// Delete all reports
document.getElementById('btn-delete-all-db').addEventListener('click', function() {
    if (!confirm('Excluir TODOS os relat\u00f3rios do banco?\nEssa a\u00e7\u00e3o n\u00e3o pode ser desfeita.')) return;
    fetch('/api/reports', { method: 'DELETE' })
        .then(res => res.json())
        .then(data => {
            if (data.error) { alert('Erro: ' + data.error); return; }
            const n = data.deleted !== undefined ? data.deleted : '?';
            reloadReportList();
            alert(n + ' relat\u00f3rio(s) exclu\u00eddo(s).');
        })
        .catch(err => alert('Erro ao excluir: ' + err));
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
              '<input type="text" class="dt-search-input" placeholder="Buscar na tabela...">' +
            '</div>' +
            '<div class="dt-controls">' +
              '<span class="dt-info"></span>' +
              '<select class="dt-page-size">' +
                PAGE_SIZES.map(function(s) {
                    return '<option value="' + s + '"' + (s === DEFAULT_PS ? ' selected' : '') + '>' + s + ' / p\u00e1g</option>';
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
                    return ta.localeCompare(tb, 'pt-BR', {sensitivity: 'base', numeric: true}) * sortDir;
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
                    td.textContent = 'Nenhum resultado encontrado.';
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
                    ? (start + 1) + '\u2013' + end + ' de ' + total + ' (filtrado de ' + allRows.length + ')'
                    : (start + 1) + '\u2013' + end + ' de ' + total;
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
