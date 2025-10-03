<script>
	import { afterUpdate, onDestroy } from 'svelte';
	import ToolTip from './ToolTip.svelte';
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
	<label
		class="flex flex-col py-2"
		class:w-20={labelWidth === 'small'}
		class:w-32={labelWidth === 'medium'}
		class:w-60={labelWidth === 'large'}
	>
		<div class="flex items-center">
			<p
				class="font-semibold text-slate-600 dark:text-gray-300 py-2 transition-colors duration-200"
			>
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
					<p class="text-slate-600 dark:text-gray-300 text-xs transition-colors duration-200">
						optional
					</p>
				</div>
			{/if}
		</div>
	</label>
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
		class="rounded-md text-center py-2 pl-2 text-gray-600 dark:text-gray-200 border border-transparent dark:border-gray-600 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-blue-500 focus:bg-gray-100 dark:focus:bg-gray-600 bg-grayblue-light dark:bg-gray-700 font-normal transition-colors duration-200"
		class:text-left={textAlign == 'left'}
		class:text-center={textAlign == 'center'}
		class:text-right={textAlign == 'right'}
		class:w-30={inputWidth === 'small'}
		class:w-44={inputWidth === 'medium'}
		class:w-60={inputWidth === 'large'}
	/>
</div>
