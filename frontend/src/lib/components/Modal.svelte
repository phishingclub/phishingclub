<script context="module">
	let modalCounter = 0;
</script>

<script>
	import { fade } from 'svelte/transition';
	import { onMount, tick } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';
	import { guideURL } from '$lib/consts/docs';

	export let headerText = '';
	export let description = '';
	export let visible = false;
	export let isSubmitting = false;
	export let onClose = () => {};
	export let bindTo = null;
	export let resetTabFocus = () => {};
	export let noAutoFocus = false;
	export let fullscreen = false;
	// a fixed width so the modal never grows sideways from its content
	// '' sizes to the content, 'md' 42rem, 'lg' 48rem, 'xl' 64rem
	export let width = '';
	// when set, a help icon next to the title links to the matching guide page
	export let docSlug = '';
	export let docAnchor = '';

	const widthClasses = {
		md: 'sm:max-w-2xl',
		lg: 'sm:max-w-3xl',
		xl: 'sm:max-w-5xl'
	};

	const modalID = `modal-${++modalCounter}`;
	let modalElement;
	let previousActiveElement;
	let focusableElements = [];
	let firstFocusableElement;
	let lastFocusableElement;

	let modalInitialized = false;
	let wasVisible = false;

	// a modal that fills the phone screen gets an opaque backdrop so no page rows show at its edges
	let sectionHeight = 0;
	let viewportHeight = 0;
	$: isTall = sectionHeight > 0 && viewportHeight > 0 && sectionHeight >= viewportHeight - 24;

	$: {
		// only handle focus when modal visibility actually changes and not during submission
		if (visible && !modalInitialized) {
			window.addEventListener('keydown', keyHandler);
			// prevent body scrolling when modal is open
			document.body.style.overflow = 'hidden';
			if (!isSubmitting) {
				handleModalOpen();
			}
			modalInitialized = true;
			wasVisible = true;
		} else if (!visible && modalInitialized) {
			window.removeEventListener('keydown', keyHandler);
			// restore body scrolling when modal is closed
			document.body.style.overflow = '';
			handleModalClose();
			modalInitialized = false;
			wasVisible = false;
		}
	}

	const keyHandler = (e) => {
		// a confirm alert opened above this modal handles its own keys
		if (document.querySelector('[data-alert-dialog]')) {
			return;
		}
		if (e.key === 'Escape') {
			// don't close modal during submission
			if (!isSubmitting) {
				close();
			}
		} else if (e.key === 'Tab') {
			// check if the focused element is a dropdown option button
			const focusedElement = document.activeElement;
			const isDropdownOption =
				focusedElement?.closest('[role="listbox"]') && focusedElement?.role === 'option';

			if (isDropdownOption) {
				// don't intercept tab from dropdown options - let them handle it first
				return;
			}

			handleTabKey(e);
		}
	};

	const handleTabKey = (e) => {
		// store the current focused element before updating the list
		const currentlyFocused = document.activeElement;

		// update focusable elements before handling tab to account for dynamic changes
		updateFocusableElements();

		if (focusableElements.length === 0) return;

		// always prevent default tab behavior to keep focus within modal
		e.preventDefault();

		let currentIndex = focusableElements.indexOf(currentlyFocused);

		// if current element is not found (-1), try to find a related element
		if (currentIndex === -1) {
			// check if the current element is inside a TextFieldSelect or similar component
			const parentComponent = currentlyFocused?.closest('.textfield-select-container');
			if (parentComponent) {
				// look for the input element within the same component
				const inputInComponent = parentComponent.querySelector('input');
				if (inputInComponent) {
					const inputIndex = focusableElements.indexOf(inputInComponent);
					if (inputIndex !== -1) {
						// use the input's position for navigation and continue tab flow
						currentIndex = inputIndex;
					}
				}
			}
		}

		// if we still can't find the element, handle it gracefully
		if (currentIndex === -1) {
			// check if the current element is inside a TextFieldSelect or similar component
			const parentComponent = currentlyFocused?.closest('.textfield-select-container');
			if (parentComponent) {
				// look for the input element within the same component
				const inputInComponent = parentComponent.querySelector('input');
				if (inputInComponent) {
					const inputIndex = focusableElements.indexOf(inputInComponent);
					if (inputIndex !== -1) {
						// use the input's position for navigation
						currentIndex = inputIndex;
					}
				}
			}
		}

		// if we still can't find the element, handle it gracefully
		if (currentIndex === -1) {
			// if we can't find the element or related element, try to be smarter
			// check if we should go forward or backward based on the shift key
			if (e.shiftKey) {
				// shift+Tab: go to last element
				lastFocusableElement?.focus();
			} else {
				// tab: try to find the first input in the form, or fallback to first element
				const firstInput = focusableElements.find((el) => el.tagName === 'INPUT');
				if (firstInput) {
					firstInput.focus();
				} else {
					firstFocusableElement?.focus();
				}
			}
			return;
		}

		// now handle normal tab navigation
		if (e.shiftKey) {
			// shift + Tab - go to previous element
			if (currentIndex <= 0) {
				// if at first element, go to last
				lastFocusableElement?.focus();
			} else {
				// go to previous element
				focusableElements[currentIndex - 1]?.focus();
			}
		} else {
			// tab - go to next element
			if (currentIndex >= focusableElements.length - 1) {
				// if at last element, go to first
				firstFocusableElement?.focus();
			} else {
				// go to next element
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

		// reorder elements: form controls first, then navigation buttons, then close button
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

		// rebuild focusable elements in desired order: form controls, then navigation, then close
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

		// store the currently focused element
		previousActiveElement = document.activeElement;

		// wait for the DOM to update
		await tick();

		updateFocusableElements();

		// focus the first field the user can change, skipping tooltip triggers,
		// readonly and disabled fields and options outside the tab order
		const autoFocusTarget =
			focusableElements.find(
				(el) =>
					!el.hasAttribute('data-tooltip-trigger') &&
					!el.readOnly &&
					!el.disabled &&
					el.getAttribute('aria-disabled') !== 'true' &&
					el.getAttribute('tabindex') !== '-1'
			) || firstFocusableElement;
		if (noAutoFocus) return;
		const closeButton = modalElement?.querySelector('[data-close-button]');
		// a view only modal has no fields, so a button would get the ring before any key press
		const hasFields = focusableElements.some((el) => el.matches('input, select, textarea, [contenteditable="true"]'));
		if (autoFocusTarget && autoFocusTarget !== closeButton && hasFields) {
			autoFocusTarget.focus();
		} else {
			// nothing to fill in, so focus the panel and keep the ring off the close button
			modalElement?.focus();
		}
	};

	const handleModalClose = () => {
		// restore focus to the previously focused element
		if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
			previousActiveElement.focus();
		}
	};

	// exposed function to reset tab focus when modal content changes
	const handleResetTabFocus = async () => {
		// check if a TextFieldSelect is currently selecting an option
		const isTextFieldSelectActive = modalElement?.querySelector('[data-selecting="true"]');

		await tick();
		updateFocusableElements();
	};

	// call resetTabFocus when it changes
	$: if (resetTabFocus) {
		resetTabFocus = handleResetTabFocus;
	}

	onMount(() => {
		return () => {
			window.removeEventListener('keydown', keyHandler);
			// ensure body scrolling is restored when component is destroyed
			document.body.style.overflow = '';
			// restore focus if modal was open when component was destroyed
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
		// restore body scrolling when modal is closed
		document.body.style.overflow = '';
		onClose();
	};
</script>

<svelte:window bind:innerHeight={viewportHeight} />

{#if visible}
	<div bind:this={bindTo}>
		<div
			transition:fade={{ duration: 150 }}
			class="fixed top-0 left-0 w-full h-full z-40 {isTall || fullscreen
				? 'bg-overlay sm:bg-overlay/[0.92]'
				: 'bg-overlay/[0.92]'}"
		/>
		<div
			class="fixed top-0 left-0 w-full h-full flex justify-center items-center z-50 {fullscreen
				? ''
				: 'p-2 sm:p-4 lg:p-6'}"
			role="dialog"
			aria-modal="true"
			aria-labelledby="{modalID}-title"
			aria-describedby={description ? `${modalID}-description` : undefined}
		>
			<section
				bind:this={modalElement}
				bind:offsetHeight={sectionHeight}
				tabindex="-1"
				class="focus:outline-none ring-1 ring-white/10 bg-white dark:bg-gray-800 opacity-100 rounded-md flex flex-col shadow-2xl transition duration-200
					{fullscreen
					? 'fixed inset-0 w-full h-full max-w-none max-h-none rounded-none'
					: widthClasses[width]
						? `w-full min-w-0 max-w-full ${widthClasses[width]} max-h-full`
						: 'w-full sm:w-auto min-w-0 sm:min-w-[24rem] max-w-full xl:max-w-[80rem] max-h-full'}"
			>
				<div
					class:opacity-20={isSubmitting}
					class="bg-cta-blue dark:bg-cta-blue text-white py-3 pl-4 pr-2 sm:py-4 sm:pl-8 sm:pr-4 flex items-start justify-between gap-3 flex-shrink-0 transition-colors duration-200
						{fullscreen ? '' : 'rounded-t-md'}"
				>
					<div class="flex-1 min-w-0 self-center">
						{#if docSlug}
							<div class="flex items-start gap-2">
								<h2
									id="{modalID}-title"
									class="uppercase font-semibold text-xl sm:text-2xl break-words min-w-0"
								>
									{headerText}
								</h2>
								<a
									class="mt-1.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-white/60 text-[10px] font-medium leading-none text-white/80 hover:border-white hover:text-white transition-colors duration-200 pc-focus-dark"
									href={guideURL(docSlug, docAnchor)}
									target="_blank"
									rel="noopener noreferrer"
									title="Open the documentation"
									aria-label="Open the documentation">?</a
								>
							</div>
						{:else}
							<h2
								id="{modalID}-title"
								class="uppercase font-semibold text-xl sm:text-2xl break-words"
							>
								{headerText}
							</h2>
						{/if}
						{#if description}
							<p id="{modalID}-description" class="mt-1 text-sm opacity-90 break-words">
								{description}
							</p>
						{/if}
					</div>
					<button
						type="button"
						class="inline-flex items-center justify-center w-10 h-10 flex-shrink-0 rounded-md hover:bg-white/15 transition-colors duration-200 pc-focus-dark disabled:opacity-50"
						on:click={close}
						disabled={isSubmitting}
						data-close-button
						aria-label="Close modal"
					>
						<img class="w-4 h-4" src="/close-white.svg" alt="" />
					</button>
				</div>
				<div
					class="modal-body px-4 sm:px-8 overflow-y-auto overflow-x-auto min-h-0 select-text {scrollBarClassesVertical} {fullscreen
						? 'flex-1'
						: ''}"
				>
					<slot />
				</div>
			</section>
		</div>
	</div>
{/if}
