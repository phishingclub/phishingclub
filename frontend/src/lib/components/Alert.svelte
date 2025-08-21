<script>
	import { onMount, tick } from 'svelte';
	import FormButton from './FormButton.svelte';
	import Input from './Input.svelte';
	import TextField from './TextField.svelte';
	import { beforeNavigate } from '$app/navigation';
	import FormError from './FormError.svelte';

	export let visible = true;
	export let verification = null;
	export let noCancel = false;
	export let headline = 'Confirm';
	export let cancel = 'Cancel';
	export let ok = 'Yes, proceed';
	export let onConfirm;

	let isLoading = false;
	let error = '';
	let alertElement;
	let previousActiveElement;
	let focusableElements = [];
	let firstFocusableElement;
	let lastFocusableElement;
	let alertInitialized = false;

	$: {
		if (visible && !alertInitialized) {
			window.addEventListener('keydown', keyHandler);
			// Prevent body scrolling when alert is open
			document.body.style.overflow = 'hidden';
			handleAlertOpen();
			alertInitialized = true;
		} else if (!visible && alertInitialized) {
			window.removeEventListener('keydown', keyHandler);
			// Restore body scrolling when alert is closed
			document.body.style.overflow = 'auto';
			handleAlertClose();
			alertInitialized = false;
		}
	}

	const keyHandler = (e) => {
		if (e.key === 'Escape') {
			close();
		} else if (e.key === 'Tab') {
			// Check if the focused element is an input field or within a complex component
			const focusedElement = document.activeElement;
			const isInputField =
				focusedElement?.tagName === 'INPUT' || focusedElement?.tagName === 'TEXTAREA';
			const isWithinTextField = focusedElement?.closest('.relative');

			if (isInputField || isWithinTextField) {
				// Allow normal tab behavior within input fields and TextFields
				return;
			}

			handleTabKey(e);
		}
	};

	const handleTabKey = (e) => {
		// Store the current focused element before updating the list
		const currentlyFocused = document.activeElement;

		// Update focusable elements before handling tab to account for dynamic changes
		updateFocusableElements();

		if (focusableElements.length === 0) return;

		// Always prevent default tab behavior to keep focus within alert
		e.preventDefault();

		let currentIndex = focusableElements.indexOf(currentlyFocused);

		// If current element is not found (-1), try to find a related element
		if (currentIndex === -1) {
			// Check if the current element is inside a TextField or similar component
			const parentComponent = currentlyFocused?.closest('.relative');
			if (parentComponent) {
				// Look for the input element within the same component
				const inputInComponent = parentComponent.querySelector('input');
				if (inputInComponent) {
					const inputIndex = focusableElements.indexOf(inputInComponent);
					if (inputIndex !== -1) {
						// Use the input's position for navigation
						currentIndex = inputIndex;
					}
				}
			}
		}

		// If we still can't find the element, handle it gracefully
		if (currentIndex === -1) {
			if (e.shiftKey) {
				lastFocusableElement?.focus();
			} else {
				firstFocusableElement?.focus();
			}
			return;
		}

		// Now handle normal tab navigation
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
		if (!alertElement) return [];

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

		const elements = alertElement.querySelectorAll(focusableSelectors.join(', '));
		return Array.from(elements).filter((el) => {
			return el.offsetWidth > 0 && el.offsetHeight > 0 && !el.hasAttribute('hidden');
		});
	};

	const updateFocusableElements = () => {
		focusableElements = getFocusableElements();

		// Exclude the close button from being the first focusable element
		const closeButton = alertElement?.querySelector('[data-close-button]');
		if (closeButton && focusableElements.length > 1) {
			focusableElements = focusableElements.filter((el) => el !== closeButton);
			focusableElements.push(closeButton); // Add close button to the end
		}

		firstFocusableElement = focusableElements[0] || null;
		lastFocusableElement = focusableElements[focusableElements.length - 1] || null;
	};

	const handleAlertOpen = async () => {
		// Store the currently focused element
		previousActiveElement = document.activeElement;

		// Wait for the DOM to update
		await tick();

		updateFocusableElements();

		// Focus priority: 1) Input fields (for confirmation), 2) Cancel button, 3) Other elements
		const firstInput = focusableElements.find(
			(el) => el.tagName === 'INPUT' || el.tagName === 'TEXTAREA'
		);
		const cancelButton = focusableElements.find(
			(el) =>
				el.tagName === 'BUTTON' &&
				(el.textContent?.toLowerCase().includes('cancel') ||
					el.textContent?.toLowerCase().includes('no') ||
					el.type === 'reset')
		);

		if (firstInput) {
			firstInput.focus();
		} else if (cancelButton) {
			cancelButton.focus();
		} else if (firstFocusableElement) {
			firstFocusableElement.focus();
		}
	};

	const handleAlertClose = () => {
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
			// Restore focus if alert was open when component was destroyed
			if (visible && previousActiveElement && typeof previousActiveElement.focus === 'function') {
				previousActiveElement.focus();
			}
			error = '';
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

	const onClickConfirm = async (e) => {
		e.preventDefault();
		isLoading = true;
		try {
			const res = await onConfirm();
			if (!res?.success) {
				throw res?.error || 'An error occurred';
			}
			close();
		} catch (e) {
			if (/Type.+\ to\ delete/.test(e)) {
				/** @type {HTMLInputElement} */
				const ele = document.querySelector('#confirmDelete');
				ele.setCustomValidity(e);
				ele.reportValidity();
				return;
			}
			error = e;
		} finally {
			isLoading = false;
		}
	};

	const close = () => {
		visible = false;
		window.removeEventListener('keydown', keyHandler);
		// Restore body scrolling when alert is closed
		document.body.style.overflow = 'auto';
	};
</script>

{#if visible}
	<div class="fixed top-0 left-0 w-full h-full bg-cta-blue opacity-20 blur-xl" />
	<div
		class="fixed top-0 left-0 w-full h-full flex justify-center items-center backdrop-blur-sm z-20"
		role="dialog"
		aria-modal="true"
		aria-labelledby="alert-title"
		aria-describedby="alert-description"
	>
		<section
			bind:this={alertElement}
			class="shadow-xl w-[32rem] bg-white opacity-100 rounded-md flex flex-col"
		>
			<!-- Header -->
			<div
				class="bg-red-700 text-white rounded-t-md py-4 px-8 flex items-center justify-between flex-shrink-0"
			>
				<div class="flex items-center">
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-6 w-6 text-white mr-4"
						fill="none"
						viewBox="0 0 24 24"
						stroke="currentColor"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
						/>
					</svg>
					<h1 id="alert-title" class="uppercase mr-8 font-semibold text-2xl">{headline}</h1>
				</div>
				<button
					class="w-4 hover:scale-110"
					on:click={close}
					disabled={isLoading}
					data-close-button
					aria-label="Close alert"
				>
					<img class="w-full" src="/close-white.svg" alt="" />
				</button>
			</div>

			<!-- Content -->
			<div class="px-8 py-6">
				<div id="alert-description" class="text-gray-600">
					{#if $$slots.default}
						<slot />
					{:else}
						Are you sure you want to proceed with this action?
					{/if}
				</div>

				{#if verification}
					<div class="mt-4">
						<TextField>Enter '{verification}' to confirm the action</TextField>
					</div>
				{/if}

				{#if error}
					<div class="mt-4">
						<FormError message={error} />
					</div>
				{/if}
			</div>

			<!-- Footer -->
			<div
				class="py-4 row-span-2 col-start-1 col-span-3 border-t-2 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end px-8 bg-gray-50 rounded-b-md"
			>
				{#if !noCancel}
					<button
						type="reset"
						on:click={close}
						class="bg-slate-400 hover:bg-slate-300 text-sm mr-2 uppercase font-bold px-4 py-2 text-white rounded-md"
					>
						{cancel}
					</button>
				{/if}
				<FormButton isSubmitting={isLoading} on:click={onClickConfirm}>{ok}</FormButton>
			</div>
		</section>
	</div>
{/if}
