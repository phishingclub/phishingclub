<script>
	import Alert from '../Alert.svelte';
	import TextField from '../TextField.svelte';

	export let onClick;
	export let name;
	export let list = [];
	export let title = 'Delete';
	export let isVisible = false;
	export let confirm = false;
	export let confirmWord = 'confirm';
	export let permanent = true;

	let confirmText = '';

	const onConfirmDelete = async () => {
		if (confirm && confirmWord !== confirmText) {
			throw `Type '${confirmWord}' to delete`;
		}

		try {
			const res = await onClick();
			if (res?.success) {
				isVisible = false;
				return res;
			}
			throw res?.error || 'Failed to delete';
		} catch (e) {
			console.error('failed to delete record', e);
			throw e;
		}
	};

	$: {
		if (!isVisible) {
			confirmText = '';
		}
	}
</script>

{#if isVisible}
	<Alert headline={title} onConfirm={onConfirmDelete} bind:visible={isVisible}>
		<div class="space-y-6">
			<!-- Main Delete Warning -->
			<div>
				<!--
				<h3 class="text-lg font-medium text-gray-900">Delete {type}</h3>
				-->
				<p class="mt-2 text-gray-600 dark:text-gray-300">
					Are you sure you want to delete
					{#if name?.length > 30}
						<br />
					{/if}
					<span class="font-medium text-gray-900 dark:text-gray-100">"{name}"</span>?
				</p>
			</div>

			<!-- Impact Section -->
			{#if list.length}
				<div class="bg-gray-50 dark:bg-gray-700 rounded-lg p-4 transition-colors duration-200">
					<p class="font-medium text-gray-900 dark:text-gray-100 mb-3">Side effects:</p>
					<ul class="space-y-2 ml-4 list-disc text-gray-600 dark:text-gray-300">
						{#each list as line}
							<li>{line}</li>
						{/each}
					</ul>
				</div>
			{/if}

			<!-- Confirmation Input -->
			{#if confirm}
				<div class="mt-2">
					<TextField
						bind:value={confirmText}
						required
						id="confirmDelete"
						on:keydown={(e) => {
							const ele = /** @type {HTMLInputElement} */ (e.target);
							ele.setCustomValidity('');
						}}
					>
						Type '{confirmWord}' to confirm deletion
					</TextField>
				</div>
			{/if}

			{#if permanent}
				<p class="text-red-700 dark:text-red-400 font-medium">This action cannot be undone.</p>
			{/if}
		</div>
	</Alert>
{/if}
