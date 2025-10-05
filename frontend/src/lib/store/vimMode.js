import { writable } from 'svelte/store';
import { getCookie, setCookie } from '$lib/utils/cookies.js';

// get initial vim mode preference from cookie
const getInitialVimMode = () => {
	if (typeof document === 'undefined') {
		return false;
	}
	const vimMode = getCookie('vim_mode_enabled');
	return vimMode === 'true';
};

// create writable store for vim mode state
export const vimModeEnabled = writable(getInitialVimMode());

// subscribe to changes and save to cookie
vimModeEnabled.subscribe((enabled) => {
	if (typeof document !== 'undefined') {
		setCookie('vim_mode_enabled', enabled.toString());
	}
});

// helper function to toggle vim mode
export const toggleVimMode = () => {
	vimModeEnabled.update(enabled => !enabled);
};
