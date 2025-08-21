<script>
	import { page } from '$app/stores';
	export let href = '#';
	export let hidden = false;
</script>

<a
	class="py-2 px-4 text-white hover:bg-active-blue hover:text-white hover:rounded-md"
	class:bg-highlight-blue={$page.url.pathname === href}
	class:font-semibold={$page.url.pathname === href}
	class:rounded-md={$page.url.pathname === href}
	class:hidden
	{href}
	on:click
>
	<slot />
</a>
