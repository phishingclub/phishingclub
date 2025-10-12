<script>
	import { page } from '$app/stores';
	import { menu, mobileTopMenu } from '$lib/consts/navigation';
	import MenuLink from './MenuLink.svelte';
	import { shouldHideMenuItem } from '$lib/utils/common';
	import ThemeToggle from '../ThemeToggle.svelte';

	export let visible = false;
	export let onClickLogout;
	export let toggleChangeCompanyModal;

	const icons = {
		dashboard: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
    </svg>`,

		campaigns_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M8 5h6c2 0 4 2 4 4v4c0 3-3 5-5 5 1.5-1.5 1.5-3 1.5-3" />
        <path stroke-linecap="round" stroke-linejoin="round" d="M14.5 15l-2 2" />
        <circle cx="14" cy="5" r="1" fill="currentColor" />
    </svg>`,
		campaign_templates: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z" />
    </svg>`,

		ip_filters: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M12 3c2.755 0 5.455.232 8.083.678.533.09.917.556.917 1.096v1.044a2.25 2.25 0 0 1-.659 1.591l-5.432 5.432a2.25 2.25 0 0 0-.659 1.591v2.927a2.25 2.25 0 0 1-1.244 2.013L9.75 21v-6.568a2.25 2.25 0 0 0-.659-1.591L3.659 7.409A2.25 2.25 0 0 1 3 5.818V4.774c0-.54.384-1.006.917-1.096A48.32 48.32 0 0 1 12 3Z" />
</svg>`,

		webhooks: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="m3.75 13.5 10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75Z" />
    </svg>`,

		recipients_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
</svg>`,

		recipient_groups: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M18 18.72a9.094 9.094 0 0 0 3.741-.479 3 3 0 0 0-4.682-2.72m.94 3.198.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0 1 12 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 0 1 6 18.719m12 0a5.971 5.971 0 0 0-.941-3.197m0 0A5.995 5.995 0 0 0 12 12.75a5.995 5.995 0 0 0-5.058 2.772m0 0a3 3 0 0 0-4.681 2.72 8.986 8.986 0 0 0 3.74.477m.94-3.197a5.971 5.971 0 0 0-.94 3.197M15 6.75a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm6 3a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Zm-13.5 0a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Z" />
    </svg>`,

		domains_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 0 0 8.716-6.747M12 21a9.004 9.004 0 0 1-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 0 1 7.843 4.582M12 3a8.997 8.997 0 0 0-7.843 4.582m15.686 0A11.953 11.953 0 0 1 12 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0 1 21 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0 1 12 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 0 1 3 12c0-1.605.42-3.113 1.157-4.418" />
    </svg>`,

		pages: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 0 0-3.375-3.375h-1.5A1.125 1.125 0 0 1 13.5 7.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 0 0-9-9Z" />
    </svg>`,

		assets: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 15.75 5.159-5.159a2.25 2.25 0 0 1 3.182 0l5.159 5.159m-1.5-1.5 1.409-1.409a2.25 2.25 0 0 1 3.182 0l2.909 2.909m-18 3.75h16.5a1.5 1.5 0 0 0 1.5-1.5V6a1.5 1.5 0 0 0-1.5-1.5H3.75A1.5 1.5 0 0 0 2.25 6v12a1.5 1.5 0 0 0 1.5 1.5Zm10.5-11.25h.008v.008h-.008V8.25Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z" />
    </svg>`,

		emails_overview: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
    </svg>`,

		attachments: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
        <path stroke-linecap="round" stroke-linejoin="round" d="m18.375 12.739-7.693 7.693a4.5 4.5 0 0 1-6.364-6.364l10.94-10.94A3 3 0 1 1 19.5 7.372L8.552 18.32m.009-.01-.01.01m5.699-9.941-7.81 7.81a1.5 1.5 0 0 0 2.112 2.13" />
    </svg>`,

		smtp_configurations: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M16.5 12a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0Zm0 0c0 1.657 1.007 3 2.25 3S21 13.657 21 12a9 9 0 1 0-2.636 6.364M16.5 12V8.25" />
</svg>`,

		api_senders: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5" />
</svg>`,

		proxy: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
</svg>`,

		// Top menu icons
		profile: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
</svg>`,

		sessions: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
</svg>`,

		users: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M18 18.72a9.094 9.094 0 0 0 3.741-.479 3 3 0 0 0-4.682-2.72m.94 3.198.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0 1 12 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 0 1 6 18.719m12 0a5.971 5.971 0 0 0-.941-3.197m0 0A5.995 5.995 0 0 0 12 12.75a5.995 5.995 0 0 0-5.058 2.772m0 0a3 3 0 0 0-4.681 2.72 8.986 8.986 0 0 0 3.74.477m.94-3.197a5.971 5.971 0 0 0-.94 3.197M15 6.75a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm6 3a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Zm-13.5 0a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Z" />
</svg>`,

		companies: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
</svg>`,

		settings: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a6.759 6.759 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" />
  <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
</svg>`,

		'user guide': `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 5.25h.008v.008H12v-.008Z" />
</svg>`,

		'change company': `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5 21 12m0 0-7.5 7.5M21 12H3" />
</svg>`
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
			'/api-sender/': 'api_senders',
			'/profile/': 'profile',
			'/sessions/': 'sessions',
			'/user/': 'users',
			'/company/': 'companies',
			'/settings/': 'settings'
		};

		return icons[iconMap[route] || 'dashboard'];
	};

	const getTopMenuIcon = (label) => {
		const iconMap = {
			Profile: 'profile',
			Sessions: 'sessions',
			Users: 'users',
			Companies: 'companies',
			Settings: 'settings',
			'User Guide': 'user guide',
			'Change Company': 'change company'
		};

		return icons[iconMap[label] || 'profile'];
	};
</script>

{#if visible}
	<!-- Overlay -->
	<button
		class="fixed inset-0 bg-black bg-opacity-50 z-40 cursor-default"
		on:click={() => (visible = false)}
		aria-label="Close mobile menu"
	></button>

	<!-- Mobile Menu -->
	<div
		class="mobile-menu-content fixed top-0 left-0 w-full h-full bg-gradient-to-b from-pc-darkblue to-slate-900 dark:from-gray-900 dark:to-gray-950 z-50 overflow-y-auto shadow-xl"
	>
		<!-- Header -->
		<div
			class="mobile-menu-header flex justify-between h-20 items-center bg-pc-darkblue/90 dark:bg-gray-900/90 backdrop-blur-sm px-6 border-b border-white/10 dark:border-gray-700/50"
		>
			<img class="w-40 h-auto" src="/logo-white.svg" alt="logo" />
			<div class="flex items-center gap-2">
				<div
					class="flex items-center justify-center w-12 h-12 rounded-lg hover:bg-white/10 dark:hover:bg-gray-600/30 transition-all duration-200"
				>
					<ThemeToggle />
				</div>
				<button
					class="flex items-center justify-center w-12 h-12 rounded-lg hover:bg-white/10 dark:hover:bg-gray-600/30 transition-all duration-200"
					on:click={() => (visible = false)}
				>
					<img class="w-6 h-6" src="/mob-menu-close.svg" alt="close mobile menu" />
				</button>
			</div>
		</div>

		<!-- Switch Company Section -->
		<div class="px-6 py-4 border-b border-white/20 dark:border-highlight-blue/30">
			<button
				class="flex items-center w-full py-3 px-4 text-white bg-active-blue dark:bg-highlight-blue/30 rounded-lg transition-all duration-200 hover:bg-active-blue/80 dark:hover:bg-highlight-blue/40 font-medium"
				on:click={() => {
					visible = false;
					toggleChangeCompanyModal();
				}}
			>
				<div class="flex-shrink-0 mr-3 text-white dark:text-highlight-blue">
					{@html getTopMenuIcon('Change Company')}
				</div>
				<span class="flex-1 text-left">Switch Company</span>
			</button>
		</div>

		<!-- Quick Actions Section -->
		<div class="p-6 border-b border-white/20 dark:border-highlight-blue/30">
			<h2
				class="text-white/80 dark:text-highlight-blue text-lg font-semibold uppercase tracking-wider mb-4 text-left"
			>
				Quick Access
			</h2>
			<div class="space-y-2">
				{#each mobileTopMenu as link}
					<a
						class="flex items-center w-full py-3 px-4 text-white text-lg font-medium rounded-lg transition-all duration-200 group {$page
							.url.pathname === link.route
							? 'bg-active-blue shadow-lg dark:bg-active-blue'
							: 'hover:bg-highlight-blue/30 hover:shadow-md dark:hover:bg-highlight-blue/20'}"
						class:hidden={shouldHideMenuItem(link.route)}
						on:click={() => (visible = false)}
						target={link.external ? '_blank' : '_self'}
						href={link.route}
					>
						<div class="flex-shrink-0 mr-3 text-white dark:text-highlight-blue">
							{@html getTopMenuIcon(link.label)}
						</div>
						<span class="flex-1 text-left">{link.label}</span>
						{#if $page.url.pathname === link.route}
							<div class="w-2 h-2 bg-white rounded-full"></div>
						{/if}
					</a>
				{/each}
			</div>
		</div>

		<!-- Main Navigation -->
		<div class="p-6">
			<h2
				class="text-white/80 dark:text-highlight-blue text-lg font-semibold uppercase tracking-wider mb-4 text-left"
			>
				Navigation
			</h2>
			<div class="space-y-1">
				{#each menu as link}
					{#if link.type === 'submenu'}
						<!-- section header -->
						<div class="pt-4 pb-2 first:pt-0">
							<div class="py-2 px-4 border-l-2 border-cta-blue/60 dark:border-highlight-blue/60">
								<h3
									class="text-white dark:text-highlight-blue font-bold text-lg uppercase tracking-wide text-left"
								>
									{link.label}
								</h3>
							</div>
						</div>
						<!-- submenu items -->
						<div class="ml-4 space-y-1">
							{#each link.items as item, i (i)}
								<a
									class="flex items-center w-full py-2.5 px-4 text-white/90 dark:text-gray-300 text-base font-medium rounded-lg transition-all duration-200 group hover:bg-highlight-blue/30 dark:hover:bg-highlight-blue/20 hover:text-white dark:hover:text-white"
									href={item.route}
									on:click={() => (visible = false)}
								>
									<div
										class="flex-shrink-0 mr-3 text-white/70 dark:text-highlight-blue/80 group-hover:text-white"
									>
										{@html getIconForRoute(item.route)}
									</div>
									<span class="text-left">
										{#if i === 0}
											Overview
										{:else if item.singleLabel}
											{item.singleLabel}
										{:else}
											{item.label}
										{/if}
									</span>
								</a>
							{/each}
						</div>
					{:else}
						<!-- standalone menu item -->
						<div class="pt-4 pb-2 first:pt-0">
							<a
								class="flex items-center w-full py-3 px-4 text-white text-lg font-bold rounded-lg transition-all duration-200 group hover:bg-highlight-blue/30 dark:hover:bg-highlight-blue/20"
								href={link.route}
								on:click={() => (visible = false)}
							>
								<div class="flex-shrink-0 mr-3 text-white dark:text-highlight-blue">
									{@html getIconForRoute(link.route)}
								</div>
								<span class="text-left">{link.label}</span>
							</a>
						</div>
					{/if}
				{/each}
			</div>
		</div>

		<!-- Logout Section -->
		<div class="p-6 border-t border-white/20 dark:border-highlight-blue/30 mt-auto">
			<button
				on:click={onClickLogout}
				class="w-full border-2 border-white/30 dark:border-highlight-blue/50 hover:border-highlight-blue/60 dark:hover:border-highlight-blue hover:bg-highlight-blue/20 dark:hover:bg-highlight-blue/20 uppercase font-medium py-2.5 px-6 rounded-lg transition-all duration-200 text-sm text-white/90 dark:text-highlight-blue hover:text-white dark:hover:text-white"
			>
				Log Out
			</button>
		</div>

		<!-- Footer spacer -->
		<div class="h-6"></div>
	</div>
{/if}

<style>
	/* ensure consistent background colors */
	.mobile-menu-content {
		background: linear-gradient(to bottom, #0b2063, #1e293b);
	}

	:global(.dark) .mobile-menu-content {
		background: linear-gradient(to bottom, #111827, #0f172a);
	}

	/* smooth scrolling */
	.mobile-menu-content {
		scroll-behavior: smooth;
	}

	/* hide scrollbar but keep functionality */
	.mobile-menu-content::-webkit-scrollbar {
		display: none;
	}
	.mobile-menu-content {
		-ms-overflow-style: none;
		scrollbar-width: none;
	}
</style>
