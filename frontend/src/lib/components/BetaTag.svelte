<script>
	import Tag from './Tag.svelte';

	// Marks a feature that is not final yet (Beta, Experimental). Use onDark on the blue menus.
	export let onDark = false;
	export let label = 'Beta';
	export let title = `This feature is ${label === 'Beta' ? 'in beta' : label.toLowerCase()}`;
</script>

<Tag {onDark} {title} class="ml-2">{label}</Tag>
