<script>
	import { toasts, removeToast } from '$lib/store/toast';
	import { slide, draw } from 'svelte/transition';
</script>

<!-- screen reader announcements, kept mounted so new messages are read out -->
<div class="sr-only" role="status" aria-live="polite">
	{#each $toasts.filter((t) => t.type !== 'Error') as toast (toast.id)}
		<p>{toast.text}</p>
	{/each}
</div>
<div class="sr-only" role="alert">
	{#each $toasts.filter((t) => t.type === 'Error') as toast (toast.id)}
		<p>{toast.text}</p>
	{/each}
</div>

{#if $toasts.length > 0}
<div
	class="fixed inset-x-0 bottom-4 lg:bottom-auto lg:top-4 z-[100] mx-auto flex flex-col items-center w-full px-4 sm:w-3/4 sm:px-0 xl:w-2/6 pointer-events-none"
>
	<div class="w-full flex flex-col items-center">
		{#each $toasts.filter((t) => t.type !== 'Error') as toast (toast.id)}
			<div
				transition:slide|global
				class="toast pointer-events-auto flex items-center gap-3 sm:gap-4 bg-pleasant-gray dark:bg-gray-800 text-gray-700 dark:text-gray-200 border border-gray-200 dark:border-gray-700 shadow-lg dark:shadow-gray-900/50 text-base sm:text-lg p-3 sm:p-4 mt-3 first:mt-0 rounded-md w-full transition-colors duration-200"
			>
				{#if toast.type === 'Success'}
					<svg class="w-8 sm:w-10 shrink-0" viewBox="0 0 32.25 32.4" aria-hidden="true">
						<path
							in:draw
							style="stroke-width: 2px; fill: none; stroke: #5dd8c4; stroke-linecap: round;"
							d="M31.25,16.28c0,8.35-6.77,15.12-15.12,15.12S1,24.63,1,16.28,7.77,1.15,16.12,1.15c1.56,0,3.06.24,4.47.67"
						/>
						<path
							in:draw
							style="stroke-width: 2px; fill: none; stroke: #5dd8c4; stroke-linecap: round;"
							d="M9.25,17.71c.5.33,6.75,6.69,6.75,6.69L30.41,1.5"
						/>
					</svg>
				{:else if toast.type === 'Warning'}
					<svg class="w-8 sm:w-10 shrink-0" viewBox="0 0 36.26 32.41" aria-hidden="true">
						<path
							in:draw|global
							style="stroke-width: 2px; fill: none; stroke: #d1c643; stroke-miterlimit: 10;"
							d="M15.74,2.68L1.64,27.11c-1.06,1.84.27,4.13,2.39,4.13h28.21c2.12,0,3.45-2.3,2.39-4.13L20.51,2.68c-1.06-1.84-3.71-1.84-4.77,0Z"
						/>
						<line
							in:draw|global
							style="stroke-width: 3px; fill: none; stroke: #d1c643; stroke-miterlimit: 10;"
							x1="17.94"
							y1="19.84"
							x2="17.94"
							y2="7.67"
						/>
						<line
							in:draw|global
							style="stroke-width: 3px; fill: none; stroke: #d1c643; stroke-miterlimit: 10;"
							x1="17.94"
							y1="26.55"
							x2="17.94"
							y2="25.5"
						/>
					</svg>
				{:else}
					<svg class="w-8 sm:w-10 shrink-0" viewBox="0 0 32.79 32.79" aria-hidden="true">
						<circle
							in:draw|global
							style="stroke-width: 2px; fill: none; stroke: #86b1f2; stroke-linecap: round;"
							cx="16.39"
							cy="16.39"
							r="15.39"
						/>
						<line
							in:draw|global
							style="stroke-width: 3px; fill: none; stroke: #86b1f2; stroke-linecap: round;"
							x1="16.39"
							y1="13.71"
							x2="16.39"
							y2="25.67"
						/>
						<line
							in:draw|global
							style="stroke-width: 3px; fill: none; stroke: #86b1f2; stroke-linecap: round;"
							x1="16.39"
							y1="7.12"
							x2="16.39"
							y2="8.15"
						/>
					</svg>
				{/if}
				<p class="flex-1 min-w-0 break-words first-letter:uppercase select-text">{toast.text}</p>
				<button
					type="button"
					class="pc-focus shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md text-gray-500 hover:text-gray-800 hover:bg-gray-200 dark:text-gray-400 dark:hover:text-white dark:hover:bg-gray-700 transition-colors duration-200"
					aria-label="Dismiss"
					on:click={() => removeToast(toast.id)}
				>
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
					</svg>
				</button>
			</div>
		{/each}
	</div>
	<div class="w-full flex flex-col items-center">
		{#each $toasts.filter((t) => t.type === 'Error') as toast (toast.id)}
			<div
				transition:slide|global
				class="toast pointer-events-auto flex items-center gap-3 sm:gap-4 bg-pleasant-gray dark:bg-gray-800 text-gray-700 dark:text-gray-200 border border-gray-200 dark:border-gray-700 shadow-lg dark:shadow-gray-900/50 text-base sm:text-lg p-3 sm:p-4 mt-3 rounded-md w-full transition-colors duration-200"
			>
				<svg class="w-8 sm:w-10 shrink-0" viewBox="0 0 32.98 32.98" aria-hidden="true">
					<line
						in:draw|global
						style="stroke-width: 3px; fill: none; stroke: #e06e94; stroke-linecap: round;"
						x1="10.51"
						y1="10.42"
						x2="22.68"
						y2="22.59"
					/>
					<line
						in:draw|global
						style="stroke-width: 3px; fill: none; stroke: #e06e94; stroke-linecap: round;"
						x1="10.51"
						y1="22.59"
						x2="22.29"
						y2="10.81"
					/>
					<circle
						in:draw|global
						style="stroke-width: 2px; fill: none; stroke: #e06e94; stroke-linecap: round;"
						cx="16.59"
						cy="16.51"
						r="15.49"
					/>
				</svg>
				<p class="flex-1 min-w-0 break-words first-letter:uppercase select-text">{toast.text}</p>
				<button
					type="button"
					class="pc-focus shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md text-gray-500 hover:text-gray-800 hover:bg-gray-200 dark:text-gray-400 dark:hover:text-white dark:hover:bg-gray-700 transition-colors duration-200"
					aria-label="Dismiss"
					on:click={() => removeToast(toast.id)}
				>
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
					</svg>
				</button>
			</div>
		{/each}
	</div>
</div>
{/if}
