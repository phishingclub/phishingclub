<script context="module">
	let instanceCount = 0;
</script>

<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';
	import { addToast } from '$lib/store/toast';
	import { local_yyyy_mm_dd } from '$lib/utils/api-utils';

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
	const labelId = `datetime-label-${++instanceCount}`;
	const editableClass =
		'bg-grayblue-light dark:bg-gray-900/60 text-slate-800 dark:text-gray-300 border-transparent dark:border-gray-700 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30';
	const readonlyClass =
		'cursor-default bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border-dashed border-gray-300 dark:border-gray-600 focus:ring-gray-300/40 dark:focus:ring-gray-600/40';
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
			const hours = x.getHours().toString().padStart(2, '0');
			const minutes = x.getMinutes().toString().padStart(2, '0');
			dateValue = local_yyyy_mm_dd(x);
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
			minDate = local_yyyy_mm_dd(min);
			const hours = min.getHours().toString().padStart(2, '0');
			const minutes = min.getMinutes().toString().padStart(2, '0');
			minTime = `${hours}:${minutes}`;
			// only keep the minimum time when the selected day is the min day.
			// both are local date strings. parsing a date only string with new Date
			// reads it as UTC and shifts the day for timezones behind UTC.
			if (dateValue && dateValue !== minDate) {
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

<div
	class="flex flex-col py-2"
	class:w-20={labelWidth === 'small'}
	class:w-32={labelWidth === 'medium'}
	class:w-60={labelWidth === 'large'}
>
	<FieldLabel id={labelId} {toolTipText} {optional}><slot /></FieldLabel>
</div>
<div class="flex flex-row flex-wrap gap-2">
	<span id="{labelId}-date" class="sr-only">date</span>
	<span id="{labelId}-time" class="sr-only">time</span>
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
		aria-labelledby="{labelId} {labelId}-date"
		class="date-input w-44 rounded-md py-2 px-2 border font-normal transition-colors duration-200 focus:outline-none focus:ring-2 disabled:opacity-60 disabled:cursor-not-allowed {readonly ? readonlyClass : editableClass}"
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
		aria-labelledby="{labelId} {labelId}-time"
		class="time-input text-center rounded-md py-2 px-2 border font-normal transition-colors duration-200 focus:outline-none focus:ring-2 disabled:opacity-60 disabled:cursor-not-allowed {readonly ? readonlyClass : editableClass}"
	/>
</div>
