<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

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
	var handleClick = (e) => {
		e.preventDefault();
		// if this is a key event and the key is not enter
		if (e.key && (e.key === 'Enter' || e.key === ' ')) {
			viewPassword = !viewPassword;
			return;
		}
		// bug - this fixes a bug where if a user
		// clicks enter inside the button field, the password is shown
		if (e.target.tagName === 'BUTTON') {
			return;
		}
		viewPassword = !viewPassword;
	};
</script>

<label class="flex flex-col py-2 w-60">
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
			<div class="bg-gray-100 dark:bg-gray-700 ml-2 px-2 rounded-md transition-colors duration-200">
				<p class="text-slate-600 dark:text-gray-300 text-xs transition-colors duration-200">
					optional
				</p>
			</div>
		{/if}
	</div>
	<div class="relative flex items-center justify-end">
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
				class="text-ellipsis w-60 rounded-md py-2 pl-4 pr-12 text-gray-600 dark:text-gray-200 border border-transparent dark:border-gray-600 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-blue-500 focus:bg-gray-100 dark:focus:bg-gray-600 bg-grayblue-light dark:bg-gray-700 font-normal transition-colors duration-200"
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
				class="text-ellipsis w-60 rounded-md py-2 pl-4 pr-12 text-gray-600 dark:text-gray-200 border border-transparent dark:border-gray-600 focus:outline-none focus:border-solid focus:border-slate-400 dark:focus:border-blue-500 focus:bg-gray-100 dark:focus:bg-gray-600 bg-grayblue-light dark:bg-gray-700 font-normal transition-colors duration-200"
			/>
		{/if}
		<button
			class="absolute w-8 mr-2 hover:opacity-70 transition-opacity duration-200"
			on:click={handleClick}
			on:keyup={handleClick}
		>
			{#if viewPassword}
				<img
					src="/view.svg"
					alt="view"
					class="dark:filter dark:brightness-0 dark:invert transition-all duration-200"
				/>
			{:else}
				<img
					src="/toggle-view.svg"
					alt="toggle view"
					class="dark:filter dark:brightness-0 dark:invert transition-all duration-200"
				/>
			{/if}
		</button>
	</div>
</label>
