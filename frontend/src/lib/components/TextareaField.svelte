<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

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
	let showToolTip = false;
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

<label class="flex flex-col py-2 w-60" class:w-full={fullWidth}>
	<div class="flex items-center">
		<p class="font-bold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200">
			<slot />
		</p>
		{#if toolTipText.length > 0}
			<ToolTip>
				{toolTipText}
			</ToolTip>
		{/if}
		{#if optional === true}
			<div
				class="bg-gray-100 dark:bg-gray-800/60 ml-2 px-2 rounded-md transition-colors duration-200 h-6 flex items-center"
			>
				<p class="text-slate-600 dark:text-gray-400 text-xs transition-colors duration-200">
					optional
				</p>
			</div>
		{/if}
	</div>

	<textarea
		{id}
		bind:this={bindTo}
		bind:value
		{required}
		minlength={minLength}
		maxlength={maxLength}
		{readonly}
		{placeholder}
		class=" focus:outline-none pl-2 border border-transparent dark:border-gray-700/60 rounded-md focus:border-solid text-gray-600 dark:text-gray-300 focus:bg-gray-100 dark:focus:bg-gray-700/60 font-light focus:border-slate-400 dark:focus:border-highlight-blue/80 bg-grayblue-light dark:bg-gray-900/60 transition-colors duration-200"
		class:h-16={height === 'small'}
		class:h-28={height === 'medium'}
		class:h-48={height === 'large'}
		class:resize-none={!resize}
	/>
</label>
