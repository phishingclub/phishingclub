<script>
	import { onMount } from 'svelte';
	import { fade } from 'svelte/transition';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';

	// servies
	const appState = AppStateService.instance;

	// local state
	let state = {};
	let visible = false;

	onMount(() => {
		appState.subscribe((s) => {
			state = s;
		});
		// on unmount
		return () => {};
	});

	const triggerToast = (type) => {
		addToast('Test toast with message.', type);
	};
</script>

{#if import.meta.env.DEV}
	<div>
		<div>
			<button
				class="fixed border-2 border-slate-500 dark:border-slate-400 bg-white dark:bg-gray-700 w-8 h-8 left-2 pb-1 bottom-4 text-white rounded-full hover:right-3 hover:bottom-3 hover:w-10 hover:h-10 transition-all"
				on:click={() => (visible = !visible)}
			>
				{#if visible}
					🎣
				{:else}
					🎣
				{/if}
			</button>
		</div>
		{#if visible}
			<div
				transition:fade={{ duration: 100 }}
				class="absolute right-0 h-auto bg-black dark:bg-gray-900 text-white p-4 z-40 border border-gray-600 dark:border-gray-500"
			>
				<h1 class="text-xl m-4">Developer Panel</h1>
				<h2 class="text-lg font-bold m-4">Links</h2>
				<ul class="m-4">
					<li>
						<a
							href="http://localhost:8101"
							target="_blank"
							class="text-blue-400 hover:text-blue-300 underline">Database</a
						>
					</li>
					<li>
						<a
							href="http://localhost:8102"
							target="_blank"
							class="text-blue-400 hover:text-blue-300 underline">Mailbox</a
						>
					</li>
					<li>
						<a
							href="http://localhost:8103"
							target="_blank"
							class="text-blue-400 hover:text-blue-300 underline">Container logs</a
						>
					</li>
					<li>
						<a
							href="http://localhost:8104"
							target="_blank"
							class="text-blue-400 hover:text-blue-300 underline">Container stats</a
						>
					</li>
				</ul>
				<h2 class="text-lg font-bold m-4">Toast</h2>
				<ul class="m-4">
					<li>
						<button
							on:click={() => triggerToast('Success')}
							class="text-green-400 hover:text-green-300 underline">Trigger toast - Success</button
						>
					</li>
					<li>
						<button
							on:click={() => triggerToast('Info')}
							class="text-blue-400 hover:text-blue-300 underline">Trigger toast - Info</button
						>
					</li>
					<li>
						<button
							on:click={() => triggerToast('Warning')}
							class="text-yellow-400 hover:text-yellow-300 underline"
							>Trigger toast - Warning</button
						>
					</li>
					<li>
						<button
							on:click={() => triggerToast('Error')}
							class="text-red-400 hover:text-red-300 underline">Trigger toast - Error</button
						>
					</li>
				</ul>
				<div class="pt-4">
					<h2 class="text-lg font-bold">Global State</h2>
					<table class="border-2 border-gray-400 dark:border-gray-500">
						{#each Object.entries(state) as [key, value]}
							<tr class="flex flex-col border-2 border-white dark:border-gray-600">
								<td class="p-4 font-bold border-1 border-white dark:border-gray-600">{key}</td>
								<td class="p-4 border-1 border-white dark:border-gray-600 w-full">
									{#if typeof value === 'object'}
										<pre class="whitespace-pre-wrap">{JSON.stringify(value, null, 2)}</pre>
									{:else}
										<p>
											{value}
										</p>
									{/if}
								</td>
							</tr>
						{/each}
					</table>
				</div>
			</div>
		{/if}
	</div>
{/if}
