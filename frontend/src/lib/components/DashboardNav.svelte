<script>
	import { page } from '$app/stores';
	import TabBar from './TabBar.svelte';

	const items = [
		{ id: '/dashboard', label: 'Overview', href: '/dashboard' },
		{ id: '/dashboard/campaigns', label: 'Campaigns', href: '/dashboard/campaigns' },
		{ id: '/dashboard/events', label: 'Events', href: '/dashboard/events' }
	];

	// normalize the path so a trailing slash still matches its tab
	$: currentPath = $page.url.pathname.replace(/\/$/, '') || '/';
</script>

<TabBar {items} active={currentPath} label="Dashboard" />
