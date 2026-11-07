<script context="module">
	let instanceCount = 0;
</script>

<script>
	import FieldLabel from './form/FieldLabel.svelte';

	export let options = [];
	export let value = null;
	export let label = '';
	// centers the options when there is no label above them
	export let center = true;
	export let optional = false;
	export let toolTipText = '';
	export let width = 'medium';
	export let disabled = false;
	/** @type {*} */
	export let onChange = () => {};

	const labelId = `select-square-label-${++instanceCount}`;
	let buttons = [];

	$: selectedIndex = options.findIndex((o) => o.value === value);
	$: focusIndex = selectedIndex >= 0 ? selectedIndex : 0;
	// with descriptions the cards can differ in height, so titles start at the top to line up,
	// and on phones each card takes its own row so none is left alone at a different width
	$: hasDescription = options.some((o) => o.description);

	const select = (option) => {
		if (disabled) return;
		value = option.value;
		onChange();
	};

	// arrow keys move the selection like a native radio group
	const onKeyDown = (e, index) => {
		if (disabled || !options.length) return;
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
			next = (index + 1) % options.length;
		} else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
			next = (index - 1 + options.length) % options.length;
		}
		if (next < 0) return;
		e.preventDefault();
		select(options[next]);
		buttons[next]?.focus();
	};
</script>

<div class="flex flex-col py-2 w-full sm:w-auto">
	{#if label}
		<FieldLabel id={labelId} {toolTipText} {optional}>{label}</FieldLabel>
	{/if}

	<div
		role="radiogroup"
		aria-labelledby={label ? labelId : undefined}
		aria-disabled={disabled ? 'true' : undefined}
		class="flex flex-wrap gap-3 sm:gap-4 py-2"
		class:sm:justify-center={center && !label}
	>
		{#each options as option, index}
			{@const selected = value === option.value}
			<button
				bind:this={buttons[index]}
				type="button"
				role="radio"
				id={option.id}
				aria-checked={selected}
				tabindex={index === focusIndex ? 0 : -1}
				{disabled}
				class="relative flex-1 min-w-[7rem] sm:flex-none p-3 rounded-lg border-2 transition-colors duration-200 flex focus:outline-none {hasDescription
					? 'flex-row items-center text-left pr-8 sm:pr-3 sm:flex-col sm:justify-start sm:text-center basis-full sm:basis-auto'
					: 'flex-col items-center text-center justify-center'} focus-visible:ring-2 focus-visible:ring-cta-blue focus-visible:ring-offset-2 focus-visible:ring-offset-white dark:focus-visible:ring-highlight-blue dark:focus-visible:ring-offset-gray-800 {disabled
					? 'opacity-50 cursor-not-allowed'
					: 'cursor-pointer'} {selected
					? 'border-cta-blue bg-blue-50 text-cta-blue font-semibold dark:border-highlight-blue dark:bg-highlight-blue/20 dark:text-white'
					: 'border-gray-200 bg-white text-gray-700 dark:border-gray-700/60 dark:bg-gray-900/60 dark:text-gray-300'} {!disabled &&
				!selected
					? 'hover:border-blue-300 hover:bg-blue-50 dark:hover:border-highlight-blue/80 dark:hover:bg-highlight-blue/10'
					: ''}"
				class:sm:min-h-32={option.icon && option.description}
				class:min-h-12={!option.icon && !option.description}
				class:sm:w-28={width === 'small'}
				class:sm:w-40={width === 'medium'}
				class:sm:w-64={width === 'large'}
				on:click={() => select(option)}
				on:keydown={(e) => onKeyDown(e, index)}
			>
				{#if selected}
					<svg
						class="absolute top-1 right-1 w-4 h-4 text-cta-blue dark:text-highlight-blue"
						viewBox="0 0 20 20"
						fill="currentColor"
						aria-hidden="true"
					>
						<path
							fill-rule="evenodd"
							d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
							clip-rule="evenodd"
						/>
					</svg>
				{/if}
				{#if option.icon}
					<span
						class="text-xl {hasDescription ? 'mr-3 sm:mr-0 sm:mb-2' : 'mb-2'}"
						aria-hidden="true">{option.icon}</span
					>
				{/if}
				<!-- on phones cards with a description lay out as a compact row, from sm up as a square -->
				<span
					class="flex flex-col min-w-0 {hasDescription ? 'items-start sm:items-center' : 'items-center'}"
				>
					<span class="text-sm" class:font-medium={!selected}>{option.label}</span>
					{#if option.description}
						<span
							class="text-xs font-normal text-gray-500 dark:text-gray-400 mt-1 transition-colors duration-200"
							>{option.description}</span
						>
					{/if}
				</span>
			</button>
		{/each}
	</div>
</div>
