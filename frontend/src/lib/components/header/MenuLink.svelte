<script>
	import { page } from '$app/stores';
	export let href = '#';
	export let hidden = false;
</script>

<a
	class="pl-5 py-2 text-white last:rounded-md first:rounded-t-md transition-colors duration-200"
	class:hover:shadow-md={$page.url.pathname !== href}
	class:hover:bg-highlight-blue={$page.url.pathname !== href}
	class:dark:hover:bg-gray-600={$page.url.pathname !== href}
	class:bg-active-blue={$page.url.pathname === href}
	class:dark:bg-gray-700={$page.url.pathname === href}
	class:shadow-md={$page.url.pathname === href}
	class:hidden
	{href}
	on:click
>
	<slot />
</a>
