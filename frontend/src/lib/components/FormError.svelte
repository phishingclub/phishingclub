<script>
	export let message = '';
	import { slide } from 'svelte/transition';
</script>

{#if message}
	<div class="flex col-span-12 justify-center p-4 select-text">
		<div class="w-80 flex col-span-12 justify-center">
			<div
				class="flex items-center w-full bg-pleasant-gray dark:bg-gray-700 rounded-md border border-gray-200 dark:border-gray-600 text-center p-2 font-titilium transition-colors duration-200"
			>
				<svg class="w-12 ml-4" transition:slide viewBox="0 0 32.94 32.94">
					<circle
						style="stroke-width: 2px; fill: none; stroke: #e06e94;"
						cx="16.47"
						cy="16.47"
						r="15.47"
					/>
					<g>
						<line
							style="stroke-width: 3px; stroke-linecap: round; fill: none; stroke: #e06e94;"
							x1="16.45"
							y1="6.06"
							x2="16.45"
							y2="19.82"
						/>
						<line
							style="stroke-width: 3px; stroke-linecap: round; fill: none; stroke: #e06e94;"
							x1="16.45"
							y1="24.4"
							x2="16.45"
							y2="26.22"
						/>
					</g>
				</svg>
				<p class="pl-4 text-gray-700 dark:text-gray-200 transition-colors duration-200">
					{message}
				</p>
			</div>
		</div>
	</div>
{/if}
