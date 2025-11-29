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
			// only trigger if the form or its descendants have focus and we're in update mode
			if (modalMode === 'update' && formElement && formElement.contains(document.activeElement)) {
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
	class="grid grid-cols-3 grid-rows-1 w-full h-full flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
	class:opacity-70={isSubmitting}
	bind:this={formElement}
	{novalidate}
>
	<slot />
</form>
