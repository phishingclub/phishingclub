/**
 * simple cookie utility for storing user preferences
 */

/**
 * get a cookie value by name
 * @param {string} name - cookie name
 * @returns {string|null} cookie value or null if not found
 */
export function getCookie(name) {
	if (typeof document === 'undefined') {
		return null;
	}

	const value = `; ${document.cookie}`;
	const parts = value.split(`; ${name}=`);

	if (parts.length === 2) {
		return parts.pop().split(';').shift();
	}

	return null;
}

/**
 * set a cookie value
 * @param {string} name - cookie name
 * @param {string} value - cookie value
 * @param {number} days - expiration in days (default: 365)
 */
export function setCookie(name, value, days = 365) {
	if (typeof document === 'undefined') {
		return;
	}

	const expires = new Date();
	expires.setTime(expires.getTime() + (days * 24 * 60 * 60 * 1000));

	document.cookie = `${name}=${value};expires=${expires.toUTCString()};path=/`;
}

/**
 * get vim mode preference from cookie
 * @returns {boolean} vim mode enabled state
 */
export function getVimModePreference() {
	const vimMode = getCookie('vim_mode_enabled');
	return vimMode === 'true';
}

/**
 * save vim mode preference to cookie
 * @param {boolean} enabled - vim mode enabled state
 */
export function setVimModePreference(enabled) {
	setCookie('vim_mode_enabled', enabled.toString());
}
