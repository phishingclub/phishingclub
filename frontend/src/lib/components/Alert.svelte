<script context="module">
	let alertCounter = 0;
</script>

<script>
	import { fade } from 'svelte/transition';
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
	// greys and blocks the confirm button while the action cannot proceed, for example an
	// install whose bundle references packages that no longer exist on the registry.
	export let okDisabled = false;
	export let onConfirm;
	// 'destructive' (red, warning icon) suits remove and uninstall, 'affirmative'
	// (blue, no icon) suits positive actions like install and update.
	export let tone = 'destructive';
	$: isDestructive = tone !== 'affirmative';

	const alertID = `alert-${++alertCounter}`;
	let isLoading = false;
	let error = '';
	// typed value when verification is required before confirming
	let verificationText = '';
	let alertElement;
	let previousActiveElement;
	let focusableElements = [];
	let firstFocusableElement;
	let lastFocusableElement;
	let alertInitialized = false;

	$: {
		if (visible && !alertInitialized) {
			window.addEventListener('keydown', keyHandler);
			// prevent body scrolling when alert is open
			document.body.style.overflow = 'hidden';
			handleAlertOpen();
			alertInitialized = true;
		} else if (!visible && alertInitialized) {
			window.removeEventListener('keydown', keyHandler);
			// restore body scrolling when alert is closed
			document.body.style.overflow = '';
			handleAlertClose();
			alertInitialized = false;
		}
	}

	const keyHandler = (e) => {
		if (e.key === 'Escape') {
			close();
		} else if (e.key === 'Tab') {
			const focusedElement = document.activeElement;
			const isDropdownOption =
				focusedElement?.closest('[role="listbox"]') && focusedElement?.role === 'option';
			if (isDropdownOption) {
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
		// reset the typed confirmation so it is empty next time the alert opens
		verificationText = '';
		// Restore focus to the previously focused element
		if (previousActiveElement && typeof previousActiveElement.focus === 'function') {
			previousActiveElement.focus();
		}
	};

	onMount(() => {
		return () => {
			window.removeEventListener('keydown', keyHandler);
			// Ensure body scrolling is restored when component is destroyed
			document.body.style.overflow = '';
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
		// require the user to type the verification word before proceeding
		if (verification) {
			const ele = /** @type {HTMLInputElement} */ (document.querySelector('#confirmVerification'));
			if (verificationText !== verification) {
				ele?.setCustomValidity(`Enter '${verification}' to confirm`);
				ele?.reportValidity();
				return;
			}
			ele?.setCustomValidity('');
		}
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
		document.body.style.overflow = '';
	};
</script>

{#if visible}
	<div
		transition:fade={{ duration: 150 }}
		class="fixed top-0 left-0 w-full h-full bg-overlay/[0.92] z-[55]"
	/>
	<div
		class="fixed top-0 left-0 w-full h-full flex justify-center items-center z-[60] p-3 sm:p-4"
		role="alertdialog"
		aria-modal="true"
		aria-labelledby="{alertID}-title"
		aria-describedby="{alertID}-description"
		data-alert-dialog
	>
		<section
			bind:this={alertElement}
			class="ring-1 ring-white/10 w-full max-w-lg max-h-full bg-white dark:bg-gray-800 opacity-100 rounded-md flex flex-col shadow-xl transition-colors duration-200"
		>
			<!-- Header -->
			<div
				class="{isDestructive
					? 'bg-red-700 dark:bg-red-700/90'
					: 'bg-cta-blue dark:bg-cta-blue'} text-white rounded-t-md py-3 px-4 sm:py-4 sm:px-8 flex items-center justify-between gap-3 flex-shrink-0 transition-colors duration-200"
			>
				<div class="flex items-center min-w-0">
					{#if isDestructive}
						<svg
							xmlns="http://www.w3.org/2000/svg"
							class="h-6 w-6 shrink-0 text-white mr-3 sm:mr-4"
							aria-hidden="true"
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
					{/if}
					<h2 id="{alertID}-title" class="uppercase font-semibold text-xl sm:text-2xl break-words min-w-0">
						{headline}
					</h2>
				</div>
				<button
					type="button"
					class="-mr-2 inline-flex items-center justify-center w-10 h-10 shrink-0 rounded-md hover:bg-white/15 transition-colors duration-200 pc-focus-dark disabled:opacity-50"
					on:click={close}
					disabled={isLoading}
					data-close-button
					aria-label="Close alert"
				>
					<img class="w-4 h-4" src="/close-white.svg" alt="" />
				</button>
			</div>

			<div class="px-4 py-5 sm:px-8 sm:py-6 overflow-y-auto min-h-0 select-text">
				<div
					id="{alertID}-description"
					class="text-gray-600 dark:text-gray-400 transition-colors duration-200"
				>
					{#if $$slots.default}
						<slot />
					{:else}
						Are you sure you want to proceed with this action?
					{/if}
				</div>

				{#if verification}
					<div class="mt-4">
						<TextField
							bind:value={verificationText}
							required
							id="confirmVerification"
							on:keydown={(e) => {
								const ele = /** @type {HTMLInputElement} */ (e.target);
								ele.setCustomValidity('');
							}}
						>
							Enter '{verification}' to confirm the action
						</TextField>
					</div>
				{/if}

				{#if error}
					<div class="mt-4">
						<FormError message={error} />
					</div>
				{/if}
			</div>

			<div
				class="py-4 border-t-2 border-gray-200 dark:border-gray-700/60 w-full flex flex-row flex-wrap justify-end items-center gap-2 px-4 sm:px-8 bg-gray-50 dark:bg-gray-800/40 rounded-b-md flex-shrink-0 transition-colors duration-200"
			>
				{#if !noCancel}
					<button
						type="reset"
						on:click={close}
						class="pc-focus min-w-[6rem] bg-transparent hover:bg-gray-100 border border-gray-300 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 text-sm uppercase font-bold px-4 py-2 rounded-md transition-colors duration-200"
					>
						{cancel}
					</button>
				{/if}
				<FormButton
					isSubmitting={isLoading}
					disabled={okDisabled}
					variant={isDestructive ? 'danger' : 'primary'}
					on:click={onClickConfirm}>{ok}</FormButton
				>
			</div>
		</section>
	</div>
{/if}
