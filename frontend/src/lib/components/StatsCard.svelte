<script>
	import { tweened } from 'svelte/motion';
	import { cubicOut } from 'svelte/easing';
	import ToolTip from './ToolTip.svelte';
	import { dashboardIcons, lightIconShade } from '$lib/utils/dashboardIcons.js';

	export let title = '';
	export let value = 0;
	// a key of dashboardIcons, drawn in iconColor on a tint of the same color
	export let icon = 'active';
	export let iconColor = 'text-cta-blue';
	export let percentages = [];
	// optional help text shown in a ToolTip after the title
	export let tip = '';
	// a tile that links to where its number is listed. The link covers the
	// whole tile and sits under the ToolTip, so the ToolTip stays its own button
	export let href = '';
	// the data-tile value of the link
	export let tileKey = '';
	// keep a line under the number when other tiles in the row have one, so the
	// numbers line up
	export let reserveContext = false;

	let initialValueSet = false;
	let previousValue = 0;
	let pulse = false;
	let currentPercentageIndex = 0;

	const displayValue = tweened(0, {
		duration: 200,
		easing: cubicOut
	});

	$: validPercentages = percentages.filter((p) => p.baseValue && p.baseValue > 0);
	$: if (currentPercentageIndex >= validPercentages.length) currentPercentageIndex = 0;
	$: shown = validPercentages[currentPercentageIndex];

	$: {
		if (!initialValueSet && value > 0) {
			displayValue.set(value, { duration: 0 });
			initialValueSet = true;
			previousValue = value;
		} else if (initialValueSet && value !== previousValue) {
			displayValue.set(value);
			// a short pulse on any change. It has no color, as a rise is not always good
			pulse = true;
			setTimeout(() => (pulse = false), 600);
			previousValue = value;
		}
	}

	function cyclePercentage() {
		if (validPercentages.length > 1) {
			currentPercentageIndex = (currentPercentageIndex + 1) % validPercentages.length;
		}
	}

	$: parts = dashboardIcons[icon] ?? [];
	$: shade = lightIconShade[iconColor] ? `${lightIconShade[iconColor]} dark:text-inherit` : '';
</script>

<div
	class="group/tile relative flex h-full min-w-0 flex-col rounded-lg bg-white p-4 ring-1 ring-slate-200 transition-colors duration-200 dark:bg-gray-900/80 dark:ring-gray-600/40 sm:p-5 {href
		? 'hover:bg-slate-50 dark:hover:bg-gray-800/70'
		: ''}"
>
	{#if href}
		<a
			{href}
			class="pc-focus absolute inset-0 rounded-lg"
			data-tile={tileKey || null}
			aria-label="{title}, {value}"
		></a>
	{/if}
	<div class="flex min-w-0 items-center gap-3">
		<span
			class="relative inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg {iconColor}"
			aria-hidden="true"
		>
			<!-- the tint and edge take the token color through currentColor -->
			<span class="absolute inset-0 rounded-lg bg-current opacity-[0.14] dark:opacity-20"></span>
			<span class="absolute inset-0 rounded-lg border border-current opacity-25"></span>
			<svg
				class="relative h-5 w-5 {shade}"
				fill="none"
				viewBox="0 0 24 24"
				stroke="currentColor"
				stroke-width="1.75"
			>
				{#each parts as part}
					<path
						stroke-linecap="round"
						stroke-linejoin="round"
						d={part.d}
						fill={part.fill ? 'currentColor' : 'none'}
					/>
				{/each}
			</svg>
		</span>
		<div
			class="min-w-0 text-sm font-medium leading-5 text-slate-700 line-clamp-2 break-words transition-colors duration-200 dark:text-gray-200"
		>
			{title}{#if tip}<span class="relative z-10"><ToolTip>{tip}</ToolTip></span>{/if}
		</div>
	</div>

	<div class="mt-auto flex items-end justify-between gap-2 pt-3">
		<!-- the value slot shows a text value such as Hidden in place of the counted number -->
		{#if $$slots.value}
			<span
				class="relative z-10 text-3xl font-semibold leading-none tabular-nums text-pc-darkblue transition-colors duration-200 dark:text-gray-100 sm:text-4xl"
			>
				<slot name="value" />
			</span>
		{:else}
			<span
				class="text-3xl font-semibold leading-none tabular-nums text-pc-darkblue transition-colors duration-200 dark:text-gray-100 sm:text-4xl"
				class:pulse
			>
				{Math.floor($displayValue)}
			</span>
		{/if}
		{#if href}
			<!-- shows the tile is a link on hover and keyboard focus -->
			<svg
				class="mb-1 h-4 w-4 shrink-0 text-slate-400 opacity-0 transition-opacity duration-200 group-hover/tile:opacity-100 group-focus-within/tile:opacity-100 dark:text-gray-500"
				fill="none"
				viewBox="0 0 24 24"
				stroke="currentColor"
				stroke-width="1.5"
				aria-hidden="true"
			>
				<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5 21 12m0 0-7.5 7.5M21 12H3" />
			</svg>
		{/if}
	</div>

	{#if validPercentages.length > 1}
		<button
			type="button"
			class="pc-focus group/cycle relative z-10 -mx-1 mt-2 flex min-h-5 items-center self-start rounded px-1 text-left text-sm text-slate-500 transition-colors duration-200 hover:text-slate-800 dark:text-gray-400 dark:hover:text-gray-200"
			aria-label="{shown.value}% {shown.relativeTo}. Show next comparison"
			title="Show next comparison"
			on:click={cyclePercentage}
		>
			<span>
				<span class="font-semibold tabular-nums text-slate-700 transition-colors duration-200 dark:text-gray-200"
					>{shown.value}%</span
				>
				<span class="ml-1">{shown.relativeTo}</span>
			</span>
			<svg
				class="ml-1.5 h-3.5 w-3.5 shrink-0 text-slate-400 transition-colors duration-200 group-hover/cycle:text-slate-600 dark:text-gray-500 dark:group-hover/cycle:text-gray-300"
				fill="none"
				viewBox="0 0 24 24"
				stroke="currentColor"
				stroke-width="1.5"
				aria-hidden="true"
			>
				<path
					stroke-linecap="round"
					stroke-linejoin="round"
					d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5"
				/>
			</svg>
		</button>
	{:else if validPercentages.length === 1}
		<p class="mt-2 min-h-5 text-sm text-slate-500 transition-colors duration-200 dark:text-gray-400">
			<span class="font-semibold tabular-nums text-slate-700 transition-colors duration-200 dark:text-gray-200"
				>{validPercentages[0].value}%</span
			>
			<span class="ml-1">{validPercentages[0].relativeTo}</span>
		</p>
	{:else if reserveContext}
		<div class="mt-2 min-h-5" aria-hidden="true"></div>
	{/if}
</div>

<style>
	@media (prefers-reduced-motion: no-preference) {
		.pulse {
			animation: pulse-animation 0.6s ease-out;
		}
	}

	@keyframes pulse-animation {
		0% {
			opacity: 1;
		}
		30% {
			opacity: 0.45;
		}
		100% {
			opacity: 1;
		}
	}
</style>
