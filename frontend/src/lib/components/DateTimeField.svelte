<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';
	import { addToast } from '$lib/store/toast';

	// bind a element to this component input field
	// use like <DateTimeField bind:bindToDate={varYouWantToBindTheInputFieldTo} />
	export let bindToDate = null;
	// use like <DateTimeField bind:bindToTime={varYouWantToBindTheInputFieldTo} />
	export let bindToTime = null;
	export let defaultValue = ''; // default checkbox value
	// for binding value, however this will contain the local value
	// for the intial input, the value is expected in a UTC string format, so is converted to
	// a locale string for the input field. The consumer must convert the from local to UTC or etc
	// when using the value, but should not mutate this value.
	export let value = defaultValue;
	export let resets = true; // reset value on parent form reset
	export let onChange = (value) => {};
	export let toolTipText = '';
	export let optional = false;
	export let disabled = false;
	export let readonly = false;
	export let required = false;
	export let min = new Date();
	/** @type {'small'|'medium'|'large'} */
	export let labelWidth = 'large';
	let minDate = '';
	let minTime = '';

	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;
	let dateValue = '';
	let timeValue = '';

	$: {
		if (!!value) {
			let x = new Date(value);
			const mm = (x.getMonth() + 1).toString().padStart(2, '0');
			const dd = x.getDate().toString().padStart(2, '0');
			const yyyy = x.getFullYear();
			const hours = x.getHours().toString().padStart(2, '0');
			const minutes = x.getMinutes().toString().padStart(2, '0');
			const timeString = (dateValue = `${yyyy}-${mm}-${dd}`);
			timeValue = `${hours}:${minutes}`;
			value = x.toString();
		} else {
			value = null;
			dateValue = '';
			timeValue = '';
		}
	}
	$: {
		if (!!min) {
			minDate = `${min.getFullYear()}-${(min.getMonth() + 1).toString().padStart(2, '0')}-${min
				.getDate()
				.toString()
				.padStart(2, '0')}`;
			const hours = min.getHours().toString().padStart(2, '0');
			const minutes = min.getMinutes().toString().padStart(2, '0');
			minTime = `${hours}:${minutes}`;
			// if there selected value is a different date then remove the min time
			if (dateValue && new Date(dateValue).toDateString() !== min.toDateString()) {
				minTime = '';
			}
		}
	}

	afterUpdate(() => {
		// handle reset
		if (!parentForm && resets) {
			parentForm = bindToDate.closest('form');
			if (!parentForm) {
				return;
			}
			parentFormResetListener = parentForm.addEventListener('reset', (event) => {
				event.preventDefault();
				dateValue = '';
				timeValue = '';
				value = defaultValue;
			});
		}
	});

	onDestroy(() => {
		if (parentFormResetListener && resets) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});

	const onChangeDate = () => {
		setValue();
		onChange(value);
	};
	const onChangeTime = () => {
		setValue();
		onChange(value);
	};
	const setValue = () => {
		if (!dateValue || !timeValue) {
			return;
		}
		value = `${dateValue}T${timeValue}`;
	};
</script>

<label
	class="flex flex-col py-2"
	class:w-20={labelWidth === 'small'}
	class:w-32={labelWidth === 'medium'}
	class:w-60={labelWidth === 'large'}
>
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
</label>
<div class="flex flex-row">
	<input
		type="date"
		min={minDate}
		bind:this={bindToDate}
		bind:value={dateValue}
		on:change={onChangeDate}
		on:select={onChangeDate}
		{disabled}
		{readonly}
		{required}
		autocomplete="off"
		class="w-44 rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border border-transparent focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-gray-500 focus:bg-gray-100 dark:focus:bg-gray-600 bg-grayblue-light dark:bg-gray-700 font-normal transition-colors duration-200"
		class:opacity-90={readonly}
	/>
	<input
		type="time"
		min={minTime}
		bind:this={bindToTime}
		bind:value={timeValue}
		on:select={onChangeTime}
		on:change={onChangeTime}
		{disabled}
		{readonly}
		{required}
		autocomplete="off"
		class="ml-2 rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 text-center border border-transparent focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-gray-500 focus:bg-gray-100 dark:focus:bg-gray-600 bg-grayblue-light dark:bg-gray-700 font-normal transition-colors duration-200"
		class:bg-yellow-200={readonly}
		class:dark:bg-yellow-700={readonly}
	/>
</div>
