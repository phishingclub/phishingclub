<script>
	import { onMount } from 'svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import JA4Builder from './panels/JA4Builder.svelte';
	import CalendarBuilder from './panels/CalendarBuilder.svelte';
	import GeoIP from './panels/GeoIP.svelte';

	const tabs = [
		{ id: 'calendar', label: 'Calendar Invitation Builder', component: CalendarBuilder },
		{ id: 'ja4', label: 'JA4 Fingerprint Builder', component: JA4Builder },
		{ id: 'geoip', label: 'GeoIP Lookup', component: GeoIP }
	];

	let active = 'calendar';

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

<HeadTitle title="Tools" />
<main class="pb-8">
	<Headline>Tools</Headline>

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
