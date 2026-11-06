<script>
	import { onMount, onDestroy, tick } from 'svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';

	export let label = 'Row actions';
	// when set, the trigger is a labelled button with this text instead of the ellipsis
	export let text = '';

	let isMenuVisible = false;
	let menuRef = null;
	let buttonRef = null;

	// generate unique id for this dropdown instance
	const dropdownId = Symbol();
	const menuId = `row-menu-${Math.random().toString(36).slice(2, 10)}`;

	// subscribe to active dropdown store
	const unsubscribe = activeFormElement.subscribe((activeId) => {
		const wasVisible = isMenuVisible;
		isMenuVisible = activeId === dropdownId;
		if (wasVisible && !isMenuVisible) {
			removeListeners();
		}
	});

	const menuItems = () =>
		menuRef
			? [...menuRef.querySelectorAll('button:not([disabled]), a[href]')].filter(
					(el) => el.offsetParent !== null
				)
			: [];

	const position = () => {
		// clientWidth and clientHeight exclude scrollbars and match the css layout
		const viewportHeight = document.documentElement.clientHeight;
		const viewportWidth = document.documentElement.clientWidth;
		const buffer = 8;
		const minHeight = 64;
		const maxHeight = 400;
		const gap = 4;

		// the menu lives in document.body and is placed with document coordinates
		const scrollX = window.scrollX || document.documentElement.scrollLeft || 0;
		const scrollY = window.scrollY || document.documentElement.scrollTop || 0;

		const buttonRect = buttonRef.getBoundingClientRect();

		menuRef.style.visibility = 'hidden';
		menuRef.style.display = 'block';
		menuRef.style.maxHeight = '';
		const menuWidth = menuRef.offsetWidth;
		const menuHeight = menuRef.scrollHeight;
		menuRef.style.display = '';
		menuRef.style.visibility = '';

		const spaceAbove = buttonRect.top - buffer;
		const spaceBelow = viewportHeight - buttonRect.bottom - buffer;
		const shouldShowAbove = spaceBelow < Math.min(menuHeight, minHeight * 2) && spaceAbove > spaceBelow;
		const availableSpace = shouldShowAbove ? spaceAbove : spaceBelow;
		const optimalHeight = Math.min(Math.max(availableSpace - gap, minHeight), maxHeight);

		// right edge of the menu lines up with the right edge of the trigger
		let menuX = buttonRect.right - menuWidth;
		menuX = Math.max(buffer, Math.min(menuX, viewportWidth - menuWidth - buffer));

		let menuY;
		if (shouldShowAbove) {
			menuY = buttonRect.top - Math.min(menuHeight, optimalHeight) - gap;
		} else {
			menuY = buttonRect.bottom + gap;
		}

		menuRef.style.left = `${menuX + scrollX}px`;
		menuRef.style.top = `${menuY + scrollY}px`;
		menuRef.style.maxHeight = `${optimalHeight}px`;
	};

	const close = (returnFocus = false) => {
		activeFormElement.update((current) => (current === dropdownId ? null : current));
		if (returnFocus && buttonRef) {
			buttonRef.focus();
		}
	};

	const removeListeners = () => {
		document.removeEventListener('click', handleClickWhenVisible);
		document.removeEventListener('keydown', handleGlobalKeydown);
		document.removeEventListener('scroll', handleScroll, true);
		window.removeEventListener('resize', handleResize);
	};

	const open = async (focusFirst = false) => {
		document.addEventListener('click', handleClickWhenVisible);
		document.addEventListener('keydown', handleGlobalKeydown);
		document.addEventListener('scroll', handleScroll, true);
		window.addEventListener('resize', handleResize);
		activeFormElement.set(dropdownId); // set this as active, closing others
		position();
		if (focusFirst) {
			await tick();
			menuItems()[0]?.focus();
		}
	};

	const toggle = (focusFirst = false) => {
		if (isMenuVisible) {
			close();
		} else {
			open(focusFirst);
		}
	};

	const handleClickWhenVisible = (event) => {
		if (isMenuVisible && menuRef && buttonRef) {
			close();
		}
		event.preventDefault();
		event.stopPropagation();
		document.removeEventListener('click', handleClickWhenVisible);
	};

	const handleGlobalKeydown = (event) => {
		if (event.key === 'Escape' && isMenuVisible) {
			close(true);
		}
	};

	// the menu follows its button while the page or a table scrolls, and closes
	// only once the button is out of view
	const handleScroll = (event) => {
		if (!isMenuVisible || !menuRef || !buttonRef) {
			return;
		}
		if (menuRef.contains(event.target)) {
			return;
		}
		const rect = buttonRef.getBoundingClientRect();
		const viewportHeight = document.documentElement.clientHeight;
		const viewportWidth = document.documentElement.clientWidth;
		if (
			rect.bottom < 0 ||
			rect.top > viewportHeight ||
			rect.right < 0 ||
			rect.left > viewportWidth
		) {
			close();
			return;
		}
		position();
	};

	const handleResize = () => {
		if (isMenuVisible) {
			position();
		}
	};

	const handleKeydown = (e) => {
		if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
			e.preventDefault();
			e.stopPropagation();
			if (e.key === 'ArrowDown' && isMenuVisible) {
				menuItems()[0]?.focus();
				return;
			}
			toggle(true);
		} else if (e.key === 'Escape' && isMenuVisible) {
			e.preventDefault();
			e.stopPropagation();
			close();
		}
	};

	const handleMenuKeydown = (e) => {
		const items = menuItems();
		if (!items.length) {
			return;
		}
		const index = items.indexOf(document.activeElement);
		let next = null;
		if (e.key === 'ArrowDown') {
			next = items[(index + 1) % items.length];
		} else if (e.key === 'ArrowUp') {
			next = items[(index - 1 + items.length) % items.length];
		} else if (e.key === 'Home') {
			next = items[0];
		} else if (e.key === 'End') {
			next = items[items.length - 1];
		} else if (e.key === 'Tab') {
			close();
			return;
		} else if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			close(true);
			return;
		}
		if (next) {
			e.preventDefault();
			next.focus();
		}
	};

	onMount(() => {
		// render the menu at the document root so table scroll boxes and the
		// sticky actions column do not clip or offset it
		document.body.appendChild(menuRef);
	});

	onDestroy(() => {
		removeListeners();
		unsubscribe();
		// clear active dropdown if this one was active
		activeFormElement.update((current) => (current === dropdownId ? null : current));
		if (menuRef && menuRef.parentNode) {
			menuRef.parentNode.removeChild(menuRef);
		}
	});
</script>

<div>
	<button
		bind:this={buttonRef}
		type="button"
		aria-label={text ? undefined : label}
		title={text ? undefined : label}
		aria-haspopup="menu"
		aria-expanded={isMenuVisible}
		aria-controls={menuId}
		class={text
			? `pc-focus inline-flex items-center gap-2 min-h-[2.625rem] px-3.5 py-2 border rounded-md text-sm font-medium border-gray-300 bg-white hover:bg-gray-100 text-slate-700 dark:bg-transparent dark:hover:bg-gray-700 dark:border-gray-500 dark:text-gray-100 transition-colors duration-200 ${
					isMenuVisible ? 'bg-gray-100 dark:bg-gray-700' : ''
				}`
			: `pc-focus w-10 h-10 mx-auto flex items-center justify-center rounded-md hover:bg-white/80 dark:hover:bg-highlight-blue/30 transition-colors duration-200 ${
					isMenuVisible
						? 'bg-white/80 dark:bg-highlight-blue/30 ring-1 ring-cta-blue/40 dark:ring-highlight-blue/50'
						: ''
				}`}
		on:click|stopPropagation|preventDefault={() => toggle(false)}
		on:keydown={handleKeydown}
	>
		{#if text}
			{text}
			<svg
				class="h-4 w-4 transition-transform duration-200"
				class:rotate-180={isMenuVisible}
				xmlns="http://www.w3.org/2000/svg"
				viewBox="0 0 20 20"
				fill="currentColor"
				aria-hidden="true"
			>
				<path
					fill-rule="evenodd"
					d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z"
					clip-rule="evenodd"
				/>
			</svg>
		{:else}
		<svg
			width="3.335557"
			height="16.465519"
			viewBox="0 0 0.88253281 4.3565019"
			aria-hidden="true"
		>
			<g transform="translate(-892.25669,88.863024)">
				<g transform="matrix(0,1.0139418,-1.0139418,0,802.48114,-807.2715)">
					<circle
						class="fill-cta-blue dark:fill-highlight-blue transition-colors duration-200"
						cx="708.99603"
						cy="-88.976357"
						r="0.40846577"
					/>
					<circle
						class="fill-cta-blue dark:fill-highlight-blue transition-colors duration-200"
						cx="710.67859"
						cy="-88.976357"
						r="0.40846577"
					/>
					<circle
						class="fill-cta-blue dark:fill-highlight-blue transition-colors duration-200"
						cx="712.36115"
						cy="-88.976357"
						r="0.40846577"
					/>
				</g>
			</g>
		</svg>
		{/if}
	</button>

	<!-- svelte-ignore a11y-no-static-element-interactions -->
	<div
		bind:this={menuRef}
		id={menuId}
		class="absolute z-[70] min-w-44 w-max max-w-64 py-1 bg-white dark:bg-gray-800 shadow-lg dark:shadow-black/40 border border-gray-200 dark:border-gray-600 rounded-md overflow-y-auto text-left {scrollBarClassesVertical}"
		class:hidden={!isMenuVisible}
		on:keydown={handleMenuKeydown}
	>
		<ul role="menu" aria-label={label} class="flex flex-col text-left">
			<slot />
		</ul>
	</div>
</div>
