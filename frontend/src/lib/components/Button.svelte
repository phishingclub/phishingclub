<script>
	export let backgroundColor = 'bg-cta-blue';
	/* 
	linear-gradient(#64a6e6, #9198e5);
	*/
	export let css = '';
	export let size = 'medium';
</script>

<button
	class="{backgroundColor} px-6 py-2 text-white rounded-md uppercase font-semibold hover:opacity-80 text-sm {css}"
	class:w-32={size === 'small'}
	class:w-36={size === 'medium'}
	class:w-60={size === 'large'}
	on:click
>
	<slot />
</button>
