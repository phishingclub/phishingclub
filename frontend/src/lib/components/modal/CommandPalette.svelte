<script>
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { menu, topMenu } from '$lib/consts/navigation';
	import { shouldHideMenuItem } from '$lib/utils/common';
	// external
	export let visible = false;
	export let toggleChangeCompanyModal = () => {};

	// local state
	let searchQuery = '';
	let filteredItems = [];
	let selectedIndex = 0;
	let searchInput;

	let allItems = [];

	// build searchable items from navigation
	const buildSearchableItems = () => {
		const items = [];

		// add all routes from menu structure
		menu.forEach((section) => {
			section.items.forEach((item) => {
				if (!shouldHideMenuItem(item)) {
					items.push({
						label: item.label,
						route: item.route,
						category: section.label
					});
				}
			});
		});

		// add top menu items
		topMenu.forEach((item) => {
			if (!shouldHideMenuItem(item)) {
				if (item.external) {
					items.push({
						label: item.label,
						url: item.route,
						category: 'Account',
						external: true
					});
				} else {
					items.push({
						label: item.label,
						route: item.route,
						category: 'Account'
					});
				}
			}
		});

		// add special pages not in main navigation
		items.push({
			label: 'Orphaned Recipients',
			route: '/recipient/orphaned/',
			category: 'Recipients'
		});

		items.push({
			label: 'Recipient Groups',
			route: '/recipient/group/',
			category: 'Recipients'
		});

		items.push({
			label: 'System Update',
			route: '/settings/update/',
			category: 'Settings'
		});

		// add actions (not navigation)
		items.push({
			label: 'Switch Company',
			action: 'changeCompany',
			category: 'Account'
		});

		// add development mode links (open in new window)
		if (import.meta.env.DEV) {
			items.push({
				label: 'Database',
				url: 'http://localhost:8101',
				category: 'Development',
				external: true
			});

			items.push({
				label: 'Mailbox',
				url: 'http://localhost:8102',
				category: 'Development',
				external: true
			});

			items.push({
				label: 'Logs',
				url: 'http://localhost:8103',
				category: 'Development',
				external: true
			});

			items.push({
				label: 'Stats',
				url: 'http://localhost:8104',
				category: 'Development',
				external: true
			});
		}

		return items;
	};

	// filter items based on search query
	const filterItems = (query) => {
		if (!query.trim()) {
			return allItems.slice(0, 10); // show first 10 items when no query
		}

		const lowercaseQuery = query.toLowerCase();
		const filtered = allItems.filter(
			(item) =>
				item.label.toLowerCase().includes(lowercaseQuery) ||
				item.category.toLowerCase().includes(lowercaseQuery)
		);

		// sort by relevance - exact matches first, then starts with, then contains
		return filtered
			.sort((a, b) => {
				const aLabel = a.label.toLowerCase();
				const bLabel = b.label.toLowerCase();

				if (aLabel === lowercaseQuery) return -1;
				if (bLabel === lowercaseQuery) return 1;
				if (aLabel.startsWith(lowercaseQuery)) return -1;
				if (bLabel.startsWith(lowercaseQuery)) return 1;
				return 0;
			})
			.slice(0, 10); // limit to 10 results
	};

	// handle keyboard events
	const handleKeydown = (e) => {
		if (!visible) return;

		switch (e.key) {
			case 'Escape':
				close();
				break;
			case 'ArrowDown':
				e.preventDefault();
				selectedIndex = Math.min(selectedIndex + 1, filteredItems.length - 1);
				break;
			case 'ArrowUp':
				e.preventDefault();
				selectedIndex = Math.max(selectedIndex - 1, 0);
				break;
			case 'Enter':
				e.preventDefault();
				if (filteredItems[selectedIndex]) {
					navigateToItem(filteredItems[selectedIndex]);
				}
				break;
		}
	};

	// global keyboard shortcut handler
	const handleGlobalKeydown = (e) => {
		if (e.ctrlKey && e.key === 'p') {
			e.preventDefault();
			open();
		}
	};

	// navigate to selected item or execute action
	const navigateToItem = (item) => {
		if (item.route) {
			goto(item.route);
			close();
		} else if (item.action === 'changeCompany') {
			toggleChangeCompanyModal();
			close();
		} else if (item.external && item.url) {
			window.open(item.url, '_blank');
			close();
		}
	};

	// open command palette
	const open = () => {
		visible = true;
		allItems = buildSearchableItems();
		searchQuery = '';
		filteredItems = filterItems('');
		selectedIndex = 0;

		// focus search input after DOM update
		setTimeout(() => {
			if (searchInput) {
				searchInput.focus();
			}
		}, 10);
	};

	// close command palette
	const close = () => {
		visible = false;
		searchQuery = '';
		selectedIndex = 0;
	};

	// handle search input changes
	$: {
		filteredItems = filterItems(searchQuery);
		selectedIndex = Math.min(selectedIndex, Math.max(0, filteredItems.length - 1));
	}

	onMount(() => {
		// add global keyboard listener
		document.addEventListener('keydown', handleGlobalKeydown);
	});

	onDestroy(() => {
		document.removeEventListener('keydown', handleGlobalKeydown);
	});
</script>

{#if visible}
	<!-- svelte-ignore a11y-click-events-have-key-events -->
	<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
	<div
		class="fixed inset-0 bg-black bg-opacity-50 backdrop-blur-sm z-50 flex items-start justify-center pt-20"
		on:click|self={close}
		on:keydown={handleKeydown}
		role="dialog"
		aria-modal="true"
		aria-label="Command palette"
		tabindex="-1"
	>
		<div
			class="bg-white dark:bg-gray-800 rounded-lg shadow-2xl w-full max-w-lg mx-4 overflow-hidden transition-colors duration-200"
			role="presentation"
		>
			<!-- search input -->
			<div class="p-4 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200">
				<input
					bind:this={searchInput}
					bind:value={searchQuery}
					type="text"
					placeholder="search pages..."
					class="w-full px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400 transition-colors duration-200"
				/>
			</div>

			<!-- results list -->
			<div class="max-h-96 overflow-y-auto">
				{#if filteredItems.length === 0}
					<div
						class="p-4 text-center text-gray-500 dark:text-gray-400 transition-colors duration-200"
					>
						no pages found
					</div>
				{:else}
					{#each filteredItems as item, index}
						<button
							type="button"
							class="w-full px-4 py-3 text-left hover:bg-gray-50 dark:hover:bg-gray-700 flex items-center justify-between transition-colors duration-200 {selectedIndex ===
							index
								? 'bg-blue-50 dark:bg-blue-900/30 border-r-2 border-blue-500'
								: ''}"
							on:click={() => navigateToItem(item)}
							on:mouseenter={() => (selectedIndex = index)}
						>
							<div class="flex flex-col">
								<span
									class="text-gray-900 dark:text-gray-100 font-medium transition-colors duration-200"
								>
									{item.label}
									{#if item.external}
										<span class="text-xs text-gray-500 dark:text-gray-400 ml-1">↗</span>
									{/if}
								</span>
								<span
									class="text-sm text-gray-500 dark:text-gray-400 transition-colors duration-200"
								>
									{item.category}
								</span>
							</div>
						</button>
					{/each}
				{/if}
			</div>
		</div>
	</div>
{/if}
