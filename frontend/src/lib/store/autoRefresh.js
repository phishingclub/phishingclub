import { writable } from 'svelte/store';

const createAutoRefreshStore = () => {
	const { subscribe, set, update } = writable({
		enabled: true,
		interval: 60000
	});

	return {
		subscribe,
		setEnabled: (enabled) => update((state) => ({ ...state, enabled })),
		setInterval: (interval) => update((state) => ({ ...state, interval })),
		set
	};
};

export const autoRefreshStore = createAutoRefreshStore();

// Helper to manage page-specific storage
export const getPageAutoRefresh = (pageId) => {
	const stored = localStorage.getItem(`autoRefresh_${pageId}`);
	if (stored) {
		return JSON.parse(stored);
	}
	return { enabled: true, interval: 60000 };
};

export const setPageAutoRefresh = (pageId, settings) => {
	localStorage.setItem(`autoRefresh_${pageId}`, JSON.stringify(settings));
};
