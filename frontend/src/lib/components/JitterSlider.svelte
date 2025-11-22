<script>
	export let id = 'jitter-slider';
	export let valueMin = 0;
	export let valueMax = 0;

	// jitter options: symmetric jitter (applied randomly as positive or negative)
	const jitterOptions = [
		{ value: 0, label: 'No jitter' },
		{ value: 1, label: '±1 min' },
		{ value: 2, label: '±2 min' },
		{ value: 5, label: '±5 min' },
		{ value: 10, label: '±10 min' },
		{ value: 15, label: '±15 min' },
		{ value: 20, label: '±20 min' },
		{ value: 30, label: '±30 min' },
		{ value: 45, label: '±45 min' },
		{ value: 60, label: '±60 min' },
		{ value: 90, label: '±90 min' },
		{ value: 120, label: '±120 min' }
	];

	let selectedIndex = 0; // default to "no jitter"
	let initialized = false;

	// initialize from incoming values when component first loads
	// this runs once on mount to detect existing jitter values
	$: if (!initialized && valueMax > 0 && valueMin === -valueMax) {
		const index = jitterOptions.findIndex((opt) => opt.value === valueMax);
		if (index >= 0) {
			selectedIndex = index;
			initialized = true;
		}
	}

	function handleInput(event) {
		selectedIndex = parseInt(event.currentTarget.value);
		const jitter = jitterOptions[selectedIndex].value;
		valueMin = -jitter;
		valueMax = jitter;
		if (!initialized) {
			initialized = true;
		}
	}
</script>

<div class="pt-4 pb-6">
	<div class="flex flex-col gap-2">
		<p class="font-semibold text-slate-600 dark:text-gray-400 py-1 transition-colors duration-200">
			<slot>Jitter</slot>

			<span class="italic font-normal">
				({jitterOptions[selectedIndex].label})
			</span>
		</p>
		<div class="flex items-center">
			<input
				{id}
				type="range"
				min="0"
				max="11"
				bind:value={selectedIndex}
				on:input={handleInput}
				class="w-96 h-2 bg-gray-200 dark:bg-gray-700 rounded-lg appearance-none cursor-pointer [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-4 [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-600 [&::-webkit-slider-thumb]:cursor-pointer hover:[&::-webkit-slider-thumb]:bg-blue-700 [&::-moz-range-thumb]:w-4 [&::-moz-range-thumb]:h-4 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:bg-blue-600 [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:cursor-pointer hover:[&::-moz-range-thumb]:bg-blue-700 transition-colors duration-200"
			/>
		</div>
	</div>
</div>
