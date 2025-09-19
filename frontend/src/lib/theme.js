import { writable } from 'svelte/store';
import { browser } from '$app/environment';

// mode constants
export const modeLight = 'light';
export const modeDark = 'dark';
const modeNotSet = 'not-set';
const modeKey = 'theme-mode';

/**
 * theme store - either 'light' or 'dark'
 * use it to know which theme is current and subscribe to know when it changes
 */
export const theme = writable(modeLight);

/**
 * toggle the theme mode from light to dark or vice versa
 * @returns {void}
 */
export const toggleMode = () => {
	theme.update((current) => {
		const newMode = current === modeLight ? modeDark : modeLight;
		setModeToLocalStorage(newMode);
		applyThemeToHTML(newMode);
		return newMode;
	});
};

/**
 * setup the theme mode - must be called once during app bootstrapping
 * checks localStorage, then OS preference, defaults to light
 * @returns {string} the mode that was set
 */
export function setupTheme() {
	if (!browser) return modeLight;

	let mode = getModeFromLocalStorage();
	if (mode === modeNotSet) {
		mode = getOSPreferredMode();
		setModeToLocalStorage(mode);
	}

	applyThemeToHTML(mode);
	theme.set(mode);
	return mode;
}

/**
 * get the OS preferred light or dark mode
 * @returns {string} either 'light' or 'dark'
 */
export const getOSPreferredMode = () => {
	if (!browser) return modeLight;

	const darkMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
	return darkMediaQuery.matches ? modeDark : modeLight;
};

/**
 * get the theme mode from localStorage
 * @returns {string} the stored mode or 'not-set'
 */
export const getModeFromLocalStorage = () => {
	if (!browser) return modeNotSet;
	return localStorage.getItem(modeKey) ?? modeNotSet;
};

/**
 * set the theme mode in localStorage
 * @param {string} mode - either 'light' or 'dark'
 */
export const setModeToLocalStorage = (mode) => {
	if (!browser) return;
	localStorage.setItem(modeKey, mode);
};

/**
 * apply the theme to the HTML element by adding/removing the 'dark' class
 * @param {string} mode - either 'light' or 'dark'
 */
const applyThemeToHTML = (mode) => {
	if (!browser) return;

	const html = document.documentElement;
	if (mode === modeDark) {
		html.classList.add('dark');
	} else {
		html.classList.remove('dark');
	}
};

/**
 * set up theme change listener for OS preference changes
 */
export const setupOSThemeListener = () => {
	if (!browser) return;

	const darkMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
	darkMediaQuery.addEventListener('change', (e) => {
		// only update if user hasn't manually set a preference
		const storedMode = getModeFromLocalStorage();
		if (storedMode === modeNotSet) {
			const newMode = e.matches ? modeDark : modeLight;
			applyThemeToHTML(newMode);
			theme.set(newMode);
		}
	});
};
