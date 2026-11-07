<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

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
	export let error = false;
	// id of the help or error text that describes the field
	export let ariaDescribedBy = null;
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

<label
	class="flex flex-col py-2"
	class:w-full={width !== 'small'}
	class:sm:w-auto={width === 'medium' || width === 'large'}
>
	<FieldLabel {toolTipText} {optional}><slot /></FieldLabel>
	<input
		{...{ type: inputType }}
		{id}
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={ariaDescribedBy || undefined}
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
		class="text-ellipsis rounded-md py-2 px-3 border font-normal transition-colors duration-200 focus:outline-none focus:ring-2 disabled:opacity-60 disabled:cursor-not-allowed {readonly
			? 'cursor-default bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border-dashed border-gray-300 dark:border-gray-600 focus:ring-gray-300/40 dark:focus:ring-gray-600/40'
			: 'bg-grayblue-light dark:bg-gray-900/60 text-slate-800 dark:text-gray-300'} {error
			? 'border-red-500 dark:border-red-400 focus:border-red-500 focus:ring-red-500/30 dark:focus:ring-red-400/30'
			: readonly
				? ''
				: 'border-transparent dark:border-gray-700 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30'}"
		class:w-24={width === 'small'}
		class:w-full={width === 'full' || width === 'large' || width === 'medium'}
		class:sm:w-60={width === 'medium'}
		class:sm:w-80={width === 'large'}
		class:max-w-full={width === 'large'}
		class:readonly
	/>
</label>
