// Returns the url only when it uses the http or https scheme, otherwise null.
// A stored value like "javascript:..." or "data:..." must never be placed in an
// anchor href or passed to window.open, because a click would run it on the
// admin origin. Use this before rendering any user supplied url as a link.
export const safeExternalHref = (url) => {
	if (typeof url !== 'string') {
		return null;
	}
	const trimmed = url.trim();
	let parsed;
	try {
		parsed = new URL(trimmed);
	} catch {
		return null;
	}
	if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
		// return the parsed form so the value that was checked is exactly the
		// value that gets used. this canonicalizes control characters, backslashes
		// and relative forms, leaving no gap between validation and use.
		return parsed.href;
	}
	return null;
};
