<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

	// use like <PasswordField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	// placeholder
	export let defaultValue = ''; // default checkbox value
	export let value = defaultValue; // for binding value
	export let toolTipText = '';
	export let optional = false;
	export let placeholder = '';
	export let required = false;
	export let minLength = null;
	export let maxLength = null;
	export let id = null;
	/** @type {'medium'|'large'|'full'} */
	export let width = 'medium';

	let parentForm = null;
	let parentFormResetListener = null;
	let viewPassword = true;

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
	const toggleView = () => {
		viewPassword = !viewPassword;
	};
</script>

<label
	class="flex flex-col py-2 w-full"
	class:sm:w-60={width === 'medium'}
	class:sm:w-80={width === 'large'}
>
	<FieldLabel {toolTipText} {optional}><slot /></FieldLabel>
	<div class="relative flex items-center">
		{#if viewPassword}
			<input
				{id}
				type="password"
				bind:this={bindTo}
				bind:value
				on:keyup
				{placeholder}
				autocomplete="off"
				minlength={minLength}
				maxlength={maxLength}
				{required}
				class="text-ellipsis w-full rounded-md py-2 pl-3 pr-11 text-slate-800 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 border border-transparent dark:border-gray-700 font-normal transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30"
			/>
		{:else}
			<input
				{id}
				on:keyup
				type="text"
				bind:this={bindTo}
				bind:value
				minlength={minLength}
				maxlength={maxLength}
				{placeholder}
				autocomplete="off"
				{required}
				class="text-ellipsis w-full rounded-md py-2 pl-3 pr-11 text-slate-800 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 border border-transparent dark:border-gray-700 font-normal transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30"
			/>
		{/if}
		<button
			type="button"
			aria-label={viewPassword ? 'Show value' : 'Hide value'}
			aria-pressed={!viewPassword}
			title={viewPassword ? 'Show value' : 'Hide value'}
			class="absolute right-1 inline-flex h-8 w-8 items-center justify-center rounded-md opacity-60 hover:opacity-100 focus:outline-none focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue transition-opacity duration-200"
			on:click|preventDefault={toggleView}
		>
			<img
				src={viewPassword ? '/view.svg' : '/toggle-view.svg'}
				alt=""
				aria-hidden="true"
				class="w-6 h-auto dark:filter dark:brightness-0 dark:invert"
			/>
		</button>
	</div>
</label>
