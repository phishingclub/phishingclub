<script>
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import { addToast } from '$lib/store/toast';
	import { stripCaptureEmoji } from '$lib/utils/eventData.js';

	// data is the formatted capture, starting with the cookie or key emoji
	export let data = '';
	export let visible = false;

	$: isToken = data.startsWith('🔑');

	const close = () => {
		visible = false;
		data = '';
	};

	const onClickCopy = async () => {
		try {
			await navigator.clipboard.writeText(stripCaptureEmoji(data));
			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			addToast(isToken ? 'Failed to copy token data' : 'Failed to copy cookie data', 'Error');
			console.error('failed to copy captured data', e);
		}
	};
</script>

<Modal
	headerText={isToken ? 'Tokens captured' : 'Cookies captured'}
	{visible}
	onClose={close}
>
	<div class="mt-4">
		{#if isToken}
			<!-- token capture introduction section -->
			<div>
				<h3
					class="text-lg font-semibold text-pc-darkblue dark:text-white mb-4 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
				>
					Import tokens
				</h3>
				<p class="text-gray-600 dark:text-gray-300 mb-4 transition-colors duration-200">
					Tokens can be imported into <a
						href="https://github.com/phishingclub/session-sushi"
						target="_blank"
						rel="noopener noreferrer"
						class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline">Session Sushi</a
					> to hijack the captured session.
				</p>
			</div>

			<!-- token copy section -->
			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md transition-colors duration-200">
				<button
					class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline font-medium inline-flex items-center gap-2"
					on:click={onClickCopy}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
						></path>
					</svg>
					Copy tokens
				</button>
			</div>
		{:else}
			<!-- cookie capture introduction section -->
			<div>
				<h3
					class="text-lg font-semibold text-pc-darkblue dark:text-white mb-4 pb-2 border-b border-gray-200 dark:border-gray-700 transition-colors duration-200"
				>
					Import cookie
				</h3>
				<p class="text-gray-600 dark:text-gray-300 mb-4 transition-colors duration-200">
					Cookies can be imported and exchanged for tokens using the <a
						href="https://github.com/phishingclub/session-sushi"
						target="_blank"
						rel="noopener noreferrer"
						class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline">Session Sushi</a
					> extension.
				</p>
			</div>

			<!-- cookie copy section -->
			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md transition-colors duration-200">
				<button
					class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline font-medium inline-flex items-center gap-2"
					on:click={onClickCopy}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
						></path>
					</svg>
					Copy cookies
				</button>
			</div>
		{/if}
	</div>
	<FormGrid on:submit={close}>
		<FormColumns>
			<FormColumn>
				<!-- Empty form column for structure -->
			</FormColumn>
		</FormColumns>
		<div
			class="py-4 row-span-2 col-start-1 col-span-3 border-t-2 border-gray-200 dark:border-gray-700 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
		>
			<button
				type="button"
				on:click={close}
				class="bg-blue-600 hover:bg-blue-500 dark:bg-blue-500 dark:hover:bg-blue-400 text-sm uppercase font-bold px-4 py-2 text-white rounded-md transition-colors duration-200"
			>
				Close
			</button>
		</div>
	</FormGrid>
</Modal>
