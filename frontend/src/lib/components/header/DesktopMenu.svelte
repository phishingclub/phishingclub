<script>
	import { menu } from '$lib/consts/navigation';
	import { page } from '$app/stores';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';
	import { shouldHideMenuItem } from '$lib/utils/common';
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import { beforeNavigate } from '$app/navigation';

	let isExpanded = false;
	let menuElement;
	let instantCollapse = false;
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

		// handle click outside to collapse menu
		const handleClickOutside = (event) => {
			if (isExpanded && menuElement && !menuElement.contains(event.target)) {
				isExpanded = false;
			}
		};

		document.addEventListener('click', handleClickOutside);

		return () => {
			unsub();
			document.removeEventListener('click', handleClickOutside);
		};
	});

	// handle navigation to collapse menu
	beforeNavigate(() => {
		if (isExpanded) {
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

	const icons = {
		dashboard: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
    </svg>`,

		campaigns_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M8 5h6c2 0 4 2 4 4v4c0 3-3 5-5 5 1.5-1.5 1.5-3 1.5-3" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M14.5 15l-2 2" />
        <circle cx="14" cy="5" r="1" fill="currentColor" />
    </svg>`,
		campaign_templates: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z" />
    </svg>`,

		ip_filters: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
  <path stroke-linecap="round" stroke-linejoin="round" d="M12 3c2.755 0 5.455.232 8.083.678.533.09.917.556.917 1.096v1.044a2.25 2.25 0 0 1-.659 1.591l-5.432 5.432a2.25 2.25 0 0 0-.659 1.591v2.927a2.25 2.25 0 0 1-1.244 2.013L9.75 21v-6.568a2.25 2.25 0 0 0-.659-1.591L3.659 7.409A2.25 2.25 0 0 1 3 5.818V4.774c0-.54.384-1.006.917-1.096A48.32 48.32 0 0 1 12 3Z" />
</svg>
`,

		webhooks: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="m3.75 13.5 10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75Z" />
    </svg>`,

		recipients_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
  <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
</svg>`,

		recipient_groups: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M18 18.72a9.094 9.094 0 0 0 3.741-.479 3 3 0 0 0-4.682-2.72m.94 3.198.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0 1 12 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 0 1 6 18.719m12 0a5.971 5.971 0 0 0-.941-3.197m0 0A5.995 5.995 0 0 0 12 12.75a5.995 5.995 0 0 0-5.058 2.772m0 0a3 3 0 0 0-4.681 2.72 8.986 8.986 0 0 0 3.74.477m.94-3.197a5.971 5.971 0 0 0-.94 3.197M15 6.75a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm6 3a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Zm-13.5 0a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Z" />
    </svg>`,

		domains_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 0 0 8.716-6.747M12 21a9.004 9.004 0 0 1-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 0 1 7.843 4.582M12 3a8.997 8.997 0 0 0-7.843 4.582m15.686 0A11.953 11.953 0 0 1 12 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0 1 21 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0 1 12 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 0 1 3 12c0-1.605.42-3.113 1.157-4.418" />
    </svg>`,

		pages: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z" />
    </svg>`,

		assets: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 15.75 5.159-5.159a2.25 2.25 0 0 1 3.182 0l5.159 5.159m-1.5-1.5 1.409-1.409a2.25 2.25 0 0 1 3.182 0l2.909 2.909m-18 3.75h16.5a1.5 1.5 0 0 0 1.5-1.5V6a1.5 1.5 0 0 0-1.5-1.5H3.75A1.5 1.5 0 0 0 2.25 6v12a1.5 1.5 0 0 0 1.5 1.5Zm10.5-11.25h.008v.008h-.008V8.25Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z" />
    </svg>`,

		emails_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
    </svg>`,

		attachments: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
        <path stroke-linecap="round" stroke-linejoin="round" d="m18.375 12.739-7.693 7.693a4.5 4.5 0 0 1-6.364-6.364l10.94-10.94A3 3 0 1 1 19.5 7.372L8.552 18.32m.009-.01-.01.01m5.699-9.941-7.81 7.81a1.5 1.5 0 0 0 2.112 2.13" />
    </svg>`,

		smtp_configurations: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
  <path stroke-linecap="round" stroke-linejoin="round" d="M16.5 12a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0Zm0 0c0 1.657 1.007 3 2.25 3S21 13.657 21 12a9 9 0 1 0-2.636 6.364M16.5 12V8.25" />
</svg>
`,

		api_senders: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
  <path stroke-linecap="round" stroke-linejoin="round" d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5" />
</svg>
`,

		proxy: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-6">
  <path stroke-linecap="round" stroke-linejoin="round" d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
</svg>
`
	};

	const getIconForRoute = (route) => {
		const iconMap = {
			'/dashboard/': 'dashboard',
			'/campaign/': 'campaigns_overview',
			'/campaign-template/': 'campaign_templates',
			'/ip-filter/': 'ip_filters',
			'/webhook/': 'webhooks',
			'/recipient/': 'recipients_overview',
			'/recipient/group/': 'recipient_groups',
			'/domain/': 'domains_overview',
			'/page/': 'pages',
			'/proxy/': 'proxy',
			'/asset/': 'assets',
			'/email/': 'emails_overview',
			'/attachment/': 'attachments',
			'/smtp-configuration/': 'smtp_configurations',
			'/api-sender/': 'api_senders'
		};

		return icons[iconMap[route] || 'dashboard']; // fallback to dashboard if route not found
	};
</script>

<div class="flex">
	<nav
		bind:this={menuElement}
		class="hidden lg:flex flex-col fixed top-16 z-10 bg-gradient-to-b from-pc-darkblue to-indigo-400 dark:from-gray-900 dark:to-gray-800 rounded-br-lg overflow-y-auto overflow-x-hidden min-h-0 max-h-[calc(100vh-4rem)] box-content border-r-[1px] border-pc-darkblue dark:border-highlight-blue/40"
		class:transition-all={!instantCollapse}
		class:w-40={isExpanded}
		class:w-12={!isExpanded}
		class:!top-[89px]={hasCompanySelected}
		class:!max-h-[calc(100vh-6rem)]={hasCompanySelected}
	>
		<div
			class="sticky top-0 bg-highlight-blue/20 dark:bg-gray-800/70 border-b w-full border-blue-700/30 dark:border-highlight-blue/40 transform-none transition-colors duration-200"
		>
			<button
				class="w-full flex items-center justify-center rounded-md hover:bg-blue-600/30 dark:hover:bg-highlight-blue/20 transition-colors group px-3 py-2"
				on:click={() => (isExpanded = !isExpanded)}
			>
				<svg
					class="text-blue-100 dark:text-highlight-blue duration-200 w-6 transition-colors"
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
		</div>

		<!-- Navigation Items -->
		<div
			class="flex flex-col py-4 flex-1 overflow-y-auto {scrollBarClassesVertical} [&::-webkit-scrollbar-track]:bg-cta-blue dark:[&::-webkit-scrollbar-track]:bg-gray-800"
		>
			{#each menu as link}
				{#if link.type === 'submenu'}
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
								<a
									class="flex items-center px-3 py-2 text-sm transition-all duration-150 relative group
                                        {$page.url.pathname === item.route
										? 'text-white font-medium bg-active-blue dark:bg-active-blue shadow-md'
										: 'text-blue-100 dark:text-gray-200 hover:shadow-md hover:bg-highlight-blue/80 dark:hover:bg-highlight-blue/20 hover:text-white dark:hover:text-gray-100'}"
									class:hidden={shouldHideMenuItem(item.route)}
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

									{#if $page.url.pathname === item.route}
										<div
											class="absolute left-0 top-0 bottom-0 w-1 bg-white dark:bg-highlight-blue"
										></div>
									{/if}
								</a>
							{/each}
						</div>
					</div>
				{:else}
					<a
						class="flex items-center px-3 py-2 text-sm transition-all duration-150 relative group
                            {$page.url.pathname === link.route
							? 'text-white font-medium bg-active-blue dark:bg-active-blue shadow-md'
							: 'text-blue-100 dark:text-gray-200 hover:text-white dark:hover:text-gray-100 hover:bg-highlight-blue/80 dark:hover:bg-highlight-blue/20'}"
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
								class="absolute left-14 rounded bg-gray-900 dark:bg-gray-800 dark:border-highlight-blue/40 text-white dark:text-highlight-blue px-2 py-1 ml-6 text-sm
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
