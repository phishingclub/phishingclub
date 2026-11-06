<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

	// bind a element to this component input field
	// use like <Textarea bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	// placeholder
	export let placeholder = '';
	export let defaultValue = ''; // default checkbox value
	export let value = defaultValue; // for binding value
	export let toolTipText = '';
	export let readonly = false;
	export let resize = true;
	export let required = false;
	export let minLength = null;
	export let maxLength = null;
	export let id = null;
	export let fullWidth = false;
	export let height = 'small';

	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;
	export let optional = false;

	onMount(() => {
		value = value ?? defaultValue;
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

<label class="flex flex-col py-2 w-full" class:sm:w-60={!fullWidth}>
	<FieldLabel {toolTipText} {optional}><slot /></FieldLabel>
	<textarea
		{id}
		bind:this={bindTo}
		bind:value
		{required}
		minlength={minLength}
		maxlength={maxLength}
		{readonly}
		{placeholder}
		class="py-2 px-3 rounded-md border font-normal transition-colors duration-200 focus:outline-none focus:ring-2 {readonly
			? 'cursor-default bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border-dashed border-gray-300 dark:border-gray-600 focus:ring-gray-300/40 dark:focus:ring-gray-600/40'
			: 'bg-grayblue-light dark:bg-gray-900/60 text-slate-800 dark:text-gray-300 border-transparent dark:border-gray-700 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30'}"
		class:h-16={height === 'small'}
		class:h-28={height === 'medium'}
		class:h-48={height === 'large'}
		class:resize-none={!resize}
	/>
</label>
