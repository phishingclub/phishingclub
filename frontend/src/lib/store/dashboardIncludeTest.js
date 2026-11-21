import { writable } from 'svelte/store';
import { browser } from '$app/environment';

// the Include test campaigns setting shared by the dashboard tabs, kept for
// this browser
const STORAGE_KEY = 'dashboard_include_test';

const read = () => {
	if (!browser) return false;
	try {
		return localStorage.getItem(STORAGE_KEY) === 'true';
	} catch (e) {
		return false;
	}
};

export const dashboardIncludeTest = writable(read());

dashboardIncludeTest.subscribe((value) => {
	if (!browser) return;
	try {
		localStorage.setItem(STORAGE_KEY, String(!!value));
	} catch (e) {
		// storage can be blocked, the setting then lasts until reload
	}
});
