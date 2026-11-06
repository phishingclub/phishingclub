<script>
	import { createEventDispatcher } from 'svelte';
	import Button from '../Button.svelte';

	export let count = 0;
	export let noun = 'item';
	export let nounPlural = `${noun}s`;

	const dispatch = createEventDispatcher();

	$: label = `${count} ${count === 1 ? noun : nounPlural} selected`;
</script>

{#if count > 0}
	<div
		class="flex flex-wrap items-center gap-x-4 gap-y-2 mb-4 px-4 py-3 rounded-md border-2 border-gray-200 dark:border-gray-700/60 bg-white dark:bg-gray-900/80 transition-colors duration-200"
	>
		<span class="font-medium text-slate-600 dark:text-gray-200 transition-colors duration-200">
			{label}
		</span>
		<div class="flex items-center gap-3 ml-auto">
			<Button size="small" variant="danger-outline" on:click={() => dispatch('delete')}>
				Delete
			</Button>
			<button
				type="button"
				class="pc-focus h-10 px-3 rounded-md text-sm font-semibold uppercase text-slate-600 dark:text-gray-300 hover:bg-grayblue-light dark:hover:bg-gray-800 transition-colors duration-200"
				on:click={() => dispatch('clear')}
			>
				Clear
			</button>
		</div>
	</div>
{/if}
