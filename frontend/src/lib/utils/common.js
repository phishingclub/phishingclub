import { addToast } from '$lib/store/toast';

export const getModalText = (name, mode) => {
	let t = '';
	switch (mode) {
		case 'create':
			t = `New ${name}`;
			break;
		case 'copy':
			t = `New ${name}`;
			break;
		case 'update':
			t = `Update ${name}`;
			break;
	}
	return t;
};

export const debounce = (func, delay) => {
	let timeoutId;
	return (...args) => {
		if (timeoutId) {
			clearTimeout(timeoutId);
		}
		timeoutId = setTimeout(() => {
			func(...args);
		}, delay);
	};
};

export const debounceTyping = (func) => debounce(func, 350);

export const shouldHideMenuItem = (route) => {
	// All menu items are now accessible - no edition restrictions
	return false;
};

export const onClickCopy = (text) => {
	navigator.clipboard
		.writeText(text)
		.then(() => {
			addToast('Copied to clipboard', 'Success');
		})
		.catch((err) => {
			addToast('Failed to copy to clipboard', 'Error');
			console.error('failed to copy to clipboard', err);
		});
};
