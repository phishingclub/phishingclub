// Opens untrusted preview content without ever giving it the admin origin.
// Untrusted markup is posted to the backend preview endpoint, which returns it
// with a sandbox content security policy. The sandbox gives the document an
// opaque origin, so its scripts can not read admin cookies, call the API with
// the admin session, or reach window.opener. Because it is a real server
// response it carries its own policy and does not inherit the strict admin CSP,
// so the preview still loads its own images, styles, fonts and scripts. A data:
// or blob: document would instead inherit the admin CSP and break the preview.

// Types the browser does not run page script for. These open directly so the
// native viewer or download is used. Everything else, including an unknown or
// empty type, is treated as active content and isolated in the sandbox, so a
// mislabeled file errs on the safe side. image/svg+xml is deliberately left out
// because an svg opened as a document can run script.
const INERT_MIME_ALLOWLIST = new Set([
	// documents and images that render in a native viewer
	'application/pdf',
	'image/png',
	'image/jpeg',
	'image/gif',
	'image/webp',
	'image/bmp',
	'image/x-icon',
	'image/vnd.microsoft.icon',
	// archives and office files, which the browser downloads. office documents
	// are zip containers, so they are sniffed as application/zip
	'application/zip',
	'application/octet-stream',
	'application/x-gzip',
	'application/gzip',
	'application/x-rar-compressed',
	'application/x-7z-compressed',
	'application/postscript',
	'application/wasm',
	// audio and video the browser plays or downloads
	'application/ogg',
	'audio/aiff',
	'audio/midi',
	'audio/mpeg',
	'audio/ogg',
	'audio/wave',
	'audio/wav',
	'audio/webm',
	'video/avi',
	'video/mp4',
	'video/ogg',
	'video/webm',
	'video/quicktime',
	// fonts
	'font/collection',
	'font/otf',
	'font/ttf',
	'font/woff',
	'font/woff2'
]);

const baseMime = (mime) => (mime || '').split(';')[0].trim().toLowerCase();

export const PREVIEW_ENDPOINT = '/api/v1/preview';

// Post html to the preview endpoint and navigate the target to the sandboxed
// response. target is a window or frame name, or '_blank' for a new tab. A new
// tab opened this way gets no opener in current browsers, and the response
// sandbox keeps it isolated regardless.
export const submitPreviewForm = (html, target = '_blank') => {
	const form = document.createElement('form');
	form.method = 'POST';
	form.action = PREVIEW_ENDPOINT;
	form.target = target;
	// a new tab must not keep a handle to the admin window. current browsers
	// imply this for a blank target, this makes it explicit where honored.
	form.rel = 'noopener noreferrer';
	form.style.display = 'none';
	const field = document.createElement('textarea');
	field.name = 'content';
	field.value = String(html ?? '');
	form.appendChild(field);
	document.body.appendChild(form);
	form.submit();
	form.remove();
};

const openBlobTab = (content, type) => {
	const url = URL.createObjectURL(new Blob([content], { type }));
	window.open(url, '_blank', 'noopener,noreferrer');
	// noopener gives no window handle to watch for load, so revoke on a timer.
	// The new tab reads the blob immediately when it opens.
	setTimeout(() => URL.revokeObjectURL(url), 60000);
};

// Open an html string isolated in a new tab through the sandbox endpoint.
export const openIsolatedHtml = (html) => submitPreviewForm(html, '_blank');

const escapeHtml = (s) =>
	String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// Text types are shown as escaped text inside the sandbox rather than opened as
// a direct blob. A direct text blob is safe in Chrome and Firefox, but some
// browsers up sniff a text blob that contains markup into html. Rendering it as
// escaped text in the opaque sandbox keeps it readable and safe everywhere.
const TEXT_DISPLAY_TYPES = new Set(['text/plain', 'text/csv', 'application/json']);

const openIsolatedText = (text) =>
	openIsolatedHtml(
		`<!doctype html><meta charset="utf-8"><pre style="white-space:pre-wrap;word-break:break-word">${escapeHtml(text)}</pre>`
	);

// Open fetched bytes. Inert types render directly, active or unknown types are
// isolated. bytes may be a Uint8Array, an ArrayBuffer or a string.
export const openPreviewBytes = (bytes, mime) => {
	const type = baseMime(mime);
	if (TEXT_DISPLAY_TYPES.has(type)) {
		const text = typeof bytes === 'string' ? bytes : new TextDecoder().decode(bytes);
		openIsolatedText(text);
		return;
	}
	if (INERT_MIME_ALLOWLIST.has(type)) {
		openBlobTab(bytes, mime);
		return;
	}
	const html = typeof bytes === 'string' ? bytes : new TextDecoder().decode(bytes);
	openIsolatedHtml(html);
};
