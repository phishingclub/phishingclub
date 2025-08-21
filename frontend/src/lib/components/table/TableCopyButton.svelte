<script>
	import TableDropDownButton from './TableDropDownButton.svelte';

	export let disabled = false;
	export let title = 'Copy';

	if (disabled && !title) {
		title = 'Disabled';
	}
</script>

<TableDropDownButton name={'Copy'} {title} on:click {disabled} />
