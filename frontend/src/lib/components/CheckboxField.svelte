<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

	export let bindTo = null;
	export let defaultValue = false;
	export let value = defaultValue;
	export let toolTipText = '';
	export let optional = false;
	export let id = null;
	export let inline = false;
	export let disabled = false;

	let parentForm = null;
	let parentFormResetListener = null;

	onMount(() => {
		if (value === null) {
			value = defaultValue;
		}
	});

	afterUpdate(() => {
		if (!parentForm) {
			parentForm = bindTo.closest('form');
			if (!parentForm) {
				return;
			}
			parentFormResetListener = parentForm.addEventListener('reset', (event) => {
				event.preventDefault();
				value = defaultValue;
			});
		}
	});

	onDestroy(() => {
		if (parentFormResetListener) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});
</script>

<label
	class="flex py-2 w-full sm:w-60"
	class:flex-col={!inline}
	class:flex-row={inline}
	class:items-center={inline}
	class:opacity-50={disabled}
	class:cursor-not-allowed={disabled}
>
	<FieldLabel {toolTipText} {optional}><slot /></FieldLabel>
	<span class="block mt-1" class:mt-0={inline} class:ml-3={inline}>
		<span
			class="relative inline-flex items-center"
			class:cursor-pointer={!disabled}
			class:cursor-not-allowed={disabled}
		>
			<input
				{id}
				type="checkbox"
				class="peer sr-only"
				bind:this={bindTo}
				bind:checked={value}
				on:change
				{disabled}
			/>
			<span
				class="w-5 h-5 border-2 rounded flex items-center justify-center transition-all duration-200 ease-in-out
					border-slate-300 dark:border-gray-600 bg-slate-50 dark:bg-gray-900/60
					peer-checked:border-cta-blue peer-checked:bg-cta-blue dark:peer-checked:border-highlight-blue dark:peer-checked:bg-highlight-blue
					peer-focus-visible:ring-2 peer-focus-visible:ring-cta-blue peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-white
					dark:peer-focus-visible:ring-highlight-blue dark:peer-focus-visible:ring-offset-gray-800"
				aria-hidden="true"
			>
				{#if value}
					<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="3"
							d="M5 13l4 4L19 7"
						/>
					</svg>
				{/if}
			</span>
		</span>
	</span>
</label>
