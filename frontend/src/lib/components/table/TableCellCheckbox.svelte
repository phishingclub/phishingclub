<script>
	export let checked = false;
	export let disabled = false;
</script>

<td class="pl-4 w-12 text-center border-hidden align-middle">
	<label
		class="relative inline-flex items-center"
		class:cursor-pointer={!disabled}
		class:cursor-not-allowed={disabled}
		class:opacity-50={disabled}
	>
		<input type="checkbox" class="peer sr-only" {checked} {disabled} on:change tabindex="0" />
		<div
			class="w-5 h-5 border-2 border-slate-300 dark:border-gray-700/60 rounded
				peer-checked:border-cta-blue dark:peer-checked:border-highlight-blue/80 peer-checked:bg-cta-blue dark:peer-checked:bg-highlight-blue/80
				peer-focus:border-slate-400 dark:peer-focus:border-highlight-blue/80
				transition-all duration-200 ease-in-out
				flex items-center justify-center
				bg-slate-50 dark:bg-gray-900/60"
		>
			{#if checked}
				<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
				</svg>
			{/if}
		</div>
	</label>
</td>
