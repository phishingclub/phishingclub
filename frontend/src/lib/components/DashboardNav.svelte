<script>
	import { page } from '$app/stores';
	import TabBar from './TabBar.svelte';
	import { displayMode, showsSimulation } from '$lib/store/displayMode';

	// the Programs tab is only shown in the modes that show programs
	$: items = [
		{ id: '/dashboard', label: 'Overview', href: '/dashboard' },
		{ id: '/dashboard/campaigns', label: 'Campaigns', href: '/dashboard/campaigns' },
		...(showsSimulation($displayMode)
			? [{ id: '/dashboard/programs', label: 'Programs', href: '/dashboard/programs' }]
			: []),
		{ id: '/dashboard/events', label: 'Events', href: '/dashboard/events' }
	];

	// normalize the path so a trailing slash still matches its tab
	$: currentPath = $page.url.pathname.replace(/\/$/, '') || '/';
</script>

<TabBar {items} active={currentPath} label="Dashboard" />
