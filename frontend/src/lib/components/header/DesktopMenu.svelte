<script>
	import { menu } from '$lib/consts/navigation';
	import { menuIcons as icons } from '$lib/consts/menuIcons.js';
	import { page } from '$app/stores';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';
	import { shouldHideMenuItem } from '$lib/utils/common';
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import { beforeNavigate } from '$app/navigation';

	export let isPinned = false;

	let isExpanded = false;

	// expose collapseMenu for parent to collapse menu on unpin
	export function collapseMenu() {
		isExpanded = false;
	}

	$: isExpanded = isPinned ? true : isExpanded;
	let menuElement;
	let menuItemsElement;
	let hasScrollbar = false;
	let instantCollapse = false;
	let context = {
		current: '',
		companyName: ''
	};

	const appState = AppStateService.instance;
	import { createEventDispatcher } from 'svelte';
	import ConditionalDisplay from '../ConditionalDisplay.svelte';
	const dispatch = createEventDispatcher();

	// check if scrollbar is present
	const checkScrollbar = () => {
		if (menuItemsElement) {
			hasScrollbar = menuItemsElement.scrollHeight > menuItemsElement.clientHeight;
		}
	};

	$: if (isPinned && menuItemsElement) {
		checkScrollbar();
	}

	onMount(() => {
		const unsub = appState.subscribe((s) => {
			context = {
				current: s.context.current,
				companyName: s.context.companyName
			};
		});

		// handle click outside to collapse menu
		const handleClickOutside = (event) => {
			if (!isPinned && isExpanded && menuElement && !menuElement.contains(event.target)) {
				isExpanded = false;
			}
		};

		// check scrollbar on resize
		const resizeObserver = new ResizeObserver(() => {
			if (isPinned) {
				checkScrollbar();
			}
		});

		if (menuItemsElement) {
			resizeObserver.observe(menuItemsElement);
		}

		document.addEventListener('click', handleClickOutside);

		return () => {
			unsub();
			resizeObserver.disconnect();
			document.removeEventListener('click', handleClickOutside);
		};
	});

	// handle navigation to collapse menu
	beforeNavigate(() => {
		if (!isPinned && isExpanded) {
			instantCollapse = true;
			isExpanded = false;
			// reset after a brief moment
			setTimeout(() => {
				instantCollapse = false;
			}, 50);
		}
	});

	$: hasCompanySelected =
		context.current === AppStateService.CONTEXT.COMPANY && context.companyName;

	const getIconForRoute = (route) => {
		const iconMap = {
			'/dashboard/': 'dashboard',
			'/campaign/': 'campaigns_overview',
			'/campaign-template/': 'campaign_templates',
			'/filter/': 'filters',
			'/webhook/': 'webhooks',
			'/script/': 'script',
			'/recipient/': 'recipients_overview',
			'/recipient/group/': 'recipient_groups',
			'/domain/': 'domains_overview',
			'/page/': 'pages',
			'/proxy/': 'proxy',
			'/remote-browser/': 'remote_browser',
			'/asset/': 'assets',
			'/email/': 'emails_overview',
			'/attachment/': 'attachments',
			'/smtp-configuration/': 'smtp_configurations',
			'/api-sender/': 'api_senders',
			'/oauth-provider/': 'oauth_providers'
		};

		return icons[iconMap[route] || 'dashboard']; // fallback to dashboard if route not found
	};
</script>

<div class="flex">
	<nav
		bind:this={menuElement}
		class="hidden lg:flex flex-col fixed top-16 z-10 bg-gradient-to-b from-pc-darkblue to-indigo-400 dark:from-gray-900 dark:to-gray-800 rounded-br-lg overflow-x-hidden min-h-0 max-h-[calc(100vh-4rem)] box-content border-r-[1px] border-pc-darkblue dark:border-highlight-blue/40"
		class:transition-all={!instantCollapse}
		class:w-40={isExpanded}
		class:w-12={!isExpanded}
		class:!top-[100px]={hasCompanySelected}
		class:!max-h-[calc(100vh-100px)]={hasCompanySelected}
	>
		{#if !isPinned}
			<div
				class="bg-highlight-blue/20 dark:bg-gray-800/70 border-b w-full border-blue-700/30 dark:border-highlight-blue/40 transition-colors duration-200 flex items-center justify-between px-1"
			>
				<div class="flex items-center w-full">
					<button
						class="flex items-center justify-center rounded-md hover:bg-blue-600/30 dark:hover:bg-highlight-blue/20 transition-colors group px-3 py-2"
						on:click={() => (isExpanded = !isExpanded)}
						type="button"
					>
						<svg
							class="text-blue-100 dark:text-highlight-blue duration-200 w-4 h-4 transition-colors"
							class:rotate-180={!isExpanded}
							xmlns="http://www.w3.org/2000/svg"
							fill="none"
							viewBox="0 0 24 24"
							stroke-width="1.5"
							stroke="currentColor"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="m18.75 4.5-7.5 7.5 7.5 7.5m-6-15L5.25 12l7.5 7.5"
							/>
						</svg>
					</button>
					{#if isExpanded}
						<button
							class="flex items-center justify-center rounded-md hover:bg-blue-600/30 dark:hover:bg-highlight-blue/20 transition-colors group px-3 py-2"
							title={isPinned ? 'Unpin menu' : 'Pin menu'}
							on:click={() => dispatch('pinToggle')}
							type="button"
						>
							{#if isPinned}
								<svg
									width="16"
									height="16"
									viewBox="0 0 24 24"
									fill="currentColor"
									xmlns="http://www.w3.org/2000/svg"
									class="w-4 h-4 text-blue-100 dark:text-highlight-blue"
								>
									<path
										d="M16 9V5a1 1 0 0 0-1-1H9a1 1 0 0 0-1 1v4l-4 4v2h6v5a1 1 0 0 0 2 0v-5h6v-2l-4-4z"
									/>
								</svg>
							{:else}
								<svg
									width="16"
									height="16"
									viewBox="0 0 24 24"
									fill="none"
									xmlns="http://www.w3.org/2000/svg"
									class="w-4 h-4 text-blue-100 dark:text-highlight-blue"
								>
									<path
										d="M16 9V5a1 1 0 0 0-1-1H9a1 1 0 0 0-1 1v4l-4 4v2h6v5a1 1 0 0 0 2 0v-5h6v-2l-4-4z"
										stroke="currentColor"
										stroke-width="2"
									/>
								</svg>
							{/if}
						</button>
					{/if}
				</div>
			</div>
		{/if}

		{#if isPinned}
			<button
				class="absolute top-2 z-10 flex items-center justify-center w-6 h-6 rounded-md bg-blue-600/30 dark:bg-gray-800/70 hover:bg-blue-600/50 dark:hover:bg-gray-700 transition-colors"
				class:right-5={hasScrollbar}
				class:right-2={!hasScrollbar}
				title="Unpin menu"
				on:click={() => dispatch('pinToggle')}
				type="button"
			>
				<svg
					width="16"
					height="16"
					viewBox="0 0 24 24"
					fill="none"
					xmlns="http://www.w3.org/2000/svg"
					class="w-4 h-4 text-blue-100 dark:text-highlight-blue"
				>
					<path
						d="M16 9V5a1 1 0 0 0-1-1H9a1 1 0 0 0-1 1v4l-4 4v2h6v5a1 1 0 0 0 2 0v-5h6v-2l-4-4z"
						stroke="currentColor"
						stroke-width="1.2"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
				</svg>
			</button>
		{/if}

		<!-- Navigation Items -->
		<div
			bind:this={menuItemsElement}
			class="flex flex-col flex-1 overflow-y-auto overflow-x-hidden [scrollbar-gutter:stable] {scrollBarClassesVertical} [&::-webkit-scrollbar-track]:bg-cta-blue dark:[&::-webkit-scrollbar-track]:bg-gray-800"
			class:py-4={!isPinned}
		>
			{#each menu as link}
				{#if link.type === 'submenu'}
					<ConditionalDisplay show={link.whitebox ? 'whitebox' : link.blackbox ? 'blackbox' : 'both'}>
					<div class="py-1 mt-4 first:mt-0">
						{#if isExpanded}
							<div
								class="px-3 py-2 text-xs font-semibold text-blue-100 dark:text-highlight-blue uppercase tracking-wider transition-colors duration-200"
							>
								{link.label}
							</div>
						{/if}

						<div>
							{#each link.items as item, i (i)}
								<ConditionalDisplay show={item.blackbox ? 'blackbox' : 'both'}>
									<a
										class="flex items-center py-2 text-sm transition-all duration-150 relative group
                                        {(
											item.route === '/dashboard/'
												? $page.url.pathname.startsWith('/dashboard')
												: $page.url.pathname === item.route
										)
											? 'text-white font-medium bg-active-blue dark:bg-active-blue shadow-md'
											: 'text-blue-100 dark:text-gray-200 hover:shadow-md hover:bg-highlight-blue/80 dark:hover:bg-highlight-blue/20 hover:text-white dark:hover:text-gray-100'}"
										class:hidden={shouldHideMenuItem(item.route)}
										class:px-3={isExpanded}
										class:justify-center={!isExpanded}
										draggable="false"
										href={item.route}
										title={item.label}
									>
										<!-- Icon -->
										<div class="flex-shrink-0 text-blue-100 dark:text-highlight-blue">
											{@html getIconForRoute(item.route)}
										</div>

										{#if isExpanded}
											<span class="ml-3 truncate">
												{#if i === 0}
													Overview
												{:else if item.singleLabel}
													{item.singleLabel}
												{:else}
													{item.label}
												{/if}
											</span>
										{/if}

										{#if item.route === '/dashboard/' ? $page.url.pathname.startsWith('/dashboard') : $page.url.pathname === item.route}
											<div
												class="absolute left-0 top-0 bottom-0 w-1 bg-white dark:bg-highlight-blue"
											></div>
										{/if}
									</a>
								</ConditionalDisplay>
							{/each}
						</div>
					</div>
					</ConditionalDisplay>
				{:else}
					<a
						class="flex items-center py-2 text-sm transition-all duration-150 relative group
                            {$page.url.pathname === link.route
							? 'text-white font-medium bg-active-blue dark:bg-active-blue shadow-md'
							: 'text-blue-100 dark:text-gray-200 hover:text-white dark:hover:text-gray-100 hover:bg-highlight-blue/80 dark:hover:bg-highlight-blue/20'}"
						class:px-3={isExpanded}
						class:justify-center={!isExpanded}
						draggable="false"
						href={link.route}
					>
						<!-- Icon -->
						<div class="flex-shrink-0 text-blue-100 dark:text-highlight-blue">
							{@html icons[link.label]}
						</div>

						{#if isExpanded}
							<span class="ml-3 truncate">{link.label}</span>
						{:else}
							<div
								class="absolute left-14 rounded bg-gray-900 dark:bg-gray-800 text-white dark:text-highlight-blue px-2 py-1 ml-6 text-sm
	                                invisible opacity-0 -translate-x-3 group-hover:visible group-hover:opacity-100 group-hover:translate-x-0
	                                transition-all duration-150 whitespace-nowrap z-50 shadow-lg border dark:border-highlight-blue/40"
							>
								{link.label}
							</div>
						{/if}

						{#if $page.url.pathname === link.route}
							<div class="absolute left-0 top-0 bottom-0 w-1 bg-white dark:bg-highlight-blue"></div>
						{/if}
					</a>
				{/if}
			{/each}
		</div>
	</nav>

	<!-- Main Content -->
	<div class="flex-1">
		<slot />
	</div>
</div>
