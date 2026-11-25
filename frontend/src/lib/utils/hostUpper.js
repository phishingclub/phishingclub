// hostUpper mirrors the backend hostUpper template function for the editor
// preview. It uppercases letters in the host of a URL, every match when n is
// 0 or the nth match otherwise. Only the host changes, the rest of the URL is
// kept as is. When the value has no host, the host is an IP or the letters are
// not found, the value is returned unchanged.
export function hostUpper(letters, url, n = 0) {
	if (!letters || typeof url !== 'string') {
		return url;
	}
	const scheme = /^[A-Za-z][A-Za-z0-9+.-]*:\/\//.exec(url);
	if (!scheme) {
		return url;
	}
	const authStart = scheme[0].length;
	const rel = url.slice(authStart).search(/[/?#\\]/);
	const authEnd = rel < 0 ? url.length : authStart + rel;
	const at = url.slice(authStart, authEnd).lastIndexOf('@');
	const hostStart = at < 0 ? authStart : authStart + at + 1;
	if (url[hostStart] === '[') {
		return url;
	}
	const colon = url.slice(hostStart, authEnd).indexOf(':');
	const hostEnd = colon < 0 ? authEnd : hostStart + colon;
	if (hostEnd <= hostStart) {
		return url;
	}
	const host = url.slice(hostStart, hostEnd);
	if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) {
		return url;
	}
	const asciiLower = (s) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
	const lowerHost = asciiLower(host);
	const needle = asciiLower(letters);
	const chars = host.split('');
	let count = 0;
	let from = 0;
	for (;;) {
		const pos = lowerHost.indexOf(needle, from);
		if (pos < 0) {
			break;
		}
		from = pos + needle.length;
		count++;
		if (n !== 0 && count !== n) {
			continue;
		}
		for (let i = pos; i < pos + needle.length; i++) {
			chars[i] = chars[i].replace(/[a-z]/, (c) => c.toUpperCase());
		}
		if (n !== 0) {
			break;
		}
	}
	return url.slice(0, hostStart) + chars.join('') + url.slice(hostEnd);
}

// HOST_UPPER_TEMPLATE matches {{.Var | hostUpper "x" n}} and {{hostUpper "x" n .Var}}
// with the occurrence number optional
export const HOST_UPPER_TEMPLATE =
	/{{\s*(?:\.(\w+)\s*\|\s*hostUpper\s+"([^"]+)"(?:\s+(\d+))?|hostUpper\s+"([^"]+)"(?:\s+(\d+))?\s+\.(\w+))\s*}}/g;

// replaceHostUpper renders hostUpper calls in a preview using the given values
// for the template variables. Calls on unknown variables, and calls with an
// occurrence of 0 which the backend rejects, are left as written.
export function replaceHostUpper(text, values) {
	if (!text.includes('hostUpper')) {
		return text;
	}
	return text.replace(HOST_UPPER_TEMPLATE, (match, pipeVar, pipeLetters, pipeN, letters, n, v) => {
		const name = pipeVar ?? v;
		if (!Object.hasOwn(values, name) || typeof values[name] !== 'string') {
			return match;
		}
		const given = pipeN ?? n;
		if (given !== undefined && parseInt(given, 10) < 1) {
			return match;
		}
		const occurrence = given === undefined ? 0 : parseInt(given, 10);
		return hostUpper(pipeLetters ?? letters, values[name], occurrence);
	});
}
