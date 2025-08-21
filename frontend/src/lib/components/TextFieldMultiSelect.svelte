<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';

	const _id = Symbol();
	export let id;
	// bind a element to this component input field
	// use like <TextField bind:bindTo={varYouWantToBindTheInputFieldTo} />
	export let bindTo = null;
	export let defaultValue = []; // default selected value
	// for binding value, it is an array of the selected items
	export let value = defaultValue;
	export let required = false;
	export let options = [];
	export let onSelect = (value) => {};
	export let onRemove = (value) => {};
	export let toolTipText = '';
	export let optional = false;

	let filteredOptions = [...options];
	// the input value is only used for searching
	let inputValue = '';
	// bind to parent form element, if there is one
	let parentForm = null;
	// listen to parent form reset event, if one exists
	let parentFormResetListener = null;
	let showSelection = false;

	onMount(() => {
		value = value ?? defaultValue;
		const unsubscribe = activeFormElement.subscribe((activeId) => {
			showSelection = activeId === _id;
		});

		return () => {
			unsubscribe();
		};
	});

	afterUpdate(() => {
		if (options.length && inputValue === '') {
			filteredOptions = [...options];
		}
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

	const removeSelection = (event) => {
		const v = event.target.dataset.value;
		// svelte remove the item from the selected array if it exists
		value = value.filter((item) => item !== v);
		onRemove(v);
	};

	const closeSelection = () => {
		showSelection = false;
		// stop listening for a click
		document.removeEventListener('click', closeSelection);
	};

	const onFocus = (e) => {
		inputValue = '';
		showSelection = true;
		activeFormElement.set(_id);
		// when we focus in the input field, we add a listener for a click anywhere
		// we add a small timeout to ensure the closeSelection is not closed at once
		// when we have focus in the box
		setTimeout(() => {
			document.addEventListener('click', closeSelection);
		}, 250);
	};

	const onKeyUp = () => {
		if (inputValue === '') {
			filteredOptions = [...options];
			return;
		}
		filteredOptions = filteredOptions.filter((opt) => opt.includes(inputValue));
	};

	/** @type {(event: Event) => void} */
	const onChange = (event) => {
		// check if the value already exists in the selected array
		const target = /** @type {HTMLInputElement} */ (event.target);
		if (!value.includes(target.value) && options.includes(target.value)) {
			value = [target.value, ...value];
			// TODO remove it from available selections and ensure the list works when removing it agian
		}
		bindTo.blur();
	};

	const onClickSelectedOption = (option) => {
		// if the option is not already selected, we add it
		if (!value.find((v) => v === option)) {
			value = [option, ...value];
			onSelect(option);
		}
		showSelection = false;
	};
</script>

<div class="flex flex-col justify-start">
	<label class="flex flex-col py-2 relative">
		<div class="flex items-center">
			<p class="font-semibold text-slate-600 py-2">
				<slot />
			</p>
			{#if toolTipText.length > 0}
				<ToolTip>
					{toolTipText}
				</ToolTip>
			{/if}
			{#if optional === true}
				<div class="bg-gray-100 ml-2 px-2 rounded-md">
					<p class="text-slate-600 text-xs">optional</p>
				</div>
			{/if}
		</div>
	</label>

	<div class="relative">
		<div class="flex items-center relative w-60">
			<input
				type="text"
				bind:this={bindTo}
				bind:value={inputValue}
				on:focus={onFocus}
				on:blur={() => {
					if (!options.includes(inputValue)) {
						inputValue = '';
					}
				}}
				on:change={onChange}
				on:keyup={onKeyUp}
				on:click|stopPropagation={() => {}}
				{id}
				required={required && !value.length}
				autocomplete="off"
				class="w-full relative rounded-md py-2 pl-4 focus:pl-10 text-gray-600 border border-transparent focus:outline-none focus:border-solid focus:border focus:border-slate-400 focus:bg-gray-100 bg-grayblue-light font-normal cursor-pointer focus:cursor-text"
			/>
			{#if showSelection}
				<img
					class="absolute w-4 left-4 pointer-events-none select-none"
					src="/search-icon.svg"
					alt="search"
				/>
			{/if}
			<img class="absolute pointer-events-none w-4 right-4" src="/arrow.svg" alt="drop down" />
		</div>
		{#if showSelection}
			<div class="w-60 absolute top-10 z-50">
				<ul
					class="bg-gray-100 list-none mt-4 rounded-md min-w-fit shadow-md border max-h-40 overflow-y-scroll"
				>
					{#if options.length}
						{#each filteredOptions as option}
							<li>
								<button
									class="w-full text-left bg-slate-100 rounded-md text-gray-600 hover:bg-grayblue-dark hover:text-white py-2 px-2 cursor-pointer"
									on:click={() => {
										onClickSelectedOption(option);
									}}
								>
									{option}
								</button>
							</li>
						{/each}
					{:else}
						<li class="w-full bg-slate-100 rounded-md text-gray-600 py-2 px-2">List is empty</li>
					{/if}
				</ul>
			</div>
		{/if}
		<div class="flex flex-row flex-wrap mb-4">
			{#each value as option}
				<button
					on:click|preventDefault={removeSelection}
					on:keypress|preventDefault={removeSelection}
					data-value={option}
					class="flex flex-row items-center bg-gray-100 hover:bg-gray-200 px-2 py-2 mt-2 mr-2 rounded-md"
				>
					{option}
					<img class="w-4 ml-2 pointer-events-none" src="/delete2.svg" alt="delete" />
				</button>
			{/each}
		</div>
	</div>
</div>
