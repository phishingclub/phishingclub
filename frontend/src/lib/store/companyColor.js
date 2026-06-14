import { writable } from 'svelte/store';

// live override for the company banner color so changes made in company
// settings show in the banner and frame without a page reload
// shape: { companyID: string, color: string } or null
export const companyColorOverride = writable(null);
