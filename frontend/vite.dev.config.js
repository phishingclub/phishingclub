import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import basicSsl from '@vitejs/plugin-basic-ssl'

export default defineConfig({
	plugins: [
		sveltekit(),
		basicSsl()
	],
	build: {
		sourcemap: true,
		cssMinify: false,
		minify: false,
		assetsInlineLimit: 0,
	},
	server: {
		https: true,
		host: '0.0.0.0',
		port: 8003,
		proxy: {
			'/api/': {
				target: 'https://backend:8002',
				secure: false,
			},
		},
	},
});
