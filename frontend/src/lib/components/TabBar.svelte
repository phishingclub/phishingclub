<script>
	import { createEventDispatcher, onMount, tick } from 'svelte';

	/**
	 * Tabs to show. Items with `href` render as links, others as buttons that
	 * dispatch `select` with the item id.
	 * @type {{ id: string, label: string, href?: string }[]}
	 */
	export let items = [];
	/** id of the active tab */
	export let active = '';
	/** accessible name for the tab navigation */
	export let label = 'Sections';

	const dispatch = createEventDispatcher();
	let scroller;

	// keep the active tab visible when the bar scrolls sideways on small screens.
	// The bar starts at a tab edge so no half cut tab shows on the left; a small
	// end spacer makes that position reachable when the active tab is near the end.
	let endPad = 0;
	let revealRun = 0;
	const revealActive = async () => {
		const run = ++revealRun;
		endPad = 0;
		await tick();
		if (run !== revealRun) return;
		const el = scroller?.querySelector('[data-active="true"]');
		if (!el || !scroller || scroller.scrollWidth <= scroller.clientWidth) return;
		const tabs = Array.from(scroller.querySelectorAll('[data-active]'));
		const box = scroller.getBoundingClientRect();
		const pos = (node) => scroller.scrollLeft + node.getBoundingClientRect().left - box.left;
		const activeEnd = pos(el) + el.getBoundingClientRect().width;
		// room kept free for the right fade, not needed after the last tab
		const room = scroller.clientWidth - (el === tabs[tabs.length - 1] ? 0 : 40);
		let target = 0;
		for (const tab of tabs) {
			const start = pos(tab);
			if (tab === el || activeEnd - start <= room) {
				target = start;
				break;
			}
		}
		endPad = Math.max(0, Math.ceil(target - (scroller.scrollWidth - scroller.clientWidth)));
		await tick();
		if (run !== revealRun) return;
		// scrollIntoView would also scroll the page
		scroller.scrollTo({ left: Math.max(0, target) });
	};

	// show fades at the edges while more tabs are hidden there
	let canScrollRight = false;
	let canScrollLeft = false;
	const updateFade = () => {
		if (!scroller) return;
		canScrollRight = scroller.scrollLeft + scroller.clientWidth < scroller.scrollWidth - 1;
		canScrollLeft = scroller.scrollLeft > 1;
	};

	$: active, revealActive().then(updateFade);
	$: items, tick().then(updateFade);
	onMount(() => {
		revealActive().then(updateFade);
		// tab widths change when the web font arrives
		document.fonts?.ready.then(() => revealActive().then(updateFade));
		const ro = new ResizeObserver(updateFade);
		if (scroller) ro.observe(scroller);
		return () => ro.disconnect();
	});

	const tabClass = (isActive) =>
		`pc-focus shrink-0 whitespace-nowrap px-4 sm:px-6 py-3 text-sm font-medium border-b-2 -mb-px rounded-t-md transition-colors ${
			isActive
				? 'border-cta-blue dark:border-highlight-blue text-cta-blue dark:text-highlight-blue'
				: 'border-transparent text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:border-gray-300 dark:hover:border-gray-600'
		}`;
</script>

<nav
	class="mt-4 mb-6 border-b border-gray-200 dark:border-gray-700 flex items-end justify-between gap-2 min-w-0"
	aria-label={label}
>
	<div class="relative flex min-w-0">
	<div
		bind:this={scroller}
		on:scroll={updateFade}
		class="tabbar-scroll flex min-w-0 overflow-x-auto overflow-y-hidden"
	>
		{#each items as item (item.id)}
			{#if item.href}
				<a
					href={item.href}
					class={tabClass(active === item.id)}
					data-active={active === item.id}
					aria-current={active === item.id ? 'page' : undefined}
				>
					{item.label}
				</a>
			{:else}
				<button
					type="button"
					class={tabClass(active === item.id)}
					data-active={active === item.id}
					aria-current={active === item.id ? 'true' : undefined}
					on:click={() => dispatch('select', item.id)}
				>
					{item.label}
				</button>
			{/if}
		{/each}
		{#if endPad}
			<div class="shrink-0" style="width: {endPad}px" aria-hidden="true"></div>
		{/if}
	</div>
	{#if canScrollLeft}
		<div
			class="pointer-events-none absolute inset-y-0 left-0 w-10 bg-gradient-to-r from-white dark:from-gray-900"
			aria-hidden="true"
		></div>
	{/if}
	{#if canScrollRight}
		<div
			class="pointer-events-none absolute inset-y-0 right-0 w-10 bg-gradient-to-l from-white dark:from-gray-900"
			aria-hidden="true"
		></div>
	{/if}
	</div>
	{#if $$slots.end}
		<!-- empty:hidden drops the box when the slot renders nothing -->
		<div class="shrink-0 empty:hidden">
			<slot name="end" />
		</div>
	{/if}
</nav>

<style>
	/* the global dark scrollbar rule must not bring the bar back */
	.tabbar-scroll {
		scrollbar-width: none !important;
	}
	.tabbar-scroll::-webkit-scrollbar {
		display: none !important;
	}
</style>
