<script>
	import { onMount } from 'svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import JA4Builder from './panels/JA4Builder.svelte';
	import CalendarBuilder from './panels/CalendarBuilder.svelte';
	import GeoIP from './panels/GeoIP.svelte';
	import ASN from './panels/ASN.svelte';

	const tabs = [
		{
			id: 'calendar',
			label: 'Calendar Invitation',
			component: CalendarBuilder,
			doc: ['attachments', 'calendar-invites']
		},
		{ id: 'ja4', label: 'JA4 Fingerprint', component: JA4Builder, doc: ['filtering', 'ja4-fingerprints'] },
		{ id: 'geoip', label: 'Geo IP Lookup', component: GeoIP, doc: ['filtering', 'geoip-asn'] },
		{ id: 'asn', label: 'ASN Lookup', component: ASN, doc: ['filtering', 'geoip-asn'] }
	];

	let active = 'calendar';

	$: activeTab = tabs.find((t) => t.id === active) || tabs[0];
	$: ActiveComponent = activeTab.component;

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
	<!-- the guide has no tools page, so help links to where each tool is described -->
	<Headline docSlug={activeTab.doc[0]} docAnchor={activeTab.doc[1]}>Tools</Headline>

	<TabBar items={tabs} {active} label="Tools" on:select={(e) => selectTab(e.detail)} />

	<div>
		<svelte:component this={ActiveComponent} />
	</div>
</main>
