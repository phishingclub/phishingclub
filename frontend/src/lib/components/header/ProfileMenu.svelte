<script>
	import { page } from '$app/stores';
	import { fade } from 'svelte/transition';
	import { topMenu } from '$lib/consts/navigation';
	import { shouldHideMenuItem } from '$lib/utils/common';
	import ConditionalDisplay from '../ConditionalDisplay.svelte';
	export let logout;
	export let visible = false;
	export let toggleChangeCompanyModal;

	const icons = {
		profile: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
</svg>`,

		sessions: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
</svg>`,

		users: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M18 18.72a9.094 9.094 0 0 0 3.741-.479 3 3 0 0 0-4.682-2.72m.94 3.198.001.031c0 .225-.012.447-.037.666A11.944 11.944 0 0 1 12 21c-2.17 0-4.207-.576-5.963-1.584A6.062 6.062 0 0 1 6 18.719m12 0a5.971 5.971 0 0 0-.941-3.197m0 0A5.995 5.995 0 0 0 12 12.75a5.995 5.995 0 0 0-5.058 2.772m0 0a3 3 0 0 0-4.681 2.72 8.986 8.986 0 0 0 3.74.477m.94-3.197a5.971 5.971 0 0 0-.94 3.197M15 6.75a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm6 3a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Zm-13.5 0a2.25 2.25 0 1 1-4.5 0 2.25 2.25 0 0 1 4.5 0Z" />
</svg>`,

		companies: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955c.44-.439 1.152-.439 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25" />
</svg>`,

		settings: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a6.759 6.759 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" />
  <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
</svg>`,

		tools: `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M11.42 15.17 17.25 21A2.652 2.652 0 0 0 21 17.25l-5.877-5.877M11.42 15.17l2.496-3.03c.317-.384.74-.626 1.208-.766M11.42 15.17l-4.655 5.653a2.548 2.548 0 1 1-3.586-3.586l6.837-5.63m5.108-.233c.55-.164 1.163-.188 1.743-.14a4.5 4.5 0 0 0 4.486-6.336l-3.276 3.277a3.004 3.004 0 0 1-2.25-2.25l3.276-3.276a4.5 4.5 0 0 0-6.336 4.486c.091 1.076-.071 2.264-.904 2.95l-.102.085m-1.745 1.437L5.909 7.5H4.5L2.25 3.75l1.5-1.5L7.5 4.5v1.409l4.26 4.26m-1.745 1.437 1.745-1.437m6.615 8.206L15.75 15.75M4.867 19.125h.008v.008h-.008v-.008Z" />
</svg>`,

		'user guide': `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 5.25h.008v.008H12v-.008Z" />
</svg>`,

		'change company': `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-4">
  <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5 21 12m0 0-7.5 7.5M21 12H3" />
</svg>`
	};

	const getTopMenuIcon = (label) => {
		const iconMap = {
			Profile: 'profile',
			Sessions: 'sessions',
			Users: 'users',
			Companies: 'companies',
			Settings: 'settings',
			Tools: 'tools',
			'User Guide': 'user guide',
			'Change Company': 'change company'
		};

		return icons[iconMap[label] || 'profile'];
	};

	const handleClickOutsideNavigation = (event) => {
		const profileMenuElement = document.getElementById('profile-menu');
		const profileToggleElement = document.getElementById('toggle-profile-menu');
		const clickOutsideMenu = profileMenuElement && !profileMenuElement.contains(event.target);
		const clickOutsideToggleButton =
			profileToggleElement && !profileToggleElement.contains(event.target);
		if (clickOutsideMenu && clickOutsideToggleButton) {
			visible = false;
		}
	};

	$: {
		if (visible) {
			// add event listener to listen for a click outside the nav element
			document.addEventListener('click', handleClickOutsideNavigation);
		} else {
			// remove event listener
			document.removeEventListener('click', handleClickOutsideNavigation);
		}
	}

	// Custom transition that only fades in
	function fadeIn(node, { duration = 150 }) {
		return {
			duration,
			css: (t) => `opacity: ${t}`
		};
	}
</script>

{#if visible}
	<nav
		id="profile-menu"
		class="lg:flex flex-col h-fit lg:col-start-10 lg:col-span-3 row-start-1 xl:col-start-11 xl:col-span-2 2xl:col-start-11 2xl:col-span-2 sticky top-20 z-30"
	>
		<div
			class="flex flex-col bg-gradient-to-b from-cta-blue to-indigo-500 dark:from-gray-800 dark:to-gray-700 rounded-md transition-colors duration-200 dark:border dark:border-highlight-blue/30"
			in:fadeIn={{ duration: 150 }}
		>
			<!-- Change Company at top with distinct styling -->
			<button
				class="flex items-center pl-5 py-3 text-white bg-active-blue dark:bg-slate-800 first:rounded-t-md transition-colors duration-200 hover:bg-active-blue/80 dark:hover:bg-slate-700 border-b border-active-blue/50 dark:border-slate-700"
				on:click={() => {
					visible = false;
					toggleChangeCompanyModal();
				}}
			>
				<div class="flex-shrink-0 mr-3 text-white dark:text-highlight-blue">
					{@html getTopMenuIcon('Change Company')}
				</div>
				<span class="font-medium">Switch Company</span>
			</button>

			{#each topMenu as item}
				<ConditionalDisplay show={item.blackbox ? 'blackbox' : 'both'}>
					<a
						class="flex items-center pl-5 py-2 text-white last:rounded-md first:rounded-t-md transition-colors duration-200 {$page
							.url.pathname === item.route
							? 'bg-active-blue shadow-md dark:bg-active-blue'
							: 'hover:shadow-md hover:bg-highlight-blue/80 dark:hover:bg-highlight-blue/20'}"
						class:hidden={shouldHideMenuItem(item.route)}
						target={item.external ? '_blank' : '_self'}
						draggable="false"
						on:click={() => {
							visible = false;
						}}
						href={item.route}
					>
						<div class="flex-shrink-0 mr-3 text-white dark:text-highlight-blue">
							{@html getTopMenuIcon(item.label)}
						</div>
						<span>{item.label}</span>
					</a>
				</ConditionalDisplay>
			{/each}
			<button
				on:click={logout}
				class="bg-white dark:bg-gray-800 dark:border dark:border-highlight-blue/40 uppercase font-bold hover:bg-cta-blue/10 dark:hover:bg-highlight-blue/20 py-2 mx-4 my-4 rounded-md transition-colors duration-200"
			>
				<p class="text-cta-blue dark:text-highlight-blue py px-8 transition-colors duration-200">
					Log Out
				</p>
			</button>
		</div>
	</nav>
{/if}
