<script>
	// keeps data such as emails and domains in their real case
	export let preserveCase = false;
</script>

<h2
	class="text-lg text-gray-600 dark:text-gray-300 font-bold break-words transition-colors duration-200"
	class:uppercase={!preserveCase}
>
	<slot />
</h2>
