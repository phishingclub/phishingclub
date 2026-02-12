import { writable } from 'svelte/store';

/**
 * store for tracking the current resource being viewed
 * this allows the company banner to show context mismatch information
 */
function createResourceContextStore() {
	const { subscribe, set, update } = writable({
		resourceType: null, // e.g., 'campaign', 'template', 'email'
		resourceCompanyID: null,
		resourceCompanyName: null,
		isActive: false
	});

	return {
		subscribe,
		/**
		 * set the current resource context
		 * @param {string} type - resource type (e.g., 'campaign', 'template')
		 * @param {string|null} companyID - company id the resource belongs to (null for global)
		 * @param {string|null} companyName - company name (null for global)
		 */
		setResource: (type, companyID, companyName) => {
			set({
				resourceType: type,
				resourceCompanyID: companyID,
				resourceCompanyName: companyName,
				isActive: true
			});
		},
		/**
		 * clear the resource context (call when leaving a resource page)
		 */
		clear: () => {
			set({
				resourceType: null,
				resourceCompanyID: null,
				resourceCompanyName: null,
				isActive: false
			});
		}
	};
}

export const resourceContext = createResourceContextStore();
