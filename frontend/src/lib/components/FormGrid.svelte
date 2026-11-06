<script>
	import { createEventDispatcher, onMount, onDestroy } from 'svelte';

	export let bindTo = null;

	$: if (formElement) {
		bindTo = formElement;
	}
	export let isSubmitting = false;
	export let novalidate = false;
	export let modalMode = null; // 'create', 'update', 'copy'

	const dispatch = createEventDispatcher();
	let formElement = null;

	function handleKeydown(event) {
		if (event.ctrlKey && event.key === 's') {
			// trigger if the form or its descendants have focus and we're in create, update, or copy mode
			if (
				(modalMode === 'create' || modalMode === 'update' || modalMode === 'copy') &&
				formElement &&
				formElement.contains(document.activeElement)
			) {
				event.preventDefault();
				event.stopPropagation();
				event.stopImmediatePropagation();
				// dispatch to our event handler, not native form submit
				dispatch('submit', { saveOnly: true });
			}
		}
	}

	onMount(() => {
		window.addEventListener('keydown', handleKeydown);
	});

	onDestroy(() => {
		window.removeEventListener('keydown', handleKeydown);
	});
</script>

<form
	on:submit|preventDefault
	class="form-grid flex flex-col w-full h-full min-w-0 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:opacity-70={isSubmitting}
	bind:this={formElement}
	{novalidate}
>
	<slot />
</form>

<style>
	/* the first block fills spare height, which fullscreen editors rely on */
	.form-grid > :global(:first-child) {
		flex: 1 1 auto;
		min-height: 0;
	}
</style>
