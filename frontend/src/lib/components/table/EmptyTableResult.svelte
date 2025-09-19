<script>
	/** @type {*} */
	export let page = null;
	export let plural;
	export let colspan = 100;
</script>

<tr class="text-center bg-pleasant-gray dark:bg-gray-700 transition-colors duration-200">
	<td class="p-24" {colspan}>
		{#if page === 1}
			<p class="text-lg text-gray-600 dark:text-gray-300 transition-colors duration-200">
				No {plural} found
			</p>
		{:else}
			<p class="text-lg text-gray-600 dark:text-gray-300 transition-colors duration-200">
				No more results found
			</p>
		{/if}
	</td>
</tr>
