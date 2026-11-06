<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import FieldLabel from './FieldLabel.svelte';
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';
	import { activeFormElement, activeFormElementSubscribe } from '$lib/store/activeFormElement';

	export let _id = Symbol();
	export let id;
	export let bindTo = null;
	export let defaultValue = '';
	export let value = defaultValue;
	export let placeholder = 'Select...';
	export let required = false;
	export let pageOptions = [];
	export let proxyOptions = [];
	export let type = 'page'; // 'page' or 'proxy'
	export let toolTipText = '';
	export let optional = false;
	export let hidden = false;
	export let size = 'normal';
	export let inline = false;
	export let onSelect = (value) => {};

	// Get current options based on type
	$: currentOptions = type === 'proxy' ? proxyOptions : pageOptions;
	$: optionsArray = Array.isArray(currentOptions) ? currentOptions : Array.from(currentOptions);

	let allOptions = [];
	let showDropdown = false;
	let inputElement;
	let dropdownElement;
	let isFocused = false;

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
	$: allOptions = hasTyped ? filterOptions(value) : [...optionsArray];

	// Show dropdown when focused and there are options
	const handleFocus = () => {
		activeFormElement.set(id);
		showDropdown = true;
		isFocused = true;
		hasTyped = false; // Reset typing flag to show all options
		allOptions = [...optionsArray]; // Show all options on focus
	};

	// Handle input changes for filtering
	const handleInput = (e) => {
		value = e.target.value;
		showDropdown = true;
		hasTyped = true; // User has typed, enable filtering
		allOptions = filterOptions(value);
	};

	// Select an option
	const selectOption = (option) => {
		value = option;
		showDropdown = false;
		isFocused = false;
		hasTyped = false; // Reset typing flag after selection
		onSelect(option);
	};

	// Close dropdown
	const closeDropdown = () => {
		showDropdown = false;
		isFocused = false;
		hasTyped = false; // Reset typing flag when closing
	};

	// Handle type change
	const handleTypeChange = (newType) => {
		type = newType;
		value = ''; // reset selection when type changes
		closeDropdown();
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
		if (!showDropdown) return;

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
	const comboboxId = id || `textfield-select-${_id.toString()}`;
	const listboxId = `${comboboxId}-listbox`;
	const labelId = `${comboboxId}-label`;

	// Reactive statements for accessibility
	$: hasValue = value && value !== '';
	$: ariaExpanded = showDropdown;
</script>

<div
	class="flex justify-start py-2 textfield-select-container"
	class:hidden
	class:flex-col={!inline}
	class:flex-row={inline}
	class:w-full={!inline && size !== 'small'}
	class:sm:w-auto={!inline && size === 'normal'}
>
	<label class="flex flex-col relative" class:pr-2={inline}>
		<FieldLabel id={labelId} {toolTipText} {optional}><slot /></FieldLabel>
	</label>
	<div class="relative">
		{#if !isFocused}
			<div class="absolute left-1 top-1/2 transform -translate-y-1/2 flex bg-transparent z-10">
				<button
					type="button"
					class="w-8 h-8 rounded-md border flex items-center justify-center transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {type === 'page' ? 'border-cta-blue bg-blue-50 text-cta-blue dark:border-highlight-blue dark:bg-highlight-blue/20 dark:text-white' : 'border-gray-200 dark:border-gray-700/60 bg-white dark:bg-gray-800/60 text-gray-600 dark:text-gray-300 hover:border-blue-300 dark:hover:border-highlight-blue/80 hover:bg-blue-50 dark:hover:bg-highlight-blue/20'}"
					on:click={() => handleTypeChange('page')}
					title="Page"
					aria-label="Page"
					aria-pressed={type === 'page'}
				>
					<svg
						class="w-4 h-4"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="2"
						stroke-linecap="round"
						stroke-linejoin="round"
						aria-hidden="true"
					>
						<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
						<polyline points="14 2 14 8 20 8" />
						<line x1="8" y1="13" x2="16" y2="13" />
						<line x1="8" y1="17" x2="13" y2="17" />
					</svg>
				</button>
				<button
					type="button"
					class="ml-0.5 w-8 h-8 rounded-md border flex items-center justify-center transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {type === 'proxy' ? 'border-cta-blue bg-blue-50 text-cta-blue dark:border-highlight-blue dark:bg-highlight-blue/20 dark:text-white' : 'border-gray-200 dark:border-gray-700/60 bg-white dark:bg-gray-800/60 text-gray-600 dark:text-gray-300 hover:border-blue-300 dark:hover:border-highlight-blue/80 hover:bg-blue-50 dark:hover:bg-highlight-blue/20'}"
					on:click={() => handleTypeChange('proxy')}
					title="Proxy"
					aria-label="Proxy"
					aria-pressed={type === 'proxy'}
				>
					<ProxySvgIcon size="w-5 h-5" />
				</button>
			</div>
		{/if}

		<div
			class="flex items-center relative"
			class:w-28={size == 'small'}
			class:w-full={size == 'normal' && !inline}
			class:sm:w-80={size == 'normal' && !inline}
			class:w-60={size == 'normal' && inline}
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
				bind:value
				on:focus={handleFocus}
				on:blur={handleBlur}
				on:input={handleInput}
				on:keydown={handleKeyDown}
				on:click={handleFocus}
				autocomplete="off"
				style={optional && hasValue ? 'padding-right: 4rem;' : 'padding-right: 2.5rem;'}
				class="w-full relative rounded-md py-2 text-slate-800 dark:text-gray-300 border border-transparent focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 bg-grayblue-light dark:bg-gray-900/60 dark:border-gray-700 font-normal cursor-pointer focus:cursor-text transition-colors duration-200 overflow-hidden text-ellipsis whitespace-nowrap"
				class:pl-10={isFocused && showDropdown}
				class:pl-20={!isFocused}
				class:pl-4={isFocused && !showDropdown}
				placeholder={!hasValue && !showDropdown ? placeholder : ''}
				{required}
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
			{#if optional === true && hasValue}
				<button
					class="absolute right-8 z-10 hover:opacity-70 transition-opacity"
					type="button"
					aria-label="Clear selection"
					on:click={(e) => {
						e.stopPropagation();
						value = '';
						onSelect('');
						inputElement?.focus();
					}}
				>
					<img class="w-4" src="/remove-value.svg" alt="" />
				</button>
			{/if}
			<!-- Dropdown arrow -->
			<img
				class="absolute pointer-events-none w-4 right-3"
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
				class:w-full={size == 'normal'}
				class:sm:w-80={size == 'normal'}
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
									aria-selected={value === option}
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
							No {type === 'proxy' ? 'Proxies' : 'pages'} available
						</li>
					{/if}
				</ul>
			</div>
		{/if}
	</div>
</div>
