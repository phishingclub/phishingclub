<script>
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';

	export let value = 'page'; // 'page' or 'proxy'
	export let disabled = false;

	const options = [
		{ value: 'page', label: 'Page' },
		{ value: 'proxy', label: 'Proxy' }
	];

	function handleChange(newValue) {
		if (!disabled) {
			value = newValue;
		}
	}
</script>

<div class="inline-flex bg-gray-100 dark:bg-gray-800/60 rounded p-0.5" role="group" aria-label="Type">
	{#each options as option}
		<button
			type="button"
			class="w-6 h-6 text-xs rounded transition-colors duration-150 flex items-center justify-center focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {value ===
			option.value
				? 'bg-white dark:bg-gray-700/80 text-cta-blue dark:text-white shadow-sm'
				: 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white'} {disabled
				? 'opacity-50 cursor-not-allowed'
				: 'cursor-pointer'}"
			on:click={() => handleChange(option.value)}
			{disabled}
			title={option.label}
			aria-label={option.label}
			aria-pressed={value === option.value}
		>
			{#if option.value === 'proxy'}
				<ProxySvgIcon size="w-4 h-4" />
			{:else}
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
				</svg>
			{/if}
		</button>
	{/each}
</div>
