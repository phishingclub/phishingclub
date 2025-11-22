<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

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

<label class="flex flex-col py-2 w-56">
	<div class="flex items-center">
		<p class="font-semibold text-slate-600 dark:text-gray-300 py-2 transition-colors duration-200">
			<slot />
		</p>
		{#if toolTipText.length > 0}
			<ToolTip>
				{toolTipText}
			</ToolTip>
		{/if}
		{#if optional === true}
			<div
				class="bg-gray-100 dark:bg-gray-700 ml-2 px-2 rounded-md transition-colors duration-200 h-6 flex items-center"
			>
				<p class="text-slate-600 dark:text-gray-300 text-xs">optional</p>
			</div>
		{/if}
	</div>
	<input
		id="files"
		type="file"
		{name}
		{accept}
		bind:this={bindTo}
		on:change
		autocomplete="off"
		{multiple}
		{required}
		{placeholder}
		class="border-solid border-2 border-gray-300 dark:border-gray-600 py-2 px-2 rounded-md file:px-4 file:py-2 file:text-white file:cursor-pointer file:text-sm file:font-semibold bg-white dark:bg-gray-700 file:bg-cta-blue hover:cursor-pointer file:hover:bg-blue-600 dark:file:bg-indigo-600 dark:file:hover:bg-indigo-700 file:border-hidden file:rounded-md text-gray-900 dark:text-white transition-colors duration-200"
	/>
</label>
