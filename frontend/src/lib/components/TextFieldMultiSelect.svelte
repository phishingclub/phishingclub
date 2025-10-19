<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';
	import { activeFormElementSubscribe } from '$lib/store/activeFormElement';

	export let _id = Symbol();
	export let id;
	export let bindTo = null;
	export let defaultValue = [];
	export let value = defaultValue;
	export let placeholder = 'Select...';
	export let required = false;
	export let options = [];
	export let onSelect = (value) => {};
	export let onRemove = (value) => {};
	export let toolTipText = '';
	export let optional = false;
	export let hidden = false;
	export let size = 'normal';
	export let inline = false;

	// Ensure options is always an array
	$: optionsArray = Array.isArray(options) ? options : Array.from(options);

	let allOptions = [];
	let showDropdown = false;
	let inputElement;
	let dropdownElement;
	let justSelected = false;
	let inputValue = '';

	// Simple function to filter options based on input
	const filterOptions = (searchValue) => {
		if (!searchValue) {
			return [...optionsArray];
		}
		return optionsArray.filter(
			(opt) => opt && opt.toLowerCase && opt.toLowerCase().includes(searchValue.toLowerCase())
		);
	};

	// Track if user has typed (for filtering) vs just focused (show all)
	let hasTyped = false;

	// Update filtered options - show all on focus, filter only when typed
	$: allOptions = hasTyped ? filterOptions(inputValue) : [...optionsArray];

	// Handle focus - don't auto-open dropdown
	const handleFocus = () => {
		// Just set focus state, don't open dropdown
		justSelected = false; // Reset the flag
	};

	// Handle click to open dropdown
	const handleClick = () => {
		if (!showDropdown) {
			showDropdown = true;
			hasTyped = false; // Reset typing flag to show all options
			allOptions = [...optionsArray]; // Show all options on click
		}
	};

	// Handle input changes for filtering
	const handleInput = (e) => {
		inputValue = e.target.value;
		showDropdown = true;
		hasTyped = true; // User has typed, enable filtering
		allOptions = filterOptions(inputValue);
	};

	// Select an option
	const selectOption = (option) => {
		// Check if option is already selected
		if (!value.includes(option)) {
			value = [...value, option];
			onSelect(option);
		}
		inputValue = '';
		showDropdown = false;
		hasTyped = false; // Reset typing flag after selection
		justSelected = true; // Set flag to prevent dropdown reopening
		// Focus the input field after selection without reopening dropdown
		setTimeout(() => {
			if (inputElement) {
				inputElement.focus();
			}
		}, 0);
	};

	// Remove a selected option
	const removeSelectedOption = (event) => {
		const optionToRemove = event.target.dataset.value;
		value = value.filter((item) => item !== optionToRemove);
		onRemove(optionToRemove);
	};

	// Close dropdown
	const closeDropdown = () => {
		showDropdown = false;
		hasTyped = false; // Reset typing flag when closing
	};

	// Handle blur to close dropdown when tabbing out
	const handleBlur = (e) => {
		// Use setTimeout to allow click events on options to complete first
		setTimeout(() => {
			const focusedElement = document.activeElement;
			const container = inputElement?.closest('.textfield-select-container');

			// If focus moved outside the component, close dropdown
			if (!container?.contains(focusedElement)) {
				closeDropdown();
			}
		}, 100);
	};

	// Handle keyboard navigation
	const handleKeyDown = (e) => {
		if (!showDropdown) {
			if (e.key === 'ArrowDown' || e.key === 'Enter') {
				e.preventDefault();
				handleClick();
				return;
			}
			return;
		}

		if (e.key === 'Escape') {
			e.preventDefault();
			closeDropdown();
			return;
		}

		if (e.key === 'Enter') {
			e.preventDefault();
			if (allOptions.length === 1) {
				selectOption(allOptions[0]);
			}
			return;
		}

		if (e.key === 'ArrowDown') {
			e.preventDefault();
			const firstOption = dropdownElement?.querySelector('button');
			firstOption?.focus();
			return;
		}
	};

	// Handle option keyboard navigation
	const handleOptionKeyDown = (e, option) => {
		if (e.key === 'Tab') {
			// Let tab work normally but close dropdown
			closeDropdown();
			return;
		}

		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			selectOption(option);
			return;
		}

		if (e.key === 'Escape') {
			e.preventDefault();
			closeDropdown();
			inputElement?.focus();
			return;
		}

		if (e.key === 'ArrowUp') {
			e.preventDefault();
			const currentButton = e.target;
			const prevButton =
				currentButton.parentElement?.previousElementSibling?.querySelector('button');
			if (prevButton) {
				prevButton.focus();
			} else {
				inputElement?.focus();
			}
			return;
		}

		if (e.key === 'ArrowDown') {
			e.preventDefault();
			const currentButton = e.target;
			const nextButton = currentButton.parentElement?.nextElementSibling?.querySelector('button');
			if (nextButton) {
				nextButton.focus();
			}
			return;
		}
	};

	// Handle clicks outside to close dropdown
	const handleOutsideClick = (e) => {
		if (!showDropdown) return;

		const container = inputElement?.closest('.textfield-select-container');
		if (container && !container.contains(e.target)) {
			closeDropdown();
		}
	};

	// Bind to parent form element
	let parentForm = null;
	let parentFormResetListener = null;

	onMount(() => {
		value = value || defaultValue;
		const unsubscribe = activeFormElementSubscribe(_id, closeDropdown);
		document.addEventListener('click', handleOutsideClick);

		return () => {
			unsubscribe();
			document.removeEventListener('click', handleOutsideClick);
		};
	});

	afterUpdate(() => {
		if (inputElement) {
			bindTo = inputElement;
		}

		if (!parentForm && inputElement) {
			parentForm = inputElement.closest('form');
			if (parentForm) {
				parentFormResetListener = parentForm.addEventListener('reset', (event) => {
					event.preventDefault();
					value = defaultValue;
				});
			}
		}
	});

	onDestroy(() => {
		if (parentFormResetListener && parentForm) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
		document.removeEventListener('click', handleOutsideClick);
	});

	// Generate unique IDs for accessibility
	const comboboxId = id || `textfield-multiselect-${_id.toString()}`;
	const listboxId = `${comboboxId}-listbox`;
	const labelId = `${comboboxId}-label`;

	// Reactive statements for accessibility
	$: hasValue = inputValue && inputValue !== '';
	$: ariaExpanded = showDropdown;
	$: displayValue = value.length > 0 ? `${value.length} selected` : '';
</script>

<div
	class="flex justify-start textfield-select-container"
	class:hidden
	class:flex-col={!inline}
	class:flex-row={inline}
>
	<label class="flex flex-col py-2 relative" class:py-2={!inline} class:pr-2={inline}>
		<div class="flex items-center">
			<p
				id={labelId}
				class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200"
			>
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
					<p class="text-slate-600 dark:text-gray-400 text-xs">optional</p>
				</div>
			{/if}
		</div>
	</label>
	<div class="relative">
		<div
			class="flex items-center relative"
			class:w-28={size == 'small'}
			class:w-60={size == 'normal'}
		>
			<input
				bind:this={inputElement}
				type="text"
				role="combobox"
				id={comboboxId}
				aria-labelledby={labelId}
				aria-expanded={ariaExpanded}
				aria-controls={listboxId}
				aria-autocomplete="list"
				aria-haspopup="listbox"
				bind:value={inputValue}
				on:focus={handleFocus}
				on:blur={handleBlur}
				on:input={handleInput}
				on:keydown={handleKeyDown}
				on:click={handleClick}
				autocomplete="off"
				class="w-full relative rounded-md py-2 pr-10 text-gray-600 dark:text-gray-300 border border-transparent focus:outline-none focus:border-solid focus:border focus:border-slate-400 dark:focus:border-highlight-blue/80 focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 font-normal cursor-pointer focus:cursor-text transition-colors duration-200"
				class:pl-10={showDropdown}
				class:pl-4={!showDropdown}
				class:text-gray-400={!hasValue && !showDropdown && value.length === 0}
				placeholder={!hasValue && !showDropdown && value.length === 0
					? placeholder
					: showDropdown
						? ''
						: displayValue}
				required={required && !value.length}
			/>
			<!-- Search icon - visible when dropdown is open -->
			{#if showDropdown}
				<img
					class="absolute w-4 left-3 select-none pointer-events-none z-10"
					src="/search-icon.svg"
					alt=""
					aria-hidden="true"
				/>
			{/if}
			<!-- Clear button for optional fields -->
			{#if optional === true && value.length > 0}
				<button
					class="absolute right-10 z-10"
					type="button"
					aria-label="Clear selection"
					on:click={(e) => {
						e.stopPropagation();
						value = [];
						inputValue = '';
						inputElement?.focus();
					}}
				>
					<img class="w-4" src="/remove-value.svg" alt="" />
				</button>
			{/if}
			<!-- Dropdown arrow -->
			<img
				class="absolute pointer-events-none w-4 right-3"
				class:right-12={optional === true && value.length > 0}
				src="/arrow.svg"
				alt=""
				aria-hidden="true"
			/>
		</div>
		<!-- Dropdown list -->
		{#if showDropdown}
			<div
				bind:this={dropdownElement}
				class="absolute top-10 z-50"
				class:w-28={size == 'small'}
				class:w-60={size == 'normal'}
			>
				<ul
					id={listboxId}
					role="listbox"
					aria-labelledby={labelId}
					class="bg-gray-100 dark:bg-gray-900 list-none mt-4 z-[999] rounded-md min-w-fit shadow-md border border-gray-200 dark:border-gray-700/60 max-h-40 overflow-y-scroll transition-colors duration-200"
				>
					{#if allOptions.length}
						{#each allOptions as option, index}
							<li role="none">
								<button
									id="{listboxId}-option-{index}"
									role="option"
									aria-selected={value.includes(option)}
									class="w-full text-left bg-slate-100 dark:bg-gray-900 rounded-md text-gray-600 dark:text-gray-300 hover:bg-grayblue-dark dark:hover:bg-highlight-blue/40 hover:text-white py-2 px-2 cursor-pointer focus:bg-grayblue-dark dark:focus:bg-highlight-blue/40 focus:text-white focus:outline-none transition-colors duration-200"
									on:click={(e) => {
										e.preventDefault();
										e.stopPropagation();
										selectOption(option);
									}}
									on:keydown={(e) => handleOptionKeyDown(e, option)}
									on:blur={handleBlur}
								>
									{option}
								</button>
							</li>
						{/each}
					{:else}
						<li
							role="none"
							class="w-full bg-slate-100 dark:bg-gray-900 text-gray-600 dark:text-gray-400 py-2 px-2 transition-colors duration-200"
						>
							No options available
						</li>
					{/if}
				</ul>
			</div>
		{/if}
		<!-- Selected items -->
		{#if value.length > 0}
			<div class="flex flex-row flex-wrap mt-4 gap-2">
				{#each value as option}
					<button
						type="button"
						on:click|preventDefault={removeSelectedOption}
						on:keypress|preventDefault={removeSelectedOption}
						data-value={option}
						class="flex flex-row items-center bg-gray-100 dark:bg-gray-800/60 hover:bg-gray-200 dark:hover:bg-gray-700/80 px-2 py-1 rounded-md text-gray-900 dark:text-gray-300 text-sm transition-colors duration-200"
						aria-label="Remove {option}"
					>
						{option}
						<img class="w-3 ml-2 pointer-events-none" src="/delete2.svg" alt="" />
					</button>
				{/each}
			</div>
		{/if}
	</div>
</div>
