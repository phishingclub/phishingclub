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
				class="fixed border-2 border-slate-500 bg-white w-8 h-8 left-2 pb-1 bottom-4 text-white rounded-full hover:right-3 hover:bottom-3 hover:w-10 hover:h-10 transition-all"
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
				class="absolute right-0 h-auto bg-black text-white p-4 z-40"
			>
				<h1 class="text-xl m-4">Developer Panel</h1>
				<h2 class="text-lg font-bold m-4">Links</h2>
				<ul class="m-4">
					<li>
						<a href="http://localhost:8101" target="_blank">Database</a>
					</li>
					<li>
						<a href="http://localhost:8102" target="_blank">Mailbox</a>
					</li>
					<li>
						<a href="http://localhost:8103" target="_blank">Container logs</a>
					</li>
					<li>
						<a href="http://localhost:8104" target="_blank">Container stats</a>
					</li>
				</ul>
				<h2 class="text-lg font-bold m-4">Toast</h2>
				<ul class="m-4">
					<li>
						<button on:click={() => triggerToast('Success')}>Trigger toast - Success</button>
					</li>
					<li>
						<button on:click={() => triggerToast('Info')}>Trigger toast - Info</button>
					</li>
					<li>
						<button on:click={() => triggerToast('Warning')}>Trigger toast - Warning</button>
					</li>
					<li>
						<button on:click={() => triggerToast('Error')}>Trigger toast - Error</button>
					</li>
				</ul>
				<div class="pt-4">
					<h2 class="text-lg font-bold">Global State</h2>
					<table class="border-2">
						{#each Object.entries(state) as [key, value]}
							<tr class="flex flex-col border-2 border-white">
								<td class="p-4 font-bold border-1 border-white">{key}</td>
								<td class="p-4 border-1 border-white w-full">
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
