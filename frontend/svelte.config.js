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
		}
		// magic for dynamic routes to render at build time
		// creates one page for each word in the dictionary
		/*
		prerender: {
			crawl: true,
			entries: ['/']
		},
		*/
		/* TODO
		csp: {
			mode: "hash",
			directives: { "script-src": ["self"] },
		},
		*/
	}
};
