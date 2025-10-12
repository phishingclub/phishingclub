<script>
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';

	let context = {
		current: '',
		companyName: ''
	};

	const appState = AppStateService.instance;

	onMount(() => {
		const unsub = appState.subscribe((s) => {
			context = {
				current: s.context.current,
				companyName: s.context.companyName
			};
		});
		return () => {
			unsub();
		};
	});
</script>

{#if context.current === AppStateService.CONTEXT.COMPANY && context.companyName}
	<div
		class="sticky top-0 w-full bg-blue-900 dark:bg-gradient-to-r dark:from-emerald-700 dark:to-teal-700 border-b border-blue-800/50 dark:border-emerald-600/50 z-30"
	>
		<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
			<div class="flex items-center justify-center py-1">
				<div class="flex items-center space-x-2 text-xs">
					<svg
						class="w-3 h-3 text-blue-100 dark:text-emerald-100"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4"
						></path>
					</svg>
					<span class="text-blue-100 dark:text-emerald-100 font-medium">Viewing as:</span>
					<span class="text-white dark:text-white font-semibold">
						{context.companyName}
					</span>
				</div>
			</div>
		</div>
	</div>
{/if}
