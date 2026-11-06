<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './form/FieldLabel.svelte';

	export let id;
	// bind a element to this component input field
	// use like <TextField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	export let value = ''; // for binding value
	export let required = false;
	export let options = [];
	export let toolTipText = '';
	export let placeholder = '';
	// 'medium' is a fixed width from sm, 'full' fills the parent
	export let width = 'medium';

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

<div class="flex flex-col py-2 w-full {width === 'full' ? '' : 'sm:w-auto'}">
	<label for={id} class="flex flex-col relative">
		<FieldLabel {toolTipText}><slot /></FieldLabel>
	</label>
	<div class="relative">
		<div class="flex items-center relative w-full {width === 'full' ? '' : 'sm:w-60'}">
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
				class="w-full relative rounded-md py-2 {showSelection ? 'pl-10' : 'pl-3'} pr-10 text-slate-800 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 border border-transparent dark:border-gray-700 font-normal cursor-pointer focus:cursor-text transition-colors duration-200 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30"
				{id}
				{required}
			/>
			{#if showSelection}
				<img class="absolute w-4 left-4" src="/search-icon.svg" alt="" aria-hidden="true" />
			{/if}
			<img
				class="absolute pointer-events-none w-4 right-4"
				src="/arrow.svg"
				alt=""
				aria-hidden="true"
			/>
		</div>
		{#if options.length && showSelection}
			<div class="w-full {width === 'full' ? '' : 'sm:w-96'} absolute top-10 z-50">
				<ul
					class="bg-gray-100 dark:bg-gray-900 list-none mt-4 rounded-md min-w-fit shadow-md dark:shadow-gray-900/50 border border-gray-200 dark:border-gray-700/60 max-h-40 overflow-y-auto transition-colors duration-200"
				>
					{#each options as option}
						<li class="break-words">
							<button
								type="button"
								class="w-full text-left bg-slate-100 dark:bg-gray-900 rounded-md text-gray-600 dark:text-gray-300 hover:bg-grayblue-dark dark:hover:bg-highlight-blue/40 hover:text-white focus:bg-grayblue-dark dark:focus:bg-highlight-blue/40 focus:text-white focus:outline-none py-2 px-2 cursor-pointer transition-colors duration-200"
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
