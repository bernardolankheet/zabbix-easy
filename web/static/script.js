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
    document.getElementById('progress-text').textContent = 'Gerando relatório...';

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
                        const reportArea = document.getElementById('report-area');
                        reportArea.style.display = 'block';
                        // Build a modern two-column layout: left = report content, right = recommendations
                        const container = document.createElement('div');
                        container.className = 'report-layout';

                        const header = document.createElement('div');
                        header.className = 'report-frame-header';
                        header.innerHTML = `
                            <div class="frame-meta">
                                <div class="frame-title">Relatório Zabbix</div>
                                <div class="frame-sub">Gerado em: ${new Date().toLocaleString()}</div>
                            </div>`;

                        const left = document.createElement('div');
                        left.className = 'report-main';
                        left.innerHTML = html; // original report HTML injected here

                                                const right = document.createElement('aside');
                                                right.className = 'report-side';
                                                // create action group so buttons can be placed in the page header or right column
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
                                                // Prefer placing action group inside the report frame header so it scrolls with the tabs
                                                // Insert the action group into the report frame header (right side)
                                                if (header) {
                                                    let frameActions = header.querySelector('.frame-actions');
                                                    if (!frameActions) {
                                                        frameActions = document.createElement('div');
                                                        frameActions.className = 'frame-actions';
                                                        header.appendChild(frameActions);
                                                    }
                                                    const inserted = actionGroup.cloneNode(true);
                                                    frameActions.appendChild(inserted);
                                                    try{
                                                        const btns = inserted.querySelectorAll('button');
                                                        if (btns[0]) btns[0].id = 'btn-export-html';
                                                        if (btns[1]) btns[1].id = 'btn-print';
                                                    }catch(e){}
                                                } else {
                                                    // fallback: place in right column
                                                    right.appendChild(actionGroup);
                                                }
                        // Only use the sidebar for auxiliary content, not for main tab content.
                        // If there is a recommendations tab, do NOT move it to the sidebar; let it render in the main area.
                        // If you want to show a summary or auxiliary info in the sidebar, add it here (currently none).
                        // This ensures all tab content, including recommendations, appears in .report-main.

                        // assemble container; only include right column if it has content
                        container.appendChild(header);
                        const cols = document.createElement('div');
                        cols.className = 'report-layout-cols';
                        cols.appendChild(left);
                        // append right only if it has meaningful children (auxiliary panels)
                        if (right.children && right.children.length > 0) {
                            cols.appendChild(right);
                        }
                        container.appendChild(cols);

                        // replace report area content
                        reportArea.innerHTML = '';
                        reportArea.appendChild(container);

                        // hide the input form (url/token) when report is displayed
                        const form = document.getElementById('zabbix-form');
                        if (form) form.style.display = 'none';

                        // Execute any scripts included in the inserted HTML (fallback) and initialize gauges
                        (function executeInsertedScripts(container){
                            const scripts = Array.from(container.querySelectorAll('script'));
                            scripts.forEach(oldScript => {
                                const newScript = document.createElement('script');
                                if (oldScript.src) {
                                    newScript.src = oldScript.src;
                                    newScript.async = false;
                                } else {
                                    newScript.text = oldScript.textContent;
                                }
                                oldScript.parentNode.replaceChild(newScript, oldScript);
                            });
                        })(left);

                        // Initialize any gauges (canvases with data-total/data-unsupported) inside the left report area
                        initGauges(left);

                        // helper: attempt to extract the Ambiente name (prefer domain) from the injected report HTML
                        function extractAmbienteName(leftEl){
                            try{
                                const text = (leftEl.innerText || leftEl.textContent || '').replace(/\u00A0/g,' ');
                                const m = text.match(/Ambiente:\s*([^\r\n]+)/i);
                                if (m && m[1]){
                                    let v = m[1].trim();
                                    // if contains 'Vers' or next headings, cut at those words
                                    v = v.split(/Versã|Versao|VersÃo|Vers\.|Vers\:|\sResumo|\sProcessos|\sTop|\sItems|\sTemplates/i)[0].trim();
                                    // remove protocol and trailing slashes
                                    v = v.replace(/^https?:\/\//i,'').replace(/\/$/, '');
                                    // keep only first token (domain) to avoid long filenames
                                    v = v.split(/\s+/)[0];
                                    return v;
                                }
                            }catch(e){}
                            return '';
                        }

                        // (frame-version removed) no version extraction needed

                        // wire up Print and Export buttons to operate on ALL tabs (full report)
                        // Build a full HTML document string from the assembled container element and CSS text
                        function buildFullDocumentHTML_fromContainer(containerEl, cssText, title){
                            const clone = containerEl.cloneNode(true);
                            // remove any UI controls from the cloned document so exports don't include buttons
                            ['.action-group', '.frame-actions', '#btn-print', '#btn-export-html'].forEach(sel => {
                                clone.querySelectorAll(sel).forEach(n => n.parentNode && n.parentNode.removeChild(n));
                            });

                            // reveal all tab panels and remove active classes
                            clone.querySelectorAll('.tab-panel').forEach(p => { p.style.display = 'block'; });
                            clone.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));

                            // if right column exists, move its meaningful content into the main column for the exported document
                            const right = clone.querySelector('.report-side');
                            const main = clone.querySelector('.report-main');
                            if (right && main){
                                // move all children except any remaining action controls into main, preserving order
                                const movable = Array.from(right.children).filter(ch => !(ch.classList && ch.classList.contains('action-group')) && !ch.classList.contains('frame-actions'));
                                movable.forEach(ch => {
                                    try{ main.appendChild(ch.cloneNode(true)); }catch(e){}
                                });
                                // remove the right column entirely so it doesn't reserve layout space
                                right.parentNode && right.parentNode.removeChild(right);
                            }

                            // ensure the root container's structure is serialized
                            const bodyInner = clone.outerHTML;

                            const head = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${(title||'Relatório Zabbix')}</title>` + (cssText?`<style>${cssText}</style>`:'') + `</head><body>`;

                            const chartsInit = `
                                <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
                                <script>
                                window.addEventListener('load', function(){
                                    try{
                                        const canvases = Array.from(document.querySelectorAll('canvas[data-total]'));
                                        canvases.forEach(function(canvas){
                                            try{
                                                const total = parseInt(canvas.getAttribute('data-total')) || 0;
                                                const unsupported = parseInt(canvas.getAttribute('data-unsupported')) || 0;
                                                const supported = Math.max(total - unsupported, 0);
                                                const ctx = canvas.getContext('2d');
                                                const unsupportedLabel = canvas.getAttribute('data-unsupported-label') || 'Não Suportados';
                                                const supportedLabel = canvas.getAttribute('data-supported-label') || 'Suportados';
                                                const colorUnsupported = canvas.getAttribute('data-color-unsupported') || '#ff7a7a';
                                                const colorSupported = canvas.getAttribute('data-color-supported') || '#66c2a5';
                                                new Chart(ctx, {
                                                    type: 'doughnut',
                                                    data: { labels: [unsupportedLabel, supportedLabel], datasets: [{ data: [unsupported, supported], backgroundColor: [colorUnsupported, colorSupported] }] },
                                                    options: { responsive:true, maintainAspectRatio:false, cutout:'60%', plugins:{legend:{display:false}} }
                                                });
                                            }catch(e){console&&console.error(e);} 
                                        });
                                    }catch(e){console&&console.error(e);} 
                                });
                                </script>`;

                            const foot = `</body></html>`;
                            return head + bodyInner + chartsInit + foot;
                        }

                        // decide document title and sanitized filename based on Ambiente name
                        const ambienteName = extractAmbienteName(left) || 'Ambiente';
                        const ambienteSafe = ('' + ambienteName).replace(/[^0-9A-Za-z-_\. ]+/g, '_').slice(0,80);
                        const docTitle = `Relatório Zabbix - ${ambienteName}`;
                        // pass sanitized title into builder by creating a small closure variable used by builder
                        const documentTitleEscaped = docTitle.replace(/"/g,'');

                        const btnPrint = document.getElementById('btn-print');
                        if (btnPrint) btnPrint.addEventListener('click', function(){
                            try{
                                // Print in-place: reveal all panels, hide action controls, move right-column content inline,
                                // call window.print(), then restore everything.
                                const panels = Array.from(document.querySelectorAll('.tab-panel'));
                                const prevDisplays = panels.map(p => p.style.display);
                                panels.forEach(p => p.style.display = 'block');

                                const tabBtns = Array.from(document.querySelectorAll('.tab-btn'));
                                const prevActive = tabBtns.map(b => b.classList.contains('active'));
                                tabBtns.forEach(b => b.classList.remove('active'));

                                const actionEls = Array.from(document.querySelectorAll('.action-group, .frame-actions'));
                                const prevActionDisplay = actionEls.map(el => el.style.display || '');
                                actionEls.forEach(el => el.style.display = 'none');

                                // Move right column content into main temporarily to avoid page cutoff
                                const right = document.querySelector('.report-side');
                                const main = document.querySelector('.report-main');
                                let movedClones = [];
                                let rightWasHidden = false;
                                if (right && main) {
                                    const children = Array.from(right.children).filter(ch => !(ch.classList && ch.classList.contains('action-group')) && !ch.classList.contains('frame-actions'));
                                    children.forEach(ch => {
                                        try{
                                            const clone = ch.cloneNode(true);
                                            main.appendChild(clone);
                                            movedClones.push(clone);
                                        }catch(e){}
                                    });
                                    // hide original right column to avoid layout reservation
                                    if (right.style.display !== 'none') { rightWasHidden = true; right.style.display = 'none'; }
                                }

                                const restore = function(){
                                    try{
                                        panels.forEach((p,i) => p.style.display = prevDisplays[i] || '');
                                        tabBtns.forEach((b,i) => { if (prevActive[i]) b.classList.add('active'); else b.classList.remove('active'); });
                                        actionEls.forEach((el,i) => el.style.display = prevActionDisplay[i] || '');
                                        // remove moved clones
                                        movedClones.forEach(c => c.parentNode && c.parentNode.removeChild(c));
                                        if (right && rightWasHidden) right.style.display = '';
                                    }catch(e){}
                                    window.removeEventListener('afterprint', restore);
                                };

                                window.addEventListener('afterprint', restore);
                                // fallback restore in case afterprint doesn't fire
                                setTimeout(restore, 2000);
                                window.print();
                            }catch(err){ alert('Erro ao imprimir: ' + err); }
                        });

                        const btnExport = document.getElementById('btn-export-html');
                        if (btnExport) btnExport.addEventListener('click', function(){
                            try {
                                fetch('/static/style.css').then(r => r.text()).catch(()=>'').then(cssText => {
                                    const fullHtml = buildFullDocumentHTML_fromContainer(container, cssText, documentTitleEscaped);
                                    const blob = new Blob([fullHtml], {type: 'text/html'});
                                    const url = URL.createObjectURL(blob);
                                    const a = document.createElement('a');
                                    a.href = url;
                                    a.download = `Relatório Zabbix - ${ambienteSafe}.html`;
                                    document.body.appendChild(a);
                                    a.click();
                                    a.remove();
                                    URL.revokeObjectURL(url);
                                });
                            } catch (err) { alert('Erro ao exportar: ' + err); }
                        });

                        // Improve keyboard accessibility: allow Enter on icon buttons
                        [ 'btn-print', 'btn-export-html' ].forEach(id => {
                            const el = document.getElementById(id);
                            if (!el) return;
                            el.setAttribute('tabindex', '0');
                            el.addEventListener('keydown', function(e){ if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); this.click(); } });
                        });
                    });
            } else if (data.status === 'processing') {
                progress = Math.min(progress + 20, 90);
                document.querySelector('.progress').style.width = progress + '%';
                setTimeout(function() { checkProgress(taskId, progress); }, 800);
            } else if (data.status === 'error') {
                document.getElementById('progress-bar').style.display = 'none';
                // if backend provided a report (HTML), show it (e.g. Token Invalido), otherwise fallback
                document.getElementById('report-area').innerHTML = data.report || '<div style="color:red;">Erro ao processar tarefa.</div>';
                document.getElementById('report-area').style.display = 'block';
            } else {
                document.getElementById('progress-bar').style.display = 'none';
                document.getElementById('report-area').innerHTML = '<div style="color:red;">Erro ao processar tarefa.</div>';
                document.getElementById('report-area').style.display = 'block';
            }
        });
}

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
            // avoid double-init: destroy existing chart instance stored on canvas
            if (canvas._chartInstance) {
                try { canvas._chartInstance.destroy(); } catch(e){}
            }
            // labels can be customized via data attributes
            const unsupportedLabel = canvas.getAttribute('data-unsupported-label') || 'Não Suportados';
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
                        tooltip: { callbacks: { label: function(ctx){ let v = ctx.parsed; let p = total>0? Math.round((v/total)*100):0; return ctx.label + ': ' + v + ' (' + p + '%)'; } } }
                    }
                }
            });
            canvas._chartInstance = chart;
        } catch (err) {
            console.error('Failed to init gauge', err);
        }
    });
}
