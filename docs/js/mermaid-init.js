(function () {
  function initMermaid() {
    // MkDocs renders ```mermaid blocks as <pre><code class="language-mermaid">.
    // Convert them to <pre class="mermaid"> which mermaid.js v10 recognises.
    document.querySelectorAll('pre > code.language-mermaid').forEach(function (code) {
      var pre = code.parentElement;
      var diagramSource = code.textContent;
      pre.classList.add('mermaid');
      pre.replaceChild(document.createTextNode(diagramSource), code);
    });
    if (typeof mermaid === 'undefined') return;
    try {
      mermaid.initialize({ startOnLoad: false });
      if (typeof mermaid.run === 'function') {
        mermaid.run();
      } else {
        mermaid.init(undefined, document.querySelectorAll('pre.mermaid'));
      }
    } catch (e) { console.warn('mermaid init failed', e); }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMermaid);
  } else {
    initMermaid();
  }
})();
