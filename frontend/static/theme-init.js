// Runs before the app renders to prevent a white flash in dark mode. Kept as a
// file served from the app origin, not an inline script, so it is allowed by the
// Content Security Policy script-src 'self' without an inline hash. Loaded as a
// classic blocking script in the head so it runs before first paint.
(function () {
	try {
		const storedTheme = localStorage.getItem('theme-mode');
		const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
		const isDark = storedTheme === 'dark' || (!storedTheme && prefersDark);

		if (isDark) {
			document.documentElement.classList.add('dark');
			document.documentElement.style.backgroundColor = '#111827';
		}
	} catch (e) {
		// ignore errors
	}
})();
