<script>
	import { onClickCopy } from '$lib/utils/common';

	export let text;
</script>

<button
	class="hover:bg-gray-100 dark:hover:bg-gray-800/40 px-2 py-1 rounded-md transition-colors w-full text-left text-ellipsis overflow-hidden text-gray-900 dark:text-gray-200"
	title={text}
	on:click={() => onClickCopy(text)}
>
	{text}
</button>
