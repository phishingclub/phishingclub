/**
 * Represents the response object returned by the API functions.
 * @typedef {Object} ApiResponse
 * @property {boolean} success - Indicates whether the request was successful.
 * @property {number} statusCode - The status code of the response.
 * @property {string} error - The error message, if any.
 * @property {any} data - The data returned by the request.
 */

/**
 * Fetches JSON data from the specified URL using the GET method.
 * @param {string} url - The URL to fetch the JSON data from.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const getJSON = async (url) => {
	const res = await fetch(url, {
		method: 'GET'
	});
	const body = await res.json();
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends JSON data to the specified URL using the POST method.
 * @param {string} url - The URL to send the JSON data to.
 * @param {Object} data - The JSON data to send.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const postJSON = async (url, data) => {
	const res = await fetch(url, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(data)
	});
	let body = {};
	try {
		body = await res.json();
	} catch (e) {
		body = {
			success: false,
			error: 'invalid JSON in response'
		};
	}
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends JSON data to the specified URL using the POST method.
 * @param {string} url - The URL to send the JSON data to.
 * @param {Object} data - The JSON data to send.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const patchJSON = async (url, data) => {
	const res = await fetch(url, {
		method: 'PATCH',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(data)
	});
	let body = {};
	try {
		body = await res.json();
	} catch (e) {
		body = {
			success: false,
			error: 'invalid JSON in response'
		};
	}
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends JSON data to the specified URL using the PUT method.
 * @param {string} url - The URL to send the JSON data to.
 * @param {Object} data - The JSON data to send.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const putJSON = async (url, data) => {
	const res = await fetch(url, {
		method: 'PUT',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(data)
	});
	let body = {};
	try {
		body = await res.json();
	} catch (e) {
		body = {
			success: false,
			error: 'invalid JSON in response'
		};
	}
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends JSON data to the specified URL using the DELETE method.
 * @param {string} url - The URL to send the JSON data to.
 * @param {Object} data - The JSON data to send.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const deleteJSON = async (url, data) => {
	const res = await fetch(url, {
		method: 'DELETE',
		headers: {
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(data)
	});
	let body = {};
	try {
		body = await res.json();
	} catch (e) {
		body = {
			success: false,
			error: 'invalid JSON in response',
			data: null
		};
	}
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends multipart form data to the specified URL using the POST method.
 * @param {string} url - The URL to send the multipart data to.
 * @param {FormData} formData - The FormData object containing the data to send.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const postMultipart = async (url, formData) => {
	console.log(formData);
	const res = await fetch(url, {
		method: 'POST',
		body: formData
	});
	let body = {};
	try {
		body = await res.json();
	} catch (e) {
		body = {
			success: false,
			error: 'invalid JSON in response'
		};
	}
	return newResponse(body.success, res.status, body.error, body.data);
};

/**
 * Sends a DELETE request to the specified URL.
 * @param {string} url - The URL to send the DELETE request to.
 * @returns {Promise<Object>} - A promise that resolves to the response object containing the JSON data.
 */
export const deleteReq = async (url) => {
	// Function implementation
	const res = await fetch(url, {
		method: 'DELETE'
	});
	try {
		const body = await res.json();
		return newResponse(body.success, res.status, body.error, body.data);
	} catch (e) {
		return newResponse(false, res.status, 'invalid JSON in response', null);
	}
};

/**
 * Creates a new response object.
 * @param {boolean} success - Indicates whether the request was successful.
 * @param {number} statusCode - The status code of the response.
 * @param {string} error - The error message, if any.
 * @param {any} data - The data returned by the request.
 * @returns {Object} - The response object.
 */
export function newResponse(success, statusCode, error, data) {
	return {
		success: success,
		statusCode: statusCode,
		error: error,
		data: data
	};
}
