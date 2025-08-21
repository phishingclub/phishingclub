import { writable } from 'svelte/store';

// modeLight is the state for light mode
export const modeLight = 'light';
// modeDark is the state for dark mode
export const modeDark = 'dark';
// modeNotSet is the state for when the mode is not set
const modeNotSet = 'not-set';
// modeKey is the key used to store the mode in localstorage
const modeKey = 'theme-mode';

/**
 * theme is either 'light' or 'dark' -
 * use it to know which theme is current and subscribe to know when it changes
 */
export const theme = writable('dark');

/**
 * toggleMode is used to toggle the theme mode from light to dark or vice versa
 * it is used by the toggle button in the header
 * @returns {void} - the new mode, either light or dark
 */
export const toggleMode = () => {
	theme.update((current) => {
		let mode = current;
		switch (mode) {
			case modeLight:
				mode = modeDark;
				break;
			case modeDark:
				mode = modeLight;
				break;
			default:
				throw new Error(`Unknown mode passed to changeMode: ${mode} - must be 'light' or 'dark'`);
		}
		setModeToLocalStorage(mode);
		return mode;
	});
};

/**
 * setupTheme is used to set the theme mode
 * it must be called once it the bootstrapping of the application
 * it checks if a prefered mode is set in localstorage, if not
 * it checks the OS preferred mode and if that is not set it defaults to light
 * the default mode is saved to localstorage
 *
 * @returns {string} - the mode that was set, either light or dark
 */
export function setupTheme() {
	let mode = getModeFromLocalStorage();
	if (mode === modeNotSet) {
		mode = getOSPreferredMode();
		setModeToLocalStorage(mode);
	}
	changeMode(mode);
	return mode;
}

/**
 * getOSPreferredMode is used to get the OS preferred light or dark mode
 * @returns {string} - the mode that was set, either light or dark
 */
export const getOSPreferredMode = () => {
	const darkMediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
	if (darkMediaQuery.matches) {
		return modeDark;
	}
	return modeLight;
};

/*
 * This function is used to set the theme mode in the local storage
 * @param {string} mode - light or dark
 * @returns {string}
 */
export const getModeFromLocalStorage = () => {
	return localStorage.getItem(modeKey) ?? modeNotSet;
};

/**
 * setModeToLocalStorage is used to set the theme mode in the local storage
 * @param {string} mode  - light or dark
 */
export const setModeToLocalStorage = (mode) => {
	localStorage.setItem(modeKey, mode);
};

/**
 * changeMode is used to change the theme mode from light to dark or vice versa
 * @param {string} mode - light or dark
 */
const changeMode = (mode) => {
	switch (mode) {
		case modeLight:
			theme.set(modeLight);
			break;
		case modeDark:
			theme.set(modeDark);
			break;
		default:
			throw new Error(`Unknown mode passed to changeMode: ${mode} - must be 'light' or 'dark'`);
	}
};
