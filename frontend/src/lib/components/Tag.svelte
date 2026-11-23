<script>
	// a small label for what kind of thing something is (Test, Training,
	// Dynamic, SCIM, Allow), in the label voice of the sidebar headings and
	// buttons. A state uses StatusBadge instead: a dot and plain text.
	export let title = '';
	// lighter colors for the dark blue menus
	export let onDark = false;
	// color classes in place of the default gray, for tags whose color is data,
	// such as the colors a Bait Box registry gives its tags
	export let colors = '';
	let cssClass = '';
	export { cssClass as class };

	$: palette =
		colors ||
		(onDark
			? 'bg-white/10 text-blue-100 dark:text-highlight-blue'
			: 'bg-grayblue-light text-slate-700 dark:bg-gray-700 dark:text-gray-200');
</script>

<span
	class="inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded px-2 py-0.5 align-middle text-xs font-semibold uppercase leading-4 transition-colors duration-200 {palette} {cssClass}"
	title={title || undefined}
>
	<slot />
</span>
