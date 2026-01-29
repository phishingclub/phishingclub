<script>
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import { showIsLoading } from '$lib/store/loading';

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

	// exit to global context
	function exitCompanyView() {
		showIsLoading();
		appState.clearContext();
		localStorage.setItem('context', '');
		location.reload();
	}

	$: isCompanyView = context.current === AppStateService.CONTEXT.COMPANY && context.companyName;
</script>

{#if isCompanyView}
	<!-- top banner -->
	<div class="w-full h-9 bg-active-blue dark:bg-active-blue z-30 company-banner">
		<div class="h-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
			<div class="flex items-center justify-center gap-4 h-full">
				<div class="flex items-center space-x-2">
					<span class="text-white/70 font-medium text-sm"> Viewing as </span>
					<span class="text-white font-semibold text-sm">
						{context.companyName}
					</span>
				</div>

				<!-- exit button -->
				<button
					on:click={exitCompanyView}
					class="flex items-center gap-1 px-2 py-0.5 text-white/50 hover:text-white/80 text-xs transition-colors duration-200"
					title="Exit company view"
				>
					<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M6 18L18 6M6 6l12 12"
						></path>
					</svg>
				</button>
			</div>
		</div>
	</div>
{/if}

<!-- border frame around entire viewport when in company view -->
{#if isCompanyView}
	<div class="company-view-frame"></div>
{/if}

<style>
	.company-banner {
		box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
	}

	.company-view-frame {
		position: fixed;
		top: 0;
		left: 0;
		right: 0;
		bottom: 0;
		border: 3px solid #1e3fa8;
		pointer-events: none;
		z-index: 9999;
	}

	:global(.dark) .company-view-frame {
		border-color: #1e3fa8;
	}
</style>
