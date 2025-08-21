<script>
	import { onClickCopy } from '$lib/utils/common';

	export let text;
</script>

<button
	class="hover:bg-gray-100 px-2 py-1 rounded-md transition-colors w-full text-left text-ellipsis overflow-hidden"
	title={text}
	on:click={() => onClickCopy(text)}
>
	{text}
</button>
