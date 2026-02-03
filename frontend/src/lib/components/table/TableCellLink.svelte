<script>
	export let href;
	export let title = '';
</script>

<td
	class="pl-4 font-regular text-slate-600 dark:text-gray-200 text-ellipsis whitespace-nowrap overflow-hidden pr-4 transition-colors duration-200 max-w-0"
	{title}
>
	<a {href} class="block w-full py-1 text-ellipsis whitespace-nowrap overflow-hidden">
		<slot />
	</a>
</td>
