<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

	// bind a element to this component input field
	// use like <TextField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	// placeholder
	export let placeholder = '';
	export let resets = true; // reset value on parent form reset
	export let toolTipText = '';
	export let optional = false;
	export let required = false;
	export let multiple = false;
	export let name = 'files';
	export let accept = '*';
	export let disabled = false;
	// pages look up the input by this id, so the default stays 'files'
	export let id = 'files';
	// fills the column on every screen size instead of a fixed width from sm up
	export let fullWidth = false;
	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;

	onMount(() => {});

	afterUpdate(() => {
		if (!parentForm && resets) {
			parentForm = bindTo.closest('form');
			if (!parentForm) {
				return;
			}
			parentFormResetListener = parentForm.addEventListener('reset', (event) => {
				event.preventDefault();
			});
		}
	});

	onDestroy(() => {
		if (parentFormResetListener && resets) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});
</script>

<label class="flex flex-col py-2 w-full" class:sm:w-60={!fullWidth}>
	{#if $$slots.default || toolTipText.length > 0 || optional === true}
		<FieldLabel {toolTipText} {optional}><slot /></FieldLabel>
	{/if}
	<input
		{id}
		type="file"
		{name}
		{accept}
		bind:this={bindTo}
		on:change
		autocomplete="off"
		{multiple}
		{required}
		{placeholder}
		{disabled}
		class="w-full min-w-0 text-sm rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900/60 text-gray-700 dark:text-gray-300 p-1.5 cursor-pointer transition-colors duration-200
			file:mr-3 file:px-4 file:py-1.5 file:rounded-md file:border-0 file:text-sm file:font-semibold file:text-white file:bg-cta-blue hover:file:bg-active-blue file:cursor-pointer
			focus:outline-none focus-visible:border-cta-blue focus-visible:ring-2 focus-visible:ring-cta-blue/30 dark:focus-visible:border-highlight-blue dark:focus-visible:ring-highlight-blue/30
			disabled:opacity-50 disabled:cursor-not-allowed file:disabled:cursor-not-allowed"
	/>
</label>
