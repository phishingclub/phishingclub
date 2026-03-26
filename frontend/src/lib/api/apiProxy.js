import { API } from '$lib/api/api.js';
import { immediateResponseHandler } from '$lib/api/middleware';

// wrap a single async function with the response handler
const wrapMethod = (fn) =>
	new Proxy(fn, {
		apply: async (target, _, argumentsList) => {
			return immediateResponseHandler(await target(...argumentsList));
		}
	});

// wrap a section (one level of methods, possibly with nested sub-objects)
const wrapSection = (section) =>
	new Proxy(section, {
		get: function (target, prop) {
			const value = target[prop];
			// if the value is a plain object (nested sub-section like scim), wrap it recursively
			if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
				return wrapSection(value);
			}
			// otherwise it is a method — wrap it with the response handler
			return wrapMethod(value);
		}
	});

// api singleton where each response is proxied to the responseHandler
export const api = new Proxy(API.instance, {
	get: function (target, prop) {
		const apiSection = target[prop];
		return wrapSection(apiSection);
	}
});
