<script>
	// dimmed visually mutes the row (e.g. for SCIM-disabled recipients)
	export let dimmed = false;
	// title is shown as a native hover tooltip on the row
	export let title = '';
</script>

<tr
	{title}
	class="table-row odd:bg-white odd:dark:bg-gray-900/40 even:bg-pleasant-gray even:dark:bg-gray-800/30 h-10 hover:bg-cta-light-blue hover:dark:bg-highlight-blue/20 transition-colors duration-200 {dimmed
		? 'opacity-50'
		: ''}"
>
	<slot />
</tr>
