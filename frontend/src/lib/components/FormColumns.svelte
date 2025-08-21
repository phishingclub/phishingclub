<script>
	export let id = null;
</script>

<div
	class="scrollX col-start-1 col-span-3 row-start-1 row-span-5 py-8 flex-col flex sm:flex-col md:flex-col lg:flex-row justify-center"
	{id}
>
	<slot />
</div>
