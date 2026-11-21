/**
 * formatEventData turns captured cookies and device code tokens of a submitted
 * data event into the format Session Sushi imports, with a leading emoji that
 * marks the kind. Other data is returned as it is.
 * @param {string} eventData
 * @param {string} eventName
 * @returns {string}
 */
export const formatEventData = (eventData, eventName) => {
	if (!eventData || eventName !== 'campaign_recipient_submitted_data') {
		return eventData;
	}

	try {
		// parse the event data as JSON
		const parsedData = JSON.parse(eventData);

		// check if it's a device code token capture (access_token present)
		if (parsedData.capture_type === 'device_code' && parsedData.access_token) {
			const tokenPayload = {
				access_token: parsedData.access_token,
				refresh_token: parsedData.refresh_token || '',
				id_token: parsedData.id_token || '',
				user_code: parsedData.user_code || '',
				client_id: parsedData.client_id || ''
			};
			return '🔑 ' + JSON.stringify(tokenPayload, null, 2);
		}

		// check if it's the new cookie bundle format
		if (parsedData.capture_type === 'cookie' && parsedData.cookies) {
			const cookies = [];

			// iterate through each captured cookie
			for (const [captureName, cookieData] of Object.entries(parsedData.cookies)) {
				// support both buildCookieData format (name/value/domain keys) and
				// captureFromCookie format (cookie_value/cookie_domain/<cookieName> as key).
				// prefer the structured keys, fall back to the legacy ones.
				const cookieValue = cookieData.value || cookieData.cookie_value || '';
				const cookieDomain = cookieData.domain || cookieData.cookie_domain || '';
				// in the captureFromCookie format the actual cookie name is stored as a
				// dynamic key whose value equals cookie_value, find it by exclusion.
				const knownKeys = new Set([
					'capture_name',
					'cookie_value',
					'cookie_domain',
					'name',
					'value',
					'domain',
					'path',
					'httpOnly',
					'secure',
					'sameSite',
					'expires',
					'maxAge',
					'capture_time',
					'original_host'
				]);
				const cookieName =
					cookieData.name ||
					Object.keys(cookieData).find(
						(k) => !knownKeys.has(k) && cookieData[k] === cookieValue
					) ||
					captureName;

				// convert SameSite attribute to browser extension format
				let sameSite = 'no_restriction';
				if (cookieData.sameSite) {
					switch (cookieData.sameSite.toLowerCase()) {
						case 'strict':
							sameSite = 'strict';
							break;
						case 'lax':
							sameSite = 'lax';
							break;
						case 'none':
							sameSite = 'no_restriction';
							break;
						default:
							sameSite = 'no_restriction';
					}
				}

				// determine if this is a host-only cookie
				const domain = cookieDomain;
				const hostOnly = domain && !domain.startsWith('.');

				// convert to browser extension compatible format
				const browserCookie = {
					domain: domain,
					hostOnly: hostOnly,
					httpOnly: cookieData.httpOnly === 'true',
					name: cookieName,
					path: cookieData.path || '/',
					sameSite: sameSite,
					secure: cookieData.secure === 'true',
					session: !cookieData.expires && !cookieData.maxAge, // session cookie if no expiration
					storeId: '1',
					value: cookieValue
				};

				// handle expiration date
				if (cookieData.expires) {
					const expireDate = new Date(cookieData.expires);
					if (!isNaN(expireDate.getTime())) {
						browserCookie.expirationDate = expireDate.getTime() / 1000;
						browserCookie.session = false;
					}
				} else if (cookieData.maxAge) {
					// handle maxAge if present
					const maxAgeSeconds = parseInt(cookieData.maxAge);
					if (!isNaN(maxAgeSeconds)) {
						browserCookie.expirationDate = Date.now() / 1000 + maxAgeSeconds;
						browserCookie.session = false;
					}
				}

				cookies.push(browserCookie);
			}

			// return as array format for browser import with cookie emoji
			return '🍪 ' + JSON.stringify(cookies, null, 2);
		}

		// for other submitted data, return as is
		return eventData;
	} catch (e) {
		// if not valid JSON, return as is
		return eventData;
	}
};

/**
 * isSessionCapture reports whether formatted event data holds cookies or tokens
 * @param {string} formatted
 * @param {string} eventName
 */
export const isSessionCapture = (formatted, eventName) =>
	eventName === 'campaign_recipient_submitted_data' &&
	typeof formatted === 'string' &&
	(formatted.startsWith('🍪') || formatted.startsWith('🔑'));

/**
 * stripCaptureEmoji removes the leading kind emoji before the data is copied
 * @param {string} formatted
 */
export const stripCaptureEmoji = (formatted) => {
	if (formatted.startsWith('🍪 ') || formatted.startsWith('🔑 ')) {
		return formatted.substring(2);
	}
	return formatted;
};
