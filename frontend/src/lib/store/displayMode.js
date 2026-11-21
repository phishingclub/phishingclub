import { writable } from 'svelte/store';

/**
 * display mode constants
 * WHITEBOX is Phishing Simulation, BLACKBOX is Red Team Phishing and
 * ALL is Show All, which shows the features of both
 */
export const DISPLAY_MODE = {
	WHITEBOX: 'whitebox',
	BLACKBOX: 'blackbox',
	ALL: 'all'
};

/**
 * true when the mode shows red team features
 * @param {string} mode
 * @returns {boolean}
 */
export const showsRedTeam = (mode) => mode === DISPLAY_MODE.BLACKBOX || mode === DISPLAY_MODE.ALL;

/**
 * true when the mode shows phishing simulation features
 * @param {string} mode
 * @returns {boolean}
 */
export const showsSimulation = (mode) => mode === DISPLAY_MODE.WHITEBOX || mode === DISPLAY_MODE.ALL;

/**
 * maps the display flags on a menu item to a ConditionalDisplay show value
 * @param {{whitebox?: boolean, blackbox?: boolean, simulation?: boolean}} item
 * @returns {'whitebox' | 'blackbox' | 'simulation' | 'both'}
 */
export const showFor = (item) => {
	if (item.whitebox) return 'whitebox';
	if (item.blackbox) return 'blackbox';
	if (item.simulation) return 'simulation';
	return 'both';
};

/**
 * true when content with this show value is shown in the mode
 * both: every mode
 * blackbox: red team phishing and show all
 * simulation: phishing simulation and show all
 * whitebox: only phishing simulation, for a simple variant of something
 * that show all already shows in its blackbox form
 * @param {'whitebox' | 'blackbox' | 'simulation' | 'both'} show
 * @param {string} mode
 * @returns {boolean}
 */
export const isShown = (show, mode) =>
	show === 'both' ||
	(show === 'whitebox' && mode === DISPLAY_MODE.WHITEBOX) ||
	(show === 'blackbox' && showsRedTeam(mode)) ||
	(show === 'simulation' && showsSimulation(mode));

/**
 * display mode store
 * controls which features the admin UI shows: phishing simulation (whitebox),
 * red team phishing (blackbox) or all of them
 */
function createDisplayModeStore() {
	const { subscribe, set } = writable(DISPLAY_MODE.WHITEBOX);

	return {
		subscribe,
		/**
		 * set display mode to whitebox (phishing simulation)
		 */
		setWhitebox: () => set(DISPLAY_MODE.WHITEBOX),
		/**
		 * set display mode to blackbox (red team phishing)
		 */
		setBlackbox: () => set(DISPLAY_MODE.BLACKBOX),
		/**
		 * set display mode
		 * @param {string} mode - one of the DISPLAY_MODE values
		 */
		setMode: (mode) => {
			if (Object.values(DISPLAY_MODE).includes(mode)) {
				set(mode);
			} else {
				console.warn('invalid display mode:', mode);
			}
		},
		/**
		 * check if current mode is whitebox
		 * @param {string} currentMode
		 * @returns {boolean}
		 */
		isWhitebox: (currentMode) => currentMode === DISPLAY_MODE.WHITEBOX,
		/**
		 * check if current mode is blackbox
		 * @param {string} currentMode
		 * @returns {boolean}
		 */
		isBlackbox: (currentMode) => currentMode === DISPLAY_MODE.BLACKBOX,
		/**
		 * check if current mode is show all
		 * @param {string} currentMode
		 * @returns {boolean}
		 */
		isAll: (currentMode) => currentMode === DISPLAY_MODE.ALL
	};
}

export const displayMode = createDisplayModeStore();
