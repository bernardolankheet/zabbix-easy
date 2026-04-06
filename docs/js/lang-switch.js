(function () {
	var PREF_KEY = 'zbx-lang';
	var pathname = window.location.pathname;

	// Detect base path and current language from the URL.
	// Works both on custom domains (basePath='') and GitHub Pages project sites
	// (basePath='/zabbix-easy').
	var ptMatch = pathname.match(/^(.*?)\/pt_BR(\/|$)/);
	var enMatch = pathname.match(/^(.*?)\/en(\/|$)/);
	var basePath = ptMatch ? ptMatch[1] : enMatch ? enMatch[1] : pathname.replace(/\/$/, '');
	var isEn    = !!enMatch;
	var isPt    = !!ptMatch;
	var isRoot  = !isEn && !isPt; // root page = not inside /en/ or /pt_BR/

	var storedLang = null;
	try { storedLang = localStorage.getItem(PREF_KEY); } catch (e) {}

	// Root page: redirect to preferred language immediately.
	if (isRoot) {
		var home = storedLang === 'en' ? basePath + '/en/' : basePath + '/pt_BR/';
		window.location.replace(home);
		return;
	}

	// Sticky language: if stored preference doesn't match the current URL, redirect.
	if (storedLang === 'en' && isPt) {
		window.location.replace(pathname.replace(basePath + '/pt_BR/', basePath + '/en/'));
		return;
	}
	if (storedLang === 'pt_BR' && isEn) {
		window.location.replace(pathname.replace(basePath + '/en/', basePath + '/pt_BR/'));
		return;
	}

	function setup() {
		// Intercept clicks: keep language sticky on nav links and home icon.
		document.addEventListener('click', function (e) {
			var a = e.target.closest('a[href]');
			if (!a) return;
			try {
				var resolved = new URL(a.href, window.location.href);
				if (resolved.origin !== window.location.origin) return;
				var rpath = resolved.pathname;
				// Home/logo click: go to language-aware home
				if (rpath === basePath + '/' || rpath === basePath) {
					e.preventDefault();
					window.location.href = isEn ? basePath + '/en/' : basePath + '/pt_BR/';
					return;
				}
				// On EN pages, rewrite nav /pt_BR/ links to /en/ to stay in English
				if (isEn && rpath.startsWith(basePath + '/pt_BR/')) {
					e.preventDefault();
					window.location.href =
						rpath.replace(basePath + '/pt_BR/', basePath + '/en/') +
						resolved.search + resolved.hash;
				}
			} catch (e2) {}
		});

		// Inject language switcher into the Material header (before GitHub icon).
		if (document.getElementById('lang-switch')) return;
		var container = document.createElement('div');
		container.id = 'lang-switch';

		var btnPt = document.createElement('button');
		btnPt.className = 'ls-btn ls-pt' + (isEn ? '' : ' ls-active');
		btnPt.title = 'Português (Brasil)';
		btnPt.innerHTML = '<span class="ls-flag">🇧🇷</span> <span class="ls-label">PT</span>';

		var btnEn = document.createElement('button');
		btnEn.className = 'ls-btn ls-en' + (isEn ? ' ls-active' : '');
		btnEn.title = 'English';
		btnEn.innerHTML = '<span class="ls-flag">🇺🇸</span> <span class="ls-label">EN</span>';

		btnPt.addEventListener('click', function () {
			if (!isEn) return;
			try { localStorage.setItem(PREF_KEY, 'pt_BR'); } catch (e) {}
			window.location.pathname = pathname.replace(basePath + '/en/', basePath + '/pt_BR/');
		});

		btnEn.addEventListener('click', function () {
			if (isEn) return;
			try { localStorage.setItem(PREF_KEY, 'en'); } catch (e) {}
			window.location.pathname = pathname.replace(basePath + '/pt_BR/', basePath + '/en/');
		});

		container.appendChild(btnPt);
		container.appendChild(btnEn);

		// Prefer injecting inside the Material header, before the GitHub source icon.
		// Fallback: append to body with fixed positioning (CSS handles both cases).
		var headerSource = document.querySelector('.md-header__source');
		if (headerSource && headerSource.parentNode) {
			headerSource.parentNode.insertBefore(container, headerSource);
		} else {
			container.classList.add('ls-fallback');
			document.body.appendChild(container);
		}
	}

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', setup);
	} else {
		setup();
	}
})();

