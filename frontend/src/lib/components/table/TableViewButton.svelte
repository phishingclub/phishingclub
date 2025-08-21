<script>
	import TableDropDownButton from './TableDropDownButton.svelte';

	export let disabled = false;
	export let title = '';
	export let name = 'View';
	if (disabled && !title) {
		title = 'Disabled';
	}
</script>

<TableDropDownButton {name} {title} on:click {disabled} />
