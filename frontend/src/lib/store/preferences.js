import { browser } from '$app/environment';

// storage keys
const STORAGE_KEY_PER_PAGE = 'preference_per_page';

// default values
const DEFAULT_PER_PAGE = 10;
const ACCEPTED_PER_PAGE_VALUES = [10, 25, 50];

/**
 * get the perPage preference from localStorage
 * @returns {number} the stored perPage or default
 */
export const getPerPagePreference = () => {
	if (!browser) return DEFAULT_PER_PAGE;

	try {
		const stored = localStorage.getItem(STORAGE_KEY_PER_PAGE);
		if (stored) {
			const value = parseInt(stored, 10);
			if (ACCEPTED_PER_PAGE_VALUES.includes(value)) {
				return value;
			}
		}
	} catch (e) {
		console.warn('failed to get perPage preference from localStorage', e);
	}

	return DEFAULT_PER_PAGE;
};

/**
 * set the perPage preference in localStorage
 * @param {number} perPage - the perPage value to store
 */
export const setPerPagePreference = (perPage) => {
	if (!browser) return;

	if (!ACCEPTED_PER_PAGE_VALUES.includes(perPage)) {
		console.warn('invalid perPage value, not saving to localStorage:', perPage);
		return;
	}

	try {
		localStorage.setItem(STORAGE_KEY_PER_PAGE, String(perPage));
	} catch (e) {
		console.warn('failed to save perPage preference to localStorage', e);
	}
};
