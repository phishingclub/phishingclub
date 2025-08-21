<script>
	import ToolTip from './ToolTip.svelte';

	export let options = [];
	export let value = null;
	export let label = '';
	export let center = true;
	export let optional = false;
	export let toolTipText = '';
	export let width = 'medium';
	/** @type {*} */
	export let onChange = () => {};
</script>

<div class="flex flex-col gap-2 py-2">
	{#if label}
		<div class="flex flex-row">
			<div class="font-semibold text-slate-600">{label}</div>
			{#if toolTipText.length > 0}
				<ToolTip>
					{toolTipText}
				</ToolTip>
			{/if}
			{#if optional}
				<div class="bg-gray-100 ml-2 px-2 rounded-md">
					<p class="text-slate-600 text-xs">optional</p>
				</div>
			{/if}
		</div>
	{/if}

	<div class="flex gap-4 py-2" class:justify-center={center}>
		{#each options as option}
			<button
				type="button"
				class:h32={option.icon && option.description}
				class:h16={!option.icon && !option.description}
				class:w-28={width === 'small'}
				class:w-40={width === 'medium'}
				class:w-64={width === 'large'}
				class={`
          p-3 rounded-lg border-2 transition-all duration-200
          flex flex-col items-center justify-center text-center
          w-40
          hover:border-blue-300 hover:bg-blue-50
          ${
						value === option.value
							? 'border-green-500 bg-green-50 text-green-700 '
							: 'border-gray-200 bg-white text-gray-700 '
					}
        `}
				on:click={() => {
					value = option.value;
					onChange();
				}}
			>
				{#if option.icon}
					<span class="text-xl mb-2">{option.icon}</span>
				{/if}
				<span class="font-medium text-sm">{option.label}</span>
				{#if option.description}
					<span class="text-xs text-gray-500 mt-1">{option.description}</span>
				{/if}
			</button>
		{/each}
	</div>
</div>
