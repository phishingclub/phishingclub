// handle not ok typical responses such as unauthenticated, renew password and such

import { goto } from '$app/navigation';

/**
 * @param {import("./client").ApiResponse} apiResponse
 * @returns {import("./client").ApiResponse} apiResponse
 **/
export const immediateResponseHandler = (apiResponse) => {
	// Unauthenticated move the user to the login page
	if (apiResponse.statusCode === 401) {
		goto('/login');
		window.location.reload();
	}
	// If the user must renew their password, redirect to login
	if (apiResponse.statusCode === 400 && apiResponse.error === 'New password required') {
		goto('/login');
		return;
	}
	return apiResponse;
};
