<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

	// bind a element to this component input field
	// use like <TextField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	// placeholder
	export let placeholder = '';
	export let defaultValue = ''; // default checkbox value
	export let value = defaultValue; // for binding value
	export let resets = true; // reset value on parent form reset
	export let toolTipText = '';
	export let optional = false;
	export let readonly = false;
	export let required = false;
	export let disabled = false;
	export let min = null;
	export let max = null;
	export let minLength = null;
	export let maxLength = null;
	export let width = 'medium';
	export let pattern = null;
	export let id = null;
	export let onBlur = () => {};
	// type can only be set initially
	export let type = 'text';
	let inputType = 'text';

	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;

	onMount(() => {
		value = value ?? defaultValue;
		inputType = type;
	});

	afterUpdate(() => {
		if (!parentForm && resets) {
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
		if (parentFormResetListener && resets) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});
</script>

<label class="flex flex-col py-2">
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
		{...{ type: inputType }}
		{id}
		bind:this={bindTo}
		bind:value
		autocomplete="off"
		title={value}
		on:click
		on:blur={onBlur}
		on:keyup
		on:keydown
		{min}
		{max}
		minlength={minLength}
		maxlength={maxLength}
		{disabled}
		{readonly}
		{required}
		{placeholder}
		{pattern}
		class="text-ellipsis row-start-1 row-span-3 justify-self-center rounded-md py-2 pl-2 text-gray-600 border border-transparent focus:outline-none focus:border-solid focus:border-slate-400 focus:bg-gray-100 bg-grayblue-light font-normal"
		class:w-24={width === 'small'}
		class:w-60={width === 'medium'}
		class:w-95={width === 'large'}
		class:w-full={width === 'full'}
	/>
</label>
