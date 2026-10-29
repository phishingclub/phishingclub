// Public documentation guide location.
export const DOCS_BASE = 'https://phishing.club/guide';

export function guideURL(slug, anchor) {
	const base = `${DOCS_BASE}/${slug}/`;
	return anchor ? `${base}#${anchor}` : base;
}
