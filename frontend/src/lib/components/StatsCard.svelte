<script>
	import { tweened } from 'svelte/motion';
	import { cubicOut } from 'svelte/easing';
	import ToolTip from './ToolTip.svelte';

	export let title = '';
	export let value = 0;
	export let borderColor = 'border-cta-blue';
	export let iconColor = 'text-cta-blue';
	export let percentages = [];
	// optional help text shown in a ToolTip after the title
	export let tip = '';

	let initialValueSet = false;
	let previousValue = 0;
	let flash = false;
	let currentPercentageIndex = 0;

	const displayValue = tweened(0, {
		duration: 200,
		easing: cubicOut
	});

	$: validPercentages = percentages.filter((p) => p.baseValue && p.baseValue > 0);

	$: {
		if (!initialValueSet && value > 0) {
			displayValue.set(value, { duration: 0 });
			initialValueSet = true;
			previousValue = value;
		} else if (initialValueSet && value !== previousValue) {
			displayValue.set(value);
			if (value > previousValue) {
				flash = true;
				setTimeout(() => (flash = false), 1000);
			}
			previousValue = value;
		}
	}

	function cyclePercentage() {
		if (validPercentages.length > 1) {
			currentPercentageIndex = (currentPercentageIndex + 1) % validPercentages.length;
		}
	}
</script>

<div
	class="relative h-full min-w-0 bg-white dark:bg-gray-900/80 p-4 sm:p-6 rounded-lg shadow-md dark:shadow-none border-l-8 {borderColor} hover:shadow-lg dark:hover:shadow-none transition-all duration-200 dark:ring-1 dark:ring-gray-600/30"
>
	<!-- optional small indicator in the top right corner, positioned so the layout does not move -->
	{#if $$slots.corner}
		<div class="absolute top-2 right-2">
			<slot name="corner" />
		</div>
	{/if}
	<!-- two line title box so values in a row of cards line up when one title wraps -->
	<div
		class="text-slate-500 dark:text-gray-400 text-sm leading-5 min-h-[2.5rem] line-clamp-2 font-semibold break-words transition-colors duration-200"
		class:pr-6={$$slots.corner}
		{title}
	>
		{title}{#if tip}<ToolTip>{tip}</ToolTip>{/if}
	</div>
	<div class="flex items-center justify-between">
		<div class="flex items-center">
			<!-- the value slot shows a text value such as a rate in place of the counted number -->
			{#if $$slots.value}
				<span
					class="text-2xl sm:text-3xl font-bold tabular-nums text-pc-darkblue dark:text-gray-100 transition-colors duration-200"
				>
					<slot name="value" />
				</span>
			{:else}
				<span
					class="text-2xl sm:text-3xl font-bold tabular-nums text-pc-darkblue dark:text-gray-100 transition-colors duration-200 {flash
						? 'flash'
						: ''}"
				>
					{Math.floor($displayValue)}
				</span>
			{/if}
			<div class="ml-2">
				<slot name="icon">
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-5 w-5 {iconColor}"
						fill="none"
						viewBox="0 0 24 24"
						stroke="currentColor"
				
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M13 10V3L4 14h7v7l9-11h-7z"
						/>
					</svg>
				</slot>
			</div>
		</div>
	</div>

	{#if validPercentages.length > 1}
		<button
			type="button"
			class="pc-focus -mx-1 mt-2 px-1 rounded text-sm text-slate-600 dark:text-gray-400 flex items-center text-left cursor-pointer hover:text-slate-800 dark:hover:text-gray-200 transition-colors duration-200"
			aria-label="{validPercentages[currentPercentageIndex].value}% {validPercentages[currentPercentageIndex]
				.relativeTo}. Show next comparison"
			title="Show next comparison"
			on:click={cyclePercentage}
		>
			<span>
				<span class="text-pc-darkblue dark:text-gray-200 font-semibold transition-colors duration-200">
					{validPercentages[currentPercentageIndex].value}%
				</span>
				<span class="ml-1">
					{validPercentages[currentPercentageIndex].relativeTo}
				</span>
			</span>
			<svg
				xmlns="http://www.w3.org/2000/svg"
				class="h-4 w-4 ml-1 flex-shrink-0 text-slate-400 dark:text-gray-500 transition-colors duration-200"
				fill="none"
				viewBox="0 0 24 24"
				stroke="currentColor"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					stroke-width="2"
					d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
				/>
			</svg>
		</button>
	{:else if $$slots.sub}
		<!-- a free text line under the value, used when there are no percentages -->
		<div class="mt-2 text-sm text-slate-600 dark:text-gray-400 transition-colors duration-200">
			<slot name="sub" />
		</div>
	{:else if validPercentages.length === 1}
		<p class="mt-2 text-sm text-slate-600 dark:text-gray-400 transition-colors duration-200">
			<span class="text-pc-darkblue dark:text-gray-200 font-semibold transition-colors duration-200">
				{validPercentages[0].value}%
			</span>
			<span class="ml-1">{validPercentages[0].relativeTo}</span>
		</p>
	{/if}
</div>

<style>
	.flash {
		animation: flash-animation 1s ease-out;
	}

	@keyframes flash-animation {
		0% {
			color: inherit;
		}
		25% {
			color: #5dd8c4;
		}
		100% {
			color: inherit;
		}
	}
</style>
