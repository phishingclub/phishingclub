<script>
	import { createEventDispatcher } from 'svelte';

	/** @type {'none'|'some'|'all'} */
	export let state = 'none';
	// non-interactive placeholder used while the table skeleton is showing, so
	// the checkbox column keeps its width and the columns do not shift on load
	export let disabled = false;

	const dispatch = createEventDispatcher();

	// clicking selects every page row unless they are all selected already
	const onToggle = () => {
		if (disabled) {
			return;
		}
		dispatch('toggleAll', state !== 'all');
	};
</script>

<th
	class="pl-4 w-12 bg-grayblue-light dark:bg-gray-800/60 py-4 border-hidden rounded-tl-lg rounded-bl-lg transition-colors duration-200"
>
	<label
		class="relative inline-flex items-center"
		class:cursor-pointer={!disabled}
		class:opacity-0={disabled}
	>
		<input
			type="checkbox"
			class="peer sr-only"
			checked={state === 'all'}
			{disabled}
			on:change={onToggle}
			tabindex={disabled ? -1 : 0}
		/>
		<div
			class="w-5 h-5 border-2 border-slate-300 dark:border-gray-700/60 rounded
				peer-checked:border-cta-blue dark:peer-checked:border-highlight-blue/80 peer-checked:bg-cta-blue dark:peer-checked:bg-highlight-blue/80
				peer-focus:border-slate-400 dark:peer-focus:border-highlight-blue/80
				transition-all duration-200 ease-in-out
				flex items-center justify-center
				bg-slate-50 dark:bg-gray-900/60"
			class:!bg-cta-blue={state === 'some'}
			class:!border-cta-blue={state === 'some'}
			class:dark:!bg-highlight-blue={state === 'some'}
		>
			{#if state === 'all'}
				<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
				</svg>
			{:else if state === 'some'}
				<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 12h14" />
				</svg>
			{/if}
		</div>
	</label>
</th>
