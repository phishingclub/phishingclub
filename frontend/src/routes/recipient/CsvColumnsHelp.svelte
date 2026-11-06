<script>
	// header names the CSV parser in lib/utils/csv.js reads, matched without case
	const optionalColumns = [
		'FirstName',
		'LastName',
		'Phone',
		'ExtraIdentifier',
		'Position',
		'Department',
		'City',
		'Country',
		'Misc'
	];
</script>

<div
	class="w-full lg:w-[36rem] rounded-md border border-dashed border-gray-300 dark:border-gray-600 bg-pleasant-gray dark:bg-gray-900/40 px-4 py-6 sm:px-6 text-sm text-slate-600 dark:text-gray-300 transition-colors duration-200"
>
	<p class="font-semibold text-gray-700 dark:text-gray-200">Choose a CSV file to preview it here</p>
	<p class="mt-2">
		The first row must hold the column names. Names are not case sensitive. Rows without an email
		are skipped.
	</p>
	<p class="mt-4 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
		Required
	</p>
	<div class="mt-1 flex flex-wrap gap-1.5">
		<code
			class="rounded bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 px-1.5 py-0.5 text-xs"
			>Email</code
		>
	</div>
	<p class="mt-3 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
		Optional
	</p>
	<div class="mt-1 flex flex-wrap gap-1.5">
		{#each optionalColumns as column}
			<code
				class="rounded bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 px-1.5 py-0.5 text-xs"
				>{column}</code
			>
		{/each}
	</div>
</div>
