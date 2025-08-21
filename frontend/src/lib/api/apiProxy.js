import { API } from '$lib/api/api.js';
import { immediateResponseHandler } from '$lib/api/middleware';

// api singleton where each response is proxyed to the responseHandler 
export const api = new Proxy(API.instance, {
    get: function(target, prop) {
        const apiSection = target[prop];
        const apiSectionProxy = new Proxy(apiSection, {
            get: function(target, prop) {
                const method = new Proxy(target[prop], {
                    apply: async (target, _, argumentsList) => {
                        return immediateResponseHandler(await target(...argumentsList));
                    }
                });
                return method
            }
        });
        return apiSectionProxy;
    }
});

