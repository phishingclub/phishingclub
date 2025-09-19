<script>
	// TODO consider renaming component this to ? as it is more than just an input

	// props
	export let fieldName = '';
	export let type = 'text';
	export let value = '';
	export let element = {};
	export let submitOnEnter = false; // new prop to control Enter behavior
</script>

<div class="flex flex-col w-full p-4 h-24">
	<label
		for={fieldName}
		class="text-md font-semibold font-titilium text-pc-darkblue dark:text-gray-200"
		>{fieldName}</label
	>
	<input
		bind:this={element}
		on:keyup={(event) => {
			const t = /** @type {HTMLInputElement} */ (event.target);
			value = t.value;
			// If submitOnEnter is true and Enter was pressed, submit the closest form
			if (submitOnEnter && event.key === 'Enter') {
				const form = /** @type {HTMLElement} */ (event.target).closest('form');
				if (form) {
					form.requestSubmit();
				}
			}
		}}
		{value}
		required
		autocomplete="off"
		{type}
		id={fieldName}
		name={fieldName}
		class="w-full p-2 rounded bg-pc-lightblue dark:bg-gray-700 dark:border-gray-600 dark:text-white dark:placeholder-gray-400 focus:outline-none focus:ring-0 focus:border-cta-blue dark:focus:border-blue-500 focus:border-2 transition-colors duration-200"
	/>
</div>
