<script>
	import { onMount } from 'svelte';
	import FormButton from './FormButton.svelte';

	export let isSubmitting = false;
	export let closeModal = () => {};
	export let okText = 'Save';
	export let closeText = 'Cancel';
	export let disabled = false;

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
	class="form-footer py-4 col-start-1 col-span-3 border-t-2 border-gray-200 dark:border-gray-700 w-full flex flex-row flex-wrap justify-end items-center gap-2 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<button
		type="reset"
		on:click|preventDefault={onCloseModal}
		class="pc-focus min-w-[6rem] bg-transparent hover:bg-gray-100 border border-gray-300 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm uppercase font-bold px-4 py-2 rounded-md transition-colors duration-200"
		>{closeText}</button
	>
	<FormButton {isSubmitting} {disabled}>{okText}</FormButton>
</div>

<style>
	/* inside a modal the footer stays in view while the body scrolls */
	:global([role='dialog']) .form-footer {
		position: sticky;
		bottom: 0;
		z-index: 10;
		margin-left: -1rem;
		margin-right: -1rem;
		padding-left: 1rem;
		padding-right: 1rem;
		width: auto;
	}
	@media (min-width: 640px) {
		:global([role='dialog']) .form-footer {
			margin-left: -2rem;
			margin-right: -2rem;
			padding-left: 2rem;
			padding-right: 2rem;
		}
	}
</style>
