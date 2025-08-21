import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	build: {
		sourcemap: true,
		cssMinify: true,
		minify: true,
		assetsInlineLimit: 4096,
		rollupOptions: {
			output: {
				compact: true
			}
		}
	}
});
