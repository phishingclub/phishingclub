import { writable } from 'svelte/store';

export const activeFormElement = writable(null);

export const activeFormElementSubscribe = (id, callback) => {
	return activeFormElement.subscribe((activeId) => {
		if (activeId === id && activeId !== null) {
			return;
		}
		callback();
	});
};
