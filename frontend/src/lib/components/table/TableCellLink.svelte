<script>
	import { onMount } from 'svelte';

	export let href;
	export let title = '';
	/** on phones show the text on up to two lines; use only on the main name column */
	export let wrap = false;

	// the link text comes from the slot, so emptiness is read from the DOM
	let linkRef = null;
	let isEmpty = false;
	const checkEmpty = () => {
		isEmpty = !linkRef?.textContent?.trim();
	};

	onMount(() => {
		checkEmpty();
		const observer = new MutationObserver(checkEmpty);
		observer.observe(linkRef, { childList: true, characterData: true, subtree: true });
		return () => observer.disconnect();
	});
</script>

<td
	class="pl-4 font-regular text-slate-600 dark:text-gray-200 text-ellipsis whitespace-nowrap overflow-hidden pr-4 transition-colors duration-200 max-w-0"
	{title}
	class:pc-cell-wrap={wrap}
>
	<a
		{href}
		bind:this={linkRef}
		class="pc-cell-link block w-full py-1 text-ellipsis whitespace-nowrap overflow-hidden"
		class:hidden={isEmpty}
	>
		<slot />
	</a>
	{#if isEmpty}
		<span class="text-slate-400 dark:text-gray-500">-</span>
	{/if}
</td>
