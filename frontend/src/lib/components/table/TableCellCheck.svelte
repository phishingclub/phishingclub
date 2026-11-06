<script>
	export let value = false;
</script>

<td class="px-4 text-center border-hidden">
	<span
		class="font-regular transition-colors duration-200 {value
			? 'text-slate-700 dark:text-gray-200'
			: 'text-slate-500 dark:text-gray-400'}"
	>
		{value ? 'Yes' : 'No'}
	</span>
</td>
