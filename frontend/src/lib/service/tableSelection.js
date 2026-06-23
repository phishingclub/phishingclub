import { writable, get } from 'svelte/store';
import { addToast } from '$lib/store/toast';

/**
 * Tracks a set of selected row ids for a table. State is local and never
 * persisted to the URL. Use $selection in markup to read the Set reactively:
 * checked={$selection.has(id)} and count via $selection.size.
 */
export const createTableSelection = () => {
	const { subscribe, set, update } = writable(new Set());

	/** @param {string} id */
	const toggle = (id) =>
		update((s) => {
			const next = new Set(s);
			if (next.has(id)) {
				next.delete(id);
			} else {
				next.add(id);
			}
			return next;
		});

	/**
	 * @param {Array<string>} ids
	 * @param {boolean} checked
	 */
	const setPageSelection = (ids, checked) =>
		update((s) => {
			const next = new Set(s);
			ids.forEach((id) => {
				if (checked) {
					next.add(id);
				} else {
					next.delete(id);
				}
			});
			return next;
		});

	const clear = () => set(new Set());

	/** @param {string} id */
	const isSelected = (id) => get({ subscribe }).has(id);

	return { subscribe, toggle, setPageSelection, clear, isSelected };
};

/**
 * Returns the select all header state for the rows on the current page.
 * @param {Set<string>} selectedSet
 * @param {Array<string>} pageIds
 * @returns {'none'|'some'|'all'}
 */
export const headerSelectionState = (selectedSet, pageIds) => {
	if (!pageIds.length) {
		return 'none';
	}
	let selectedCount = 0;
	for (const id of pageIds) {
		if (selectedSet.has(id)) {
			selectedCount++;
		}
	}
	if (selectedCount === 0) {
		return 'none';
	}
	if (selectedCount === pageIds.length) {
		return 'all';
	}
	return 'some';
};

/**
 * Deletes each id by looping the single item delete endpoint, then toasts a
 * summary of the outcome. Respects the same per item guards as single delete,
 * so items that cannot be deleted are reported as failures.
 * @param {{ ids: Array<string>, deleteFn: (id: string) => Promise<*>, noun: string, nounPlural?: string }} args
 * @returns {Promise<{ deleted: number, failed: number }>}
 */
export const runBulkDelete = async ({ ids, deleteFn, noun, nounPlural = `${noun}s` }) => {
	let deleted = 0;
	let failed = 0;
	for (const id of ids) {
		try {
			const res = await deleteFn(id);
			if (res?.success) {
				deleted++;
			} else {
				failed++;
			}
		} catch (e) {
			failed++;
			console.error(`failed to delete ${noun}`, e);
		}
	}
	if (failed === 0) {
		addToast(`Deleted ${deleted} ${deleted === 1 ? noun : nounPlural}`, 'Success');
	} else {
		addToast(`Deleted ${deleted}, failed ${failed}`, 'Error');
	}
	return { deleted, failed };
};
