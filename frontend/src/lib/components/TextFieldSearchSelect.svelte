<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

	export let id;
	// bind a element to this component input field
	// use like <TextField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	export let value = ''; // for binding value
	export let required = false;
	export let options = [];
	export let toolTipText = '';
	export let placeholder = '';

	// (string) => void
	export let onKeyUp;
	// () => string
	export let onSelect;

	let showSelection = false;

	const closeSelection = () => {
		showSelection = false;
		// stop listening for a click
		document.removeEventListener('click', closeSelection);
	};

	const onFocus = () => {
		document.addEventListener('click', closeSelection);
	};

	const _onKeyUp = () => {
		try {
			if (value.length == 0) {
				return;
			}
			onKeyUp(value);
			showSelection = true;
		} catch (err) {
			console.error('failed to search', err);
		}
	};

	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;

	onMount(() => {});

	afterUpdate(() => {
		if (!parentForm) {
			parentForm = bindTo.closest('form');
			if (!parentForm) {
				return;
			}
			parentFormResetListener = parentForm.addEventListener('reset', (event) => {
				event.preventDefault();
				value = '';
			});
		}
	});

	onDestroy(() => {
		if (parentFormResetListener) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});
</script>

<div class="flex flex-col justify-start">
	<label class="flex flex-col py-2 relative">
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
		</div>
	</label>
	<div class="relative">
		<div class="flex items-center relative w-60">
			<input
				type="text"
				{placeholder}
				bind:this={bindTo}
				bind:value
				on:focus={onFocus}
				on:blur={() => {
					if (!options.includes(value)) {
						value = '';
					}
				}}
				on:keyup={_onKeyUp}
				on:click|stopPropagation={() => {}}
				autocomplete="off"
				class="w-full relative rounded-md py-2 pl-4 focus:pl-10 text-gray-600 dark:text-gray-100 border border-transparent focus:outline-none focus:border-solid focus:border focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal cursor-pointer focus:cursor-text transition-colors duration-200"
				{id}
				{required}
			/>
			{#if showSelection}
				<img class="absolute w-4 left-4" src="/search-icon.svg" alt="search" />
			{/if}
			<img class="absolute pointer-events-none w-4 right-4" src="/arrow.svg" alt="drop down" />
		</div>
		{#if options.length && showSelection}
			<div class="w-96 absolute top-10 z-50">
				<ul
					class="bg-gray-100 dark:bg-gray-700 list-none mt-4 rounded-md min-w-fit shadow-md dark:shadow-gray-900/50 border border-gray-200 dark:border-gray-600 max-h-40 overflow-y-scroll transition-colors duration-200"
				>
					{#each options as option}
						<li class="break-words">
							<button
								class="w-full text-left bg-slate-100 dark:bg-gray-700 rounded-md text-gray-600 dark:text-gray-200 hover:bg-grayblue-dark dark:hover:bg-gray-600 hover:text-white py-2 px-2 cursor-pointer transition-colors duration-200"
								on:click|preventDefault={() => {
									value = '';
									showSelection = false;
									onSelect(option);
								}}
							>
								{option}
							</button>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</div>
</div>
