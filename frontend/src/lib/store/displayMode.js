import { writable } from 'svelte/store';

/**
 * display mode constants
 */
export const DISPLAY_MODE = {
	WHITEBOX: 'whitebox',
	BLACKBOX: 'blackbox'
};

/**
 * display mode store
 * controls whether the application shows detailed technical information (whitebox)
 * or simplified user-friendly information (blackbox)
 */
function createDisplayModeStore() {
	const { subscribe, set, update } = writable(DISPLAY_MODE.WHITEBOX);

	return {
		subscribe,
		/**
		 * set display mode to whitebox (show technical details)
		 */
		setWhitebox: () => set(DISPLAY_MODE.WHITEBOX),
		/**
		 * set display mode to blackbox (hide technical details)
		 */
		setBlackbox: () => set(DISPLAY_MODE.BLACKBOX),
		/**
		 * set display mode
		 * @param {string} mode - DISPLAY_MODE.WHITEBOX or DISPLAY_MODE.BLACKBOX
		 */
		setMode: (mode) => {
			if (mode === DISPLAY_MODE.WHITEBOX || mode === DISPLAY_MODE.BLACKBOX) {
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
		isBlackbox: (currentMode) => currentMode === DISPLAY_MODE.BLACKBOX
	};
}

export const displayMode = createDisplayModeStore();
