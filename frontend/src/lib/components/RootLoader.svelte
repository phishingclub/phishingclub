<script>
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { Session } from '$lib/service/session';
	import { AppStateService } from '$lib/service/appState';

	// services
	const session = Session.instance;
	const appState = AppStateService.instance;

	onMount(() => {
		showIsLoading();
		const id = setInterval(async () => {
			try {
				const ok = await api.application.health();
				if (ok) {
					await session.ping();
					hideIsLoading();
					clearInterval(id);
					appState.ready();
				}
			} catch (e) {}
		}, 1000);
		return () => {
			hideIsLoading();
			clearInterval(id);
			appState.ready();
		};
	});
</script>

<div class="w-full h-full flex border-4 border-pc-darkblue animate-pulse"></div>
