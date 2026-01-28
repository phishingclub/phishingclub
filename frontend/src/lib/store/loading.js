import { writable, derived } from 'svelte/store';

// manual loading state (from explicit showIsLoading/hideIsLoading calls)
const manualLoading = writable(false);

// navigation loading state (from sveltekit navigation)
export const navigationLoading = writable(false);

// combined loading state - true if either manual or navigation is loading
export const isLoading = derived(
	[manualLoading, navigationLoading],
	([$manual, $navigation]) => $manual || $navigation
);

export const showIsLoading = () => {
	manualLoading.set(true);
};

export const hideIsLoading = () => {
	manualLoading.set(false);
};

export const setNavigationLoading = (value) => {
	navigationLoading.set(value);
};
