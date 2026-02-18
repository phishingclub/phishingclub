<script>
	import { page } from '$app/stores';

	// determine active tab based on current route
	$: currentPath = $page.url.pathname;
</script>

<nav class="mb-6 border-b border-gray-200 dark:border-gray-700">
	<div class="flex gap-6">
		<a
			href="/dashboard"
			class="px-1 py-3 text-sm font-medium border-b-2 transition-colors
				{currentPath === '/dashboard'
				? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
				: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
		>
			Overview
		</a>
		<a
			href="/dashboard/campaigns"
			class="px-1 py-3 text-sm font-medium border-b-2 transition-colors
				{currentPath === '/dashboard/campaigns'
				? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
				: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
		>
			Campaigns
		</a>
		<a
			href="/dashboard/events"
			class="px-1 py-3 text-sm font-medium border-b-2 transition-colors
				{currentPath === '/dashboard/events'
				? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
				: 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
		>
			Events
		</a>
	</div>
</nav>
