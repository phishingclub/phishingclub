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
	export let bgColor = '';
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
		<p class="font-semibold text-slate-600 py-2">
			<slot />
		</p>
		{#if toolTipText.length > 0}
			<ToolTip>
				{toolTipText}
			</ToolTip>
		{/if}
		{#if optional === true}
			<div class="bg-gray-100 ml-2 px-2 rounded-md">
				<p class="text-slate-600 text-xs">optional</p>
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
		class="border-solid border-2 py-2 px-2 rounded-md file:px-4 file:py-2 file:text-white file:cursor-pointer file:text-sm file:font-semibold bg-white file:bg-cta-green hover:cursor-pointer file:hover:bg-teal-300 file:border-hidden file:rounded-md"
	/>
</label>
