import { writable } from "svelte/store";

/**
 * @type {import("svelte/store").Writable<{id: number, text: string, type:string}[]>} 
 */
export const toasts = writable([]);

let nextID = 0;

/**
 * add a toast
 *  
 * @param {string} text
 * @param {"Success"|"Info"|"Warning"|"Error"} type
 */
export const addToast = (text, type, visibilityMS = 5000) => {
    const t = { id: nextID++, text, type }
    toasts.update((toasts) => [...toasts, t]);
    // remove the toast after the specified time
    setTimeout(() => {
        toasts.update((toasts) => toasts.filter((toast) => toast.id !== t.id));
    }, visibilityMS);
}

/**
 * Removes a toast 
 * @param {number} id 
 */
export const removeToast = (id) => {
    toasts.update((toasts) => toasts.filter((toast) => toast.id !== id));
}