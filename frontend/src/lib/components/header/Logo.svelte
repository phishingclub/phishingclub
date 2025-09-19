<script>
	import { goto } from '$app/navigation';
</script>

<div
	on:click={() => goto('/dashboard/')}
	on:keydown={(e) => e.key === 'Enter' && goto('/dashboard/')}
	tabindex="0"
	role="button"
	class="flex items-center w-40 sm:w-40 md:w-42 lg:w-56 justify-center py-4 my-6 ml-4 cursor-pointer"
>
	<img draggable="false" src="/logo-white.svg" alt="logo" />
</div>

<style>
	div {
		background: none !important;
	}
	div:hover {
		background: none !important;
		background-color: transparent !important;
	}
	div:focus {
		background: none !important;
		background-color: transparent !important;
		outline: none !important;
	}
</style>
