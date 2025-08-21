<script>
	import { afterUpdate, onDestroy, onMount } from 'svelte';
	import ToolTip from './ToolTip.svelte';

	export let bindTo = null;
	export let defaultValue = false;
	export let value = defaultValue;
	export let toolTipText = '';
	export let optional = false;
	export let id = null;

	let parentForm = null;
	let parentFormResetListener = null;

	onMount(() => {
		if (value === null) {
			value = defaultValue;
		}
	});

	afterUpdate(() => {
		if (!parentForm) {
			parentForm = bindTo.closest('form');
			if (!parentForm) {
				return;
			}
			parentFormResetListener = parentForm.addEventListener('reset', (event) => {
				event.preventDefault();
				value = defaultValue;
			});
		}
	});

	onDestroy(() => {
		if (parentFormResetListener) {
			parentForm.removeEventListener('reset', parentFormResetListener);
		}
	});
</script>

<label class="flex flex-col py-2 w-60">
	<div class="flex items-center">
		<p class="font-semibold text-slate-600 py-2">
			<slot />
		</p>
		{#if toolTipText.length > 0}
			<ToolTip>
				{toolTipText}
			</ToolTip>
		{/if}
		{#if optional === true}
			<div class="bg-gray-100 ml-2 px-2 rounded-md">
				<p class="text-slate-600 text-xs">optional</p>
			</div>
		{/if}
	</div>
	<div class="mt-1">
		<label class="relative flex items-center cursor-pointer">
			<input
				{id}
				type="checkbox"
				class="peer sr-only"
				bind:this={bindTo}
				bind:checked={value}
				on:change
			/>
			<div
				class="w-5 h-5 border-2 border-slate-300 rounded
                        peer-checked:border-cta-blue peer-checked:bg-cta-blue
                        transition-all duration-200 ease-in-out
                        flex items-center justify-center
						bg-slate-50
                        focus-within:ring-2 focus-within:ring-cta-blue focus-within:ring-offset-2"
			>
				{#if value}
					<svg class="w-3 h-3 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="3"
							d="M5 13l4 4L19 7"
						/>
					</svg>
				{/if}
			</div>
		</label>
	</div>
</label>
