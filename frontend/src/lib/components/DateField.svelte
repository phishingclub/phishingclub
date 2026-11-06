<script context="module">
	let instanceCount = 0;
</script>

<script>
	import { afterUpdate, onDestroy } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';
	import { addToast } from '$lib/store/toast';
	import { utc_yyyy_mm_dd } from '$lib/utils/api-utils';

	// bind a element to this component input field
	// use like <DateField bind:bindToDate={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	export let defaultValue = null; // default checkbox value
	export let value = defaultValue; // for binding value - this will contain the ISO string
	export let resets = true; // reset value on parent form reset
	export let onChange = (value) => {};
	export let toolTipText = '';
	export let optional = false;
	export let disabled = false;
	export let required = false;
	export let min = '';
	export let noLabel = false;
	export let labelWidth = 'large';
	export let inputWidth = 'medium';
	export let textAlign = 'left';

	const labelId = `date-label-${++instanceCount}`;

	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;

	if (value instanceof Date) {
		value = utc_yyyy_mm_dd(value);
	}

	afterUpdate(() => {
		// handle reset
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

{#if !noLabel}
	<div
		class="flex flex-col py-2"
		class:w-20={labelWidth === 'small'}
		class:w-32={labelWidth === 'medium'}
		class:w-60={labelWidth === 'large'}
	>
		<FieldLabel id={labelId} {toolTipText} {optional}><slot /></FieldLabel>
	</div>
{:else}
	<span id={labelId} class="sr-only"><slot /></span>
{/if}
<div class="flex flex-row">
	<input
		type="date"
		{min}
		bind:this={bindTo}
		bind:value
		on:change={onChange}
		on:select={onChange}
		{required}
		{disabled}
		autocomplete="off"
		aria-labelledby={labelId}
		class="date-field-input rounded-md py-2 px-2 text-slate-800 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 border border-transparent dark:border-gray-700 font-normal transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 disabled:opacity-60 disabled:cursor-not-allowed"
		class:text-left={textAlign == 'left'}
		class:text-center={textAlign == 'center'}
		class:text-right={textAlign == 'right'}
		class:w-44={inputWidth === 'medium'}
		class:w-60={inputWidth === 'large'}
	/>
</div>
