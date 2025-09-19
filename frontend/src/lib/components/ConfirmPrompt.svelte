<script>
	import { onMount, tick } from 'svelte';
	import { beforeNavigate } from '$app/navigation';

	export let confirm_text = '';
	export let visible = true;
	export let onConfirm = () => {};
	export let onCancel = () => {};

	let confirmElement;
	let previousActiveElement;
	let focusableElements = [];
	let firstFocusableElement;
	let lastFocusableElement;
	let confirmInitialized = false;

	$: {
		if (visible && !confirmInitialized) {
			window.addEventListener('keydown', keyHandler);
			// Prevent body scrolling when confirm prompt is open
			document.body.style.overflow = 'hidden';
			handleConfirmOpen();
			confirmInitialized = true;
		} else if (!visible && confirmInitialized) {
			window.removeEventListener('keydown', keyHandler);
			// Restore body scrolling when confirm prompt is closed
			document.body.style.overflow = 'auto';
			handleConfirmClose();
			confirmInitialized = false;
		}
	}

	const keyHandler = (e) => {
		if (e.key === 'Escape') {
			close();
		} else if (e.key === 'Tab') {
			handleTabKey(e);
		}
	};

	const handleTabKey = (e) => {
		const currentlyFocused = document.activeElement;
		updateFocusableElements();

		if (focusableElements.length === 0) return;

		e.preventDefault();

		let currentIndex = focusableElements.indexOf(currentlyFocused);

		if (currentIndex === -1) {
			// If we can't find the element, handle it gracefully
			if (e.shiftKey) {
				lastFocusableElement?.focus();
			} else {
				firstFocusableElement?.focus();
			}
			return;
		}

		if (e.shiftKey) {
			// Shift + Tab - go to previous element
			if (currentIndex <= 0) {
				lastFocusableElement?.focus();
			} else {
				focusableElements[currentIndex - 1]?.focus();
			}
		} else {
			// Tab - go to next element
			if (currentIndex >= focusableElements.length - 1) {
				firstFocusableElement?.focus();
			} else {
				focusableElements[currentIndex + 1]?.focus();
			}
		}
	};

	const getFocusableElements = () => {
		if (!confirmElement) return [];

		const focusableSelectors = [
			'button:not([disabled])',
			'[href]',
			'input:not([disabled])',
			'select:not([disabled])',
			'textarea:not([disabled])',
			'[tabindex]:not([tabindex="-1"]):not([disabled])',
			'details',
			'summary'
		];

		const elements = confirmElement.querySelectorAll(focusableSelectors.join(', '));
		return Array.from(elements).filter((el) => {
			return el.offsetWidth > 0 && el.offsetHeight > 0 && !el.hasAttribute('hidden');
		});
	};

	const updateFocusableElements = () => {
		focusableElements = getFocusableElements();
		firstFocusableElement = focusableElements[0] || null;
		lastFocusableElement = focusableElements[focusableElements.length - 1] || null;
	};

	const handleConfirmOpen = async () => {
		// Store the currently focused element
		previousActiveElement = document.activeElement;

		// Wait for the DOM to update
		await tick();

		updateFocusableElements();

		// Focus the first focusable element
		if (firstFocusableElement) {
			firstFocusableElement.focus();
		}
	};

	const handleConfirmClose = () => {
		// Restore focus to the previously focused element
		if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
			previousActiveElement.focus();
		}
	};

	onMount(() => {
		return () => {
			window.removeEventListener('keydown', keyHandler);
			// Ensure body scrolling is restored when component is destroyed
			document.body.style.overflow = 'auto';
			// Restore focus if confirm was open when component was destroyed
			if (visible && previousActiveElement && typeof previousActiveElement.focus === 'function') {
				previousActiveElement.focus();
			}
		};
	});

	beforeNavigate((opts) => {
		if (!opts.from || !opts.to || !visible) {
			return;
		}
		const navigationIsNotOnSamePage = opts.from.url.pathname === opts.to.url.pathname;
		if (opts.type === 'popstate' && !navigationIsNotOnSamePage) {
			close();
		}
	});

	const close = () => {
		visible = false;
		window.removeEventListener('keydown', keyHandler);
		// Restore body scrolling when confirm prompt is closed
		document.body.style.overflow = 'auto';
		onCancel();
	};

	const confirm = () => {
		onConfirm();
		close();
	};
</script>

{#if visible}
	<div
		class="fixed top-0 left-0 w-full h-full bg-cta-blue dark:bg-gray-900 opacity-20 blur-xl transition-colors duration-200"
	/>
	<div
		class="fixed top-0 left-0 w-full h-full flex justify-center items-center backdrop-blur-sm z-20"
		role="dialog"
		aria-modal="true"
		aria-labelledby="confirm-title"
		aria-describedby="confirm-description"
	>
		<section
			bind:this={confirmElement}
			class="flex flex-col items-center w-1/3 bg-slate-100 dark:bg-gray-800 shadow-xl dark:shadow-gray-900/70 rounded-md transition-colors duration-200"
		>
			<div
				class="bg-cta-orange2 dark:bg-orange-600 text-white rounded-tl-md rounded-tr-md w-full transition-colors duration-200"
			>
				<p id="confirm-title" class="uppercase font-bold text-center py">Confirm action</p>
			</div>
			<h1
				class="uppercase font-bold text-center text-gray-500 dark:text-gray-300 text-4xl pt-10 transition-colors duration-200"
			>
				Are you sure?
			</h1>
			<p
				id="confirm-description"
				class="text-center text-gray-700 dark:text-gray-200 transition-colors duration-200"
			>
				{confirm_text}
			</p>
			<div class="flex pt-4 pb-8">
				<button
					class="mt-6 bg-grayblue-dark dark:bg-gray-600 w-40 py-2 mr-2 hover:bg-slate-300 dark:hover:bg-gray-500 text-white font-bold uppercase rounded-md transition-colors duration-200"
					on:click={close}
					aria-label="Cancel action"
				>
					no
				</button>
				<button
					class="mt-6 bg-cta-blue dark:bg-blue-600 w-56 py-2 hover:bg-blue-500 dark:hover:bg-blue-700 text-white font-bold uppercase rounded-md transition-colors duration-200"
					on:click={confirm}
					aria-label="Confirm action"
				>
					Yes
				</button>
			</div>
		</section>
	</div>
{/if}
