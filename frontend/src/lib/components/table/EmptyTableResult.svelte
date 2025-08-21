<script>
	/** @type {*} */
	export let page = null;
	export let plural;
	export let colspan = 100;
</script>

<tr class="text-center bg-pleasant-gray">
	<td class="p-24" {colspan}>
		{#if page === 1}
			<p class="text-lg text-gray-600">No {plural} found</p>
		{:else}
			<p class="text-lg text-gray-600">No more results found</p>
		{/if}
	</td>
</tr>
