<script>
	import { AppStateService } from '$lib/service/appState';
	import { onMount, onDestroy } from 'svelte';
	import { showIsLoading } from '$lib/store/loading';
	import { resourceContext } from '$lib/store/resourceContext';

	let context = {
		current: '',
		companyName: '',
		companyID: null
	};

	let resource = {
		resourceType: null,
		resourceCompanyID: null,
		resourceCompanyName: null,
		isActive: false
	};

	const appState = AppStateService.instance;

	let unsubAppState;
	let unsubResource;

	onMount(() => {
		unsubAppState = appState.subscribe((s) => {
			context = {
				current: s.context.current,
				companyName: s.context.companyName,
				companyID: s.context.companyID
			};
		});

		unsubResource = resourceContext.subscribe((r) => {
			resource = { ...r };
		});
	});

	onDestroy(() => {
		if (unsubAppState) unsubAppState();
		if (unsubResource) unsubResource();
	});

	// exit to global context
	function exitCompanyView() {
		showIsLoading();
		appState.clearContext();
		localStorage.setItem('context', '');
		location.reload();
	}

	// switch to resource's context
	function switchToResourceContext() {
		showIsLoading();
		if (resource.resourceCompanyID) {
			// switch to company context
			appState.setCompanyContext(resource.resourceCompanyID, resource.resourceCompanyName);
			localStorage.setItem(
				'context',
				JSON.stringify({ id: resource.resourceCompanyID, name: resource.resourceCompanyName })
			);
		} else {
			// switch to global context
			appState.clearContext();
			localStorage.setItem('context', '');
		}
		location.reload();
	}

	$: isCompanyView = context.current === AppStateService.CONTEXT.COMPANY && context.companyName;
	$: isResourceActive = resource.isActive;
	$: isResourceGlobal = isResourceActive && !resource.resourceCompanyID;
	$: isResourceInDifferentCompany =
		isResourceActive &&
		resource.resourceCompanyID &&
		context.companyID !== resource.resourceCompanyID;
	$: hasContextMismatch =
		isResourceActive &&
		((isCompanyView && isResourceGlobal) ||
			(!isCompanyView && resource.resourceCompanyID) ||
			isResourceInDifferentCompany);

	// determine banner style and content
	$: bannerStyle = 'bg-active-blue dark:bg-active-blue';
</script>

{#if isCompanyView || hasContextMismatch}
	<!-- top banner -->
	<div class="w-full h-9 {bannerStyle} z-30 company-banner">
		<div class="h-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
			<div class="flex items-center justify-center gap-4 h-full">
				{#if hasContextMismatch}
					<!-- context mismatch view -->
					<div class="flex items-center space-x-2">
						{#if isCompanyView && isResourceGlobal}
							<span class="text-white/70 font-medium text-sm">Viewing as</span>
							<span class="text-white font-semibold text-sm">{context.companyName}</span>
							<span class="text-white/70 font-medium text-sm">•</span>
							<span class="text-white/90 font-medium text-sm">
								This {resource.resourceType || 'resource'} is
								<strong class="font-bold">global</strong>
							</span>
						{:else if !isCompanyView && resource.resourceCompanyID}
							<span class="text-white/90 font-medium text-sm">
								This {resource.resourceType || 'resource'} belongs to
								<strong class="font-bold">{resource.resourceCompanyName || 'a company'}</strong>
							</span>
						{:else if isResourceInDifferentCompany}
							<span class="text-white/70 font-medium text-sm">Viewing as</span>
							<span class="text-white font-semibold text-sm">{context.companyName}</span>
							<span class="text-white/70 font-medium text-sm">•</span>
							<span class="text-white/90 font-medium text-sm">
								This {resource.resourceType || 'resource'} belongs to
								<strong class="font-bold"
									>{resource.resourceCompanyName || 'another company'}</strong
								>
							</span>
						{/if}

						<!-- switch button -->
						<button
							on:click={switchToResourceContext}
							class="flex items-center gap-1.5 px-3 py-1 bg-white/20 hover:bg-white/30 text-white rounded text-xs font-semibold transition-colors duration-200"
							title="Switch to {isResourceGlobal
								? 'global'
								: resource.resourceCompanyName || 'company'} context"
						>
							<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
								></path>
							</svg>
							Switch
						</button>
					</div>
				{:else}
					<!-- normal company view -->
					<div class="flex items-center space-x-2">
						<span class="text-white/70 font-medium text-sm">Viewing as</span>
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
				{/if}
			</div>
		</div>
	</div>
{/if}

<!-- border frame around entire viewport when in company view or context mismatch -->
{#if isCompanyView || hasContextMismatch}
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
		border: 3px solid;
		pointer-events: none;
		z-index: 9999;
	}

	:global(.dark) .company-view-frame {
		border-color: #1e3fa8;
	}
</style>
