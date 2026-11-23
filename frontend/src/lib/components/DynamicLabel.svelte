<script>
	import Tag from './Tag.svelte';

	// spacing from the caller, such as ml-2 after a name
	let cssClass = '';
	export { cssClass as class };
</script>

<Tag title="Dynamic group" class={cssClass}>Dynamic</Tag>
