<script>
	import { onMount, tick } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';

	export let headerText = '';
	export let description = '';
	export let visible = false;
	export let isSubmitting = false;
	export let onClose = () => {};
	export let bindTo = null;
	export let resetTabFocus = () => {};
	export let noAutoFocus = false;

	let modalElement;
	let previousActiveElement;
	let focusableElements = [];
	let firstFocusableElement;
	let lastFocusableElement;

	let modalInitialized = false;
	let wasVisible = false;

	$: {
		// only handle focus when modal visibility actually changes and not during submission
		if (visible && !modalInitialized) {
			window.addEventListener('keydown', keyHandler);
			// Prevent body scrolling when modal is open
			document.body.style.overflow = 'hidden';
			if (!isSubmitting) {
				handleModalOpen();
			}
			modalInitialized = true;
			wasVisible = true;
		} else if (!visible && modalInitialized) {
			window.removeEventListener('keydown', keyHandler);
			// Restore body scrolling when modal is closed
			document.body.style.overflow = 'auto';
			handleModalClose();
			modalInitialized = false;
			wasVisible = false;
		}
	}

	const keyHandler = (e) => {
		if (e.key === 'Escape') {
			// don't close modal during submission
			if (!isSubmitting) {
				close();
			}
		} else if (e.key === 'Tab') {
			// Check if the focused element is a dropdown option button
			const focusedElement = document.activeElement;
			const isDropdownOption =
				focusedElement?.closest('[role="listbox"]') && focusedElement?.role === 'option';

			if (isDropdownOption) {
				// Don't intercept tab from dropdown options - let them handle it first
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

		// Always prevent default tab behavior to keep focus within modal
		e.preventDefault();

		let currentIndex = focusableElements.indexOf(currentlyFocused);

		// If current element is not found (-1), try to find a related element
		if (currentIndex === -1) {
			// Check if the current element is inside a TextFieldSelect or similar component
			const parentComponent = currentlyFocused?.closest('.textfield-select-container');
			if (parentComponent) {
				// Look for the input element within the same component
				const inputInComponent = parentComponent.querySelector('input');
				if (inputInComponent) {
					const inputIndex = focusableElements.indexOf(inputInComponent);
					if (inputIndex !== -1) {
						// Use the input's position for navigation and continue tab flow
						currentIndex = inputIndex;
					}
				}
			}
		}

		// If we still can't find the element, handle it gracefully
		if (currentIndex === -1) {
			// Check if the current element is inside a TextFieldSelect or similar component
			const parentComponent = currentlyFocused?.closest('.textfield-select-container');
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
			// If we can't find the element or related element, try to be smarter
			// Check if we should go forward or backward based on the shift key
			if (e.shiftKey) {
				// Shift+Tab: go to last element
				lastFocusableElement?.focus();
			} else {
				// Tab: try to find the first input in the form, or fallback to first element
				const firstInput = focusableElements.find((el) => el.tagName === 'INPUT');
				if (firstInput) {
					firstInput.focus();
				} else {
					firstFocusableElement?.focus();
				}
			}
			return;
		}

		// Now handle normal tab navigation
		if (e.shiftKey) {
			// Shift + Tab - go to previous element
			if (currentIndex <= 0) {
				// If at first element, go to last
				lastFocusableElement?.focus();
			} else {
				// Go to previous element
				focusableElements[currentIndex - 1]?.focus();
			}
		} else {
			// Tab - go to next element
			if (currentIndex >= focusableElements.length - 1) {
				// If at last element, go to first
				firstFocusableElement?.focus();
			} else {
				// Go to next element
				focusableElements[currentIndex + 1]?.focus();
			}
		}
	};

	const getFocusableElements = () => {
		if (!modalElement) return [];

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

		const elements = modalElement.querySelectorAll(focusableSelectors.join(', '));
		return Array.from(elements).filter((el) => {
			// exclude dropdown option buttons from TextFieldSelect components
			if (el.role === 'option' && el.closest('[role="listbox"]')) {
				return false;
			}

			// check if element is truly visible (has dimensions and not hidden)
			if (el.offsetWidth === 0 && el.offsetHeight === 0) {
				return false;
			}

			// check for hidden attribute
			if (el.hasAttribute('hidden')) {
				return false;
			}

			// check computed styles for visibility
			const style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') {
				return false;
			}

			// check if any parent container is hidden (for multi-step forms)
			let parent = el.parentElement;
			while (parent && parent !== modalElement) {
				const parentStyle = window.getComputedStyle(parent);
				if (parentStyle.display === 'none' || parentStyle.visibility === 'hidden') {
					return false;
				}
				parent = parent.parentElement;
			}

			return true;
		});
	};

	const updateFocusableElements = () => {
		focusableElements = getFocusableElements();

		// Reorder elements: form controls first, then navigation buttons, then close button
		const formElements = [];
		const navigationButtons = [];
		const closeButton = modalElement?.querySelector('[data-close-button]');

		focusableElements.forEach((el) => {
			if (el === closeButton) {
				// close button goes last
				return;
			} else if (
				el instanceof HTMLButtonElement &&
				(el.textContent?.trim() === 'Next' || el.textContent?.trim() === 'Previous')
			) {
				navigationButtons.push(el);
			} else {
				// all other elements (form fields, form control buttons, etc.) go first
				formElements.push(el);
			}
		});

		// Rebuild focusable elements in desired order: form controls, then navigation, then close
		focusableElements = [...formElements, ...navigationButtons];
		if (closeButton) {
			focusableElements.push(closeButton);
		}

		firstFocusableElement = focusableElements[0] || null;
		lastFocusableElement = focusableElements[focusableElements.length - 1] || null;
	};

	const handleModalOpen = async () => {
		// don't manage focus during form submission
		if (isSubmitting) {
			return;
		}

		// Store the currently focused element
		previousActiveElement = document.activeElement;

		// Wait for the DOM to update
		await tick();

		updateFocusableElements();

		// Focus the first focusable element (excluding close button)
		if (!noAutoFocus && firstFocusableElement) {
			firstFocusableElement.focus();
		}
	};

	const handleModalClose = () => {
		// Restore focus to the previously focused element
		if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
			previousActiveElement.focus();
		}
	};

	// Exposed function to reset tab focus when modal content changes
	const handleResetTabFocus = async () => {
		// Check if a TextFieldSelect is currently selecting an option
		const isTextFieldSelectActive = modalElement?.querySelector('[data-selecting="true"]');

		await tick();
		updateFocusableElements();
	};

	// Call resetTabFocus when it changes
	$: if (resetTabFocus) {
		resetTabFocus = handleResetTabFocus;
	}

	onMount(() => {
		return () => {
			window.removeEventListener('keydown', keyHandler);
			// Ensure body scrolling is restored when component is destroyed
			document.body.style.overflow = 'auto';
			// Restore focus if modal was open when component was destroyed
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
		// prevent closing during submission
		if (isSubmitting) {
			return;
		}
		visible = false;
		window.removeEventListener('keydown', keyHandler);
		// Restore body scrolling when modal is closed
		document.body.style.overflow = 'auto';
		onClose();
	};
</script>

{#if visible}
	<div bind:this={bindTo}>
		<div class="fixed top-0 left-0 w-full h-full opacity-[0.5]" />
		<div
			class="fixed top-0 left-0 w-full h-full flex justify-center items-center backdrop-blur-sm z-20"
			role="dialog"
			aria-modal="true"
			aria-labelledby="modal-title"
			aria-describedby={description ? 'modal-description' : undefined}
		>
			<section
				bind:this={modalElement}
				class="shadow-xl dark:shadow-gray-900/70 w-auto ml-20 mr-8 max-h-[90vh] bg-white dark:bg-gray-800 opacity-100 rounded-md flex flex-col transition-colors duration-200"
			>
				<div
					class:opacity-20={isSubmitting}
					class="bg-cta-blue dark:bg-blue-800 text-white rounded-t-md py-4 px-8 flex justify-between flex-shrink-0 transition-colors duration-200"
				>
					<div class="flex-1">
						<h1 id="modal-title" class="uppercase mr-8 font-semibold text-2xl">{headerText}</h1>
						{#if description}
							<p id="modal-description" class="mt-2 text-sm opacity-90">{description}</p>
						{/if}
					</div>
					<button
						class="w-4 hover:scale-110 flex-shrink-0"
						on:click={close}
						disabled={isSubmitting}
						data-close-button
						aria-label="Close modal"
					>
						<img class="w-full" src="/close-white.svg" alt="" />
					</button>
				</div>
				<div class="px-8 overflow-y-auto overflow-x-visible {scrollBarClassesVertical}">
					<slot />
				</div>
			</section>
		</div>
	</div>
{/if}
