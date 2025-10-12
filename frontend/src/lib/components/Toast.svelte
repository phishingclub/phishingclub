<script>
	import { toasts, removeToast } from '$lib/store/toast';
	import { slide, draw } from 'svelte/transition';

	function preload(src) {
		return new Promise(function (resolve) {
			let img = new Image();
			img.onload = resolve;
			img.src = src;
		});
	}

	let src1 = '/t-succes.svg';
</script>

{#if $toasts.length > 0}
	<div
		class="fixed flex flex-col items-center w-3/4 xl:w-2/6 bottom-0 mt-6 lg:bottom-auto mx-auto inset-x-0 z-[100]"
	>
		{#each $toasts as toast (toast.id)}
			{#if toast.type === 'Success'}
				{#await preload(src1) then _}
					<div
						role="button"
						tabindex="0"
						on:click={() => removeToast(toast.id)}
						on:keydown={(e) => {
							if (e.key === 'Enter') {
								removeToast(toast.id);
							}
						}}
						transition:slide|global
						class="flex items-center bg-pleasant-gray dark:bg-gray-800/80 text-gray-500 dark:text-gray-300 shadow-lg dark:shadow-gray-900/50 capitalize text-xl p-4 first:mt-0 mt-4 min-w-max rounded-md justify-self-center w-full transition-colors duration-200"
					>
						<!-- <img class="w-9 mr-6" draggable="false" src={src1} alt="checkmark success" /> -->
						<svg class="w-11 mr-6" viewBox="0 0 32.25 32.4">
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

						{toast.text}
					</div>
				{/await}
			{:else if toast.type === 'Warning'}
				<div
					role="button"
					tabindex="0"
					on:click={() => removeToast(toast.id)}
					on:keydown={(e) => {
						if (e.key === 'Enter') {
							removeToast(toast.id);
						}
					}}
					transition:slide|global
					class="flex items-center bg-pleasant-gray dark:bg-gray-800/80 text-gray-500 dark:text-gray-300 shadow-lg dark:shadow-gray-900/50 capitalize text-xl p-4 first:mt-0 mt-4 min-w-max rounded-md justify-self-center w-full transition-colors duration-200"
				>
					<!-- <img class="w-9 mr-6" draggable="false" src="/t-warning.svg" alt="checkmark warning" /> -->
					<svg class="w-11 mr-6" viewBox="0 0 36.26 32.41">
						<path
							in:draw|global
							style="stroke-width: 2px; fill: none; stroke: #d1c643; stroke-miterlimit: 10;"
							d="M15.74,2.68L1.64,27.11c-1.06,1.84.27,4.13,2.39,4.13h28.21c2.12,0,3.45-2.3,2.39-4.13L20.51,2.68c-1.06-1.84-3.71-1.84-4.77,0Z"
						/>
						<g>
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
						</g>
					</svg>
					{toast.text}
				</div>
			{:else if toast.type === 'Error'}
				<div
					role="button"
					tabindex="0"
					on:click={() => removeToast(toast.id)}
					on:keydown={(e) => {
						if (e.key === 'Enter') {
							removeToast(toast.id);
						}
					}}
					transition:slide|global
					class="flex items-center bg-pleasant-gray dark:bg-gray-800/80 text-gray-500 dark:text-gray-300 shadow-lg dark:shadow-gray-900/50 capitalize text-xl p-4 first:mt-0 mt-4 min-w-max rounded-md justify-self-center w-full transition-colors duration-200"
				>
					<!-- <img class="w-9 mr-6" draggable="false" src="/t-error.svg" alt="checkmark error" /> -->
					<svg class="w-11 mr-6" viewBox="0 0 32.98 32.98">
						<g>
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
						</g>
						<circle
							in:draw|global
							style="stroke-width: 2px; fill: none; stroke: #e06e94; stroke-linecap: round;"
							cx="16.59"
							cy="16.51"
							r="15.49"
						/>
					</svg>
					{toast.text}
				</div>
			{:else}
				<div
					role="button"
					tabindex="0"
					on:click={() => removeToast(toast.id)}
					on:keydown={(e) => {
						if (e.key === 'Enter') {
							removeToast(toast.id);
						}
					}}
					class="flex items-center bg-pleasant-gray dark:bg-gray-800/80 text-gray-500 dark:text-gray-300 shadow-lg dark:shadow-gray-900/50 capitalize text-xl p-4 first:mt-0 mt-4 min-w-max rounded-md justify-self-center w-full transition-colors duration-200"
				>
					<!-- <img class="w-9 mr-6" draggable="false" src="/t-info.svg" alt="checkmark info" /> -->
					<svg class="w-11 mr-6" viewBox="0 0 32.79 32.79">
						<circle
							in:draw|global
							style="stroke-width: 2px; fill: none; stroke: #86b1f2; stroke-linecap: round;"
							cx="16.39"
							cy="16.39"
							r="15.39"
						/>
						<g>
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
						</g>
					</svg>
					{toast.text}
				</div>
			{/if}
		{/each}
	</div>
{/if}
