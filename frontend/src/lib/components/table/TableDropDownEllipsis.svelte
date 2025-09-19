<script>
	import { onMount, onDestroy } from 'svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';

	let isMenuVisible = false;
	let menuX = 0;
	let menuY = 0;
	let menuRef = null;
	let buttonRef = null;

	// generate unique id for this dropdown instance
	const dropdownId = Symbol();

	// subscribe to active dropdown store
	const unsubscribe = activeFormElement.subscribe((activeId) => {
		isMenuVisible = activeId === dropdownId;
	});

	const toggle = (e) => {
		if (isMenuVisible) {
			activeFormElement.set(null);
		} else {
			document.addEventListener('click', handleClickWhenVisible);
			document.addEventListener('keydown', handleGlobalKeydown);
			activeFormElement.set(dropdownId); // set this as active, closing others

			const viewportHeight = window.innerHeight;
			const buffer = 20; // extra space to ensure some padding from viewport edges
			const minHeight = 64; // minimum dropdown height
			const maxHeight = 400; // maximum dropdown height

			let clickViewportY, pageX, pageY;

			// handle both mouse and keyboard events
			if (e.clientY !== undefined && e.pageX !== undefined) {
				// mouse event
				clickViewportY = e.clientY;
				pageX = e.pageX;
				pageY = e.pageY;
			} else {
				// keyboard event - use button position
				const buttonRect = buttonRef.getBoundingClientRect();
				clickViewportY = buttonRect.top;
				pageX = buttonRect.left + window.scrollX;
				pageY = buttonRect.top + window.scrollY;
			}

			// calculate available space above and below
			const spaceAbove = clickViewportY - buffer;
			const spaceBelow = viewportHeight - clickViewportY - buffer;

			// choose position based on available space, with preference for below
			const shouldShowAbove = spaceBelow < minHeight && spaceAbove > spaceBelow;
			const availableSpace = shouldShowAbove ? spaceAbove : spaceBelow;

			// calculate optimal height within bounds
			const optimalHeight = Math.min(Math.max(availableSpace, minHeight), maxHeight);

			// find position
			const gap = 8; // small gap between menu and cursor/button
			menuX = pageX - 192;

			if (shouldShowAbove) {
				// calculate actual menu height by temporarily showing it
				menuRef.style.visibility = 'hidden';
				menuRef.style.display = 'block';
				const actualMenuHeight = menuRef.scrollHeight;
				menuRef.style.display = '';
				menuRef.style.visibility = '';

				// position above by moving up by the actual menu height
				menuY = pageY - actualMenuHeight - gap;
			} else {
				// for below positioning, use original click/button position
				menuY = pageY + gap;
			}

			menuRef.style = `left: ${menuX}px; top: ${menuY}px; max-height: ${optimalHeight}px`;
		}
	};

	const handleClickWhenVisible = (event) => {
		if (isMenuVisible && menuRef && buttonRef) {
			activeFormElement.set(null);
		}
		event.preventDefault();
		event.stopPropagation();
		document.removeEventListener('click', handleClickWhenVisible);
	};

	const handleGlobalKeydown = (event) => {
		if (event.key === 'Escape' && isMenuVisible) {
			activeFormElement.set(null);
			document.removeEventListener('keydown', handleGlobalKeydown);
		}
	};

	const handleKeydown = (e) => {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			e.stopPropagation();
			toggle(e);
		} else if (e.key === 'Escape' && isMenuVisible) {
			e.preventDefault();
			e.stopPropagation();
			activeFormElement.set(null);
		}
	};

	const _onDestroy = () => {
		document.removeEventListener('click', handleClickWhenVisible);
		document.removeEventListener('keydown', handleGlobalKeydown);
		unsubscribe();
		// clear active dropdown if this one was active
		activeFormElement.update((current) => (current === dropdownId ? null : current));
	};

	onDestroy(_onDestroy);

	onMount(() => {
		return () => {
			_onDestroy();
		};
	});
</script>

<div class="">
	<button
		bind:this={buttonRef}
		class="w-full h-full py-3 flex items-center justify-center"
		on:click|stopPropagation|preventDefault={toggle}
		on:keydown={handleKeydown}
	>
		<svg width="3.335557" height="16.465519" viewBox="0 0 0.88253281 4.3565019">
			<g transform="translate(-892.25669,88.863024)">
				<g transform="matrix(0,1.0139418,-1.0139418,0,802.48114,-807.2715)">
					<circle
						class="fill-cta-blue dark:fill-blue-500 transition-colors duration-200"
						cx="708.99603"
						cy="-88.976357"
						r="0.40846577"
					/>
					<circle
						class="fill-cta-blue dark:fill-blue-500 transition-colors duration-200"
						cx="710.67859"
						cy="-88.976357"
						r="0.40846577"
					/>
					<circle
						class="fill-cta-blue dark:fill-blue-500 transition-colors duration-200"
						cx="712.36115"
						cy="-88.976357"
						r="0.40846577"
					/>
				</g>
			</g>
		</svg>
	</button>

	<div
		bind:this={menuRef}
		class="absolute bg-white dark:bg-gray-800 drop-shadow-md dark:shadow-gray-900/50 border dark:border-gray-600 z-20 w-48 rounded-md overflow-y-scroll transition-colors duration-200 {scrollBarClassesVertical}"
		class:hidden={!isMenuVisible}
	>
		<ul class="flex flex-col text-left">
			<slot />
		</ul>
	</div>
</div>
