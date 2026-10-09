import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
export default {
	kit: {
		adapter: adapter({
			// pages: 'build',
			assets: 'build',
			fallback: 'index.html',
			precompress: false,
			strict: true
		}),
		alias: {
			$lib: './src/lib',
			'$lib/*': './src/lib/*'
		},
		// Hash mode lets SvelteKit add the hash of its own inline start script, so
		// the app boots under script-src 'self' with no unsafe-inline. style-src
		// keeps unsafe-inline because Monaco and Svelte inject styles at runtime
		// that can not be hashed. blob: and data: are allowed only where previews
		// and Monaco workers need them, never for script. frame-ancestors and
		// form-action can not be set from a meta tag, so they live in the Go
		// security headers middleware instead.
		csp: {
			mode: 'hash',
			directives: {
				'default-src': ['self'],
				'script-src': ['self'],
				'style-src': ['self', 'unsafe-inline'],
				'img-src': ['self', 'data:', 'blob:'],
				'font-src': ['self', 'data:'],
				'connect-src': ['self'],
				'worker-src': ['self', 'blob:'],
				// previews load from the same origin preview endpoint, not data: or
				// blob: documents, so frame-src only needs self
				'frame-src': ['self'],
				'object-src': ['none'],
				'base-uri': ['self']
			}
		}
	}
};
