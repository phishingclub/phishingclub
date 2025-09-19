<script>
	import { tweened } from 'svelte/motion';
	import { cubicOut } from 'svelte/easing';

	export let title = '';
	export let value = 0;
	export let borderColor = 'border-cta-blue';
	export let iconColor = 'text-cta-blue';
	export let percentages = [];

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
	class="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-md dark:shadow-gray-900/50 border-l-[12px] {borderColor} hover:shadow-lg dark:hover:shadow-gray-900/70 transition-all duration-200"
>
	<div
		class="text-grayblue-dark dark:text-gray-400 text-sm font-semibold transition-colors duration-200"
	>
		{title}
	</div>
	<div class="flex items-center justify-between">
		<div class="flex items-center">
			<span
				class="text-3xl font-bold text-pc-darkblue dark:text-gray-100 transition-colors duration-200 {flash
					? 'flash'
					: ''}"
			>
				{Math.floor($displayValue)}
			</span>
			<div class="ml-2">
				<slot name="icon">
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-5 w-5 {iconColor}"
						fill="none"
						viewBox="0 0 24 24"
						stroke="currentColor"
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

	{#if validPercentages.length > 0}
		<button
			class="mt-2 text-sm text-gray-600 dark:text-gray-400 flex items-center transition-colors duration-200"
			on:click={cyclePercentage}
			class:cursor-pointer={validPercentages.length > 1}
		>
			<div class="flex items-center">
				<span
					class="text-pc-darkblue dark:text-gray-200 font-semibold transition-colors duration-200"
				>
					{validPercentages[currentPercentageIndex].value}%
				</span>
				<span class="ml-1">
					{validPercentages[currentPercentageIndex].relativeTo}
				</span>
			</div>
			{#if validPercentages.length > 1}
				<svg
					xmlns="http://www.w3.org/2000/svg"
					class="h-4 w-4 ml-1 text-gray-400 dark:text-gray-500 transition-colors duration-200"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-width="2"
						d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
					/>
				</svg>
			{/if}
		</button>
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
