<script>
	import { onMount } from 'svelte';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import General from './panels/General.svelte';
	import Access from './panels/Access.svelte';
	import Data from './panels/Data.svelte';
	import Reports from './panels/Reports.svelte';
	import RedTeam from './panels/RedTeam.svelte';
	import System from './panels/System.svelte';

	// Red Team panel is only relevant in red team phishing (blackbox) mode
	$: tabs = [
		{ id: 'general', label: 'General', component: General },
		{ id: 'access', label: 'Access', component: Access },
		{ id: 'data', label: 'Data', component: Data },
		{ id: 'reports', label: 'Reports', component: Reports },
		...($displayMode === DISPLAY_MODE.BLACKBOX
			? [{ id: 'redteam', label: 'Red Team', component: RedTeam }]
			: []),
		{ id: 'system', label: 'System', component: System }
	];

	let active = 'general';

	$: ActiveComponent = (tabs.find((t) => t.id === active) || tabs[0]).component;

	onMount(() => {
		const hash = window.location.hash.replace('#', '');
		if (tabs.some((t) => t.id === hash)) {
			active = hash;
		}
	});

	const selectTab = (id) => {
		active = id;
		window.location.hash = id;
	};
</script>

<HeadTitle title="Settings" />
<main class="pb-8">
	<Headline>Settings</Headline>

	<nav class="mt-4 mb-6 border-b border-gray-200 dark:border-gray-700">
		<div class="flex">
			{#each tabs as tab}
				<button
					on:click={() => selectTab(tab.id)}
					class="px-6 py-3 text-sm font-medium border-b-2 transition-colors
						{active === tab.id
						? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
						: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
				>
					{tab.label}
				</button>
			{/each}
		</div>
	</nav>

	<div class="max-w-7xl">
		<svelte:component this={ActiveComponent} />
	</div>
</main>
