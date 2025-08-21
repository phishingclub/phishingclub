<script>
	import { onMount } from 'svelte';
	import FormButton from './FormButton.svelte';

	export let isSubmitting = false;
	export let closeModal = () => {};
	export let okText = 'Save';
	export let closeText = 'Cancel';

	export const onCloseModal = () => {
		closeModal();
	};

	onMount(() => {
		return () => {
			isSubmitting = false;
		};
	});
</script>

<div
	class="py-4 row-span-2 col-start-1 col-span-3 border-t-2 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end"
>
	<button
		type="reset"
		on:click|preventDefault={onCloseModal}
		class="bg-slate-400 hover:bg-slate-300 text-sm mr-2 uppercase font-bold px-4 py-2 text-white rounded-md"
		>{closeText}</button
	>
	<FormButton {isSubmitting}>{okText}</FormButton>
</div>
