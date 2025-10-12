<script>
	import ProxySvgIcon from '$lib/components/ProxySvgIcon.svelte';

	export let value = 'page'; // 'page' or 'proxy'
	export let disabled = false;

	const options = [
		{ value: 'page', icon: '📄', label: 'Page' },
		{ value: 'proxy', icon: 'proxy', label: 'Proxy' }
	];

	function handleChange(newValue) {
		if (!disabled) {
			value = newValue;
		}
	}
</script>

<div class="inline-flex bg-gray-100 dark:bg-gray-800/60 rounded p-0.5">
	{#each options as option}
		<button
			type="button"
			class="w-6 h-6 text-xs rounded transition-colors duration-150 flex items-center justify-center {value ===
			option.value
				? 'bg-white dark:bg-gray-700/80 text-gray-900 dark:text-white shadow-sm'
				: 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white'} {disabled
				? 'opacity-50 cursor-not-allowed'
				: 'cursor-pointer'}"
			on:click={() => handleChange(option.value)}
			{disabled}
			title={option.label}
		>
			{#if option.icon === 'proxy'}
				<ProxySvgIcon size="w-4 h-4" />
			{:else}
				{option.icon}
			{/if}
		</button>
	{/each}
</div>
