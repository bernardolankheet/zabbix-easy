if (typeof mermaid !== 'undefined') {
  try {
    mermaid.initialize({ startOnLoad: true });
  } catch (e) { console.warn('mermaid init failed', e); }
}
