document.addEventListener('DOMContentLoaded', function () {
  try {
    var path = window.location.pathname || '/';
    var isEn = path.indexOf('/en/') !== -1;

    var wrapper = document.createElement('div');
    wrapper.id = 'lang-switch';
    wrapper.setAttribute('aria-label', 'Language selector');
    wrapper.innerHTML =
      '<div id="lang-switch-track">' +
        '<a id="ls-pt" class="ls-btn" href="/" title="Portugu\u00eas (Brasil)">'
          + '<span class="ls-flag">\ud83c\udde7\ud83c\uddf7</span>'
          + '<span class="ls-label">PT&#8209;BR</span>'
        + '</a>'
        + '<a id="ls-en" class="ls-btn" href="/en/" title="English">'
          + '<span class="ls-flag">\ud83c\uddfa\ud83c\uddf8</span>'
          + '<span class="ls-label">EN</span>'
        + '</a>'
      + '</div>';

    document.body.appendChild(wrapper);

    var ptBtn = document.getElementById('ls-pt');
    var enBtn = document.getElementById('ls-en');

    if (isEn) {
      enBtn.classList.add('ls-active');
    } else {
      ptBtn.classList.add('ls-active');
    }
  } catch (e) { console.warn('lang switch init failed', e); }
});
