import { writable } from 'svelte/store';

export const isLoading = writable(false);

export const showIsLoading = () => {
	isLoading.set(true);
};

export const hideIsLoading = () => {
	isLoading.set(false);
};
