(function () {
	var PREF_KEY = 'zbx-lang';
	var pathname = window.location.pathname;
	var isEn = pathname.startsWith('/en/');
	var isPt = pathname.startsWith('/pt_BR/');
	var storedLang = null;
	try { storedLang = localStorage.getItem(PREF_KEY); } catch (e) {}

	// If the user lands on root (/), redirect to the preferred language index.
	if (pathname === '/' || pathname === '') {
		var home = storedLang === 'en' ? '/en/' : '/pt_BR/';
		window.location.replace(home);
		return;
	}

	// If the stored preference doesn't match the current URL, redirect immediately.
	// This keeps the language sticky when navigating via the nav menu.
	if (storedLang === 'en' && isPt) {
		window.location.replace(pathname.replace(/^\/pt_BR\//, '/en/'));
		return;
	}
	if (storedLang === 'pt_BR' && isEn) {
		window.location.replace(pathname.replace(/^\/en\//, '/pt_BR/'));
		return;
	}

	function setup() {
		// Intercept every click: handle the home icon (/), and on EN pages
		// rewrite /pt_BR/ links to /en/ to keep the language sticky.
		document.addEventListener('click', function (e) {
			var a = e.target.closest('a[href]');
			if (!a) return;
			try {
				var resolved = new URL(a.href, window.location.href);
				if (resolved.origin !== window.location.origin) return;
				// Home icon always points to '/'
				if (resolved.pathname === '/' || resolved.pathname === '') {
					e.preventDefault();
					window.location.href = isEn ? '/en/' : '/pt_BR/';
					return;
				}
				// On EN pages redirect /pt_BR/ links to /en/
				if (isEn && resolved.pathname.startsWith('/pt_BR/')) {
					e.preventDefault();
					window.location.href =
						resolved.pathname.replace(/^\/pt_BR\//, '/en/') +
						resolved.search + resolved.hash;
				}
			} catch (e2) {}
		});

		// Inject language selector UI
		if (document.getElementById('lang-switch')) return;
		var container = document.createElement('div');
		container.id = 'lang-switch';
		var track = document.createElement('div');
		track.id = 'lang-switch-track';

		var btnPt = document.createElement('button');
		btnPt.className = 'ls-btn ls-pt' + (isEn ? '' : ' ls-active');
		btnPt.innerHTML = '<span class="ls-flag">🇧🇷</span> <span class="ls-label">PT</span>';

		var btnEn = document.createElement('button');
		btnEn.className = 'ls-btn ls-en' + (isEn ? ' ls-active' : '');
		btnEn.innerHTML = '<span class="ls-flag">🇬🇧</span> <span class="ls-label">EN</span>';

		btnPt.addEventListener('click', function () {
			if (!isEn) return;
			try { localStorage.setItem(PREF_KEY, 'pt_BR'); } catch (e) {}
			window.location.pathname = pathname.replace(/^\/en\//, '/pt_BR/');
		});

		btnEn.addEventListener('click', function () {
			if (isEn) return;
			try { localStorage.setItem(PREF_KEY, 'en'); } catch (e) {}
			window.location.pathname = pathname.replace(/^\/pt_BR\//, '/en/');
		});

		track.appendChild(btnPt);
		track.appendChild(btnEn);
		container.appendChild(track);
		document.body.appendChild(container);
	}

	// extra_javascript runs at end of <body>; DOMContentLoaded may have already fired.
	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', setup);
	} else {
		setup();
	}
})();

