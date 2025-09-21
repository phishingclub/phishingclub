<script>
	export let href;
	export let title = '';
</script>

<td
	class="pl-4 font-regular text-slate-600 dark:text-gray-300 text-ellipsis whitespace-nowrap overflow-hidden pr-4 transition-colors duration-200"
	{title}
>
	<a href={href} class="block w-full py-1">
		<slot />
	</a>
</td>
