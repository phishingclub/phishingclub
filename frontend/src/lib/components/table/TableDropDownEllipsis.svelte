<script>
	import { onMount, onDestroy } from 'svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';

	let isMenuVisible = false;
	let menuX = 0;
	let menuY = 0;
	let menuRef = null;
	let buttonRef = null;

	// generate unique ID for this dropdown instance
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
			const menuHeight = 128; // max-h-32 in pixels
			const buffer = 20; // extra space to ensure some padding from viewport edges

			let clickViewportY, pageX, pageY;

			// Handle both mouse and keyboard events
			if (e.clientY !== undefined && e.pageX !== undefined) {
				// Mouse event
				clickViewportY = e.clientY;
				pageX = e.pageX;
				pageY = e.pageY;
			} else {
				// Keyboard event - use button position
				const buttonRect = buttonRef.getBoundingClientRect();
				clickViewportY = buttonRect.top;
				pageX = buttonRect.left + window.scrollX;
				pageY = buttonRect.top + window.scrollY;
			}

			// is the room enough to show the box
			const shouldShowAbove = viewportHeight - clickViewportY < menuHeight + buffer;

			// find position
			menuX = pageX - 192;
			menuY = shouldShowAbove
				? pageY - menuHeight // Position above click
				: pageY; // Position below click

			menuRef.style = `left: ${menuX}px; top: ${menuY}px`;
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
		// Clear active dropdown if this one was active
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
		class="py-2 px-2"
		on:click|stopPropagation|preventDefault={toggle}
		on:keydown={handleKeydown}
	>
		<svg width="3.335557" height="16.465519" viewBox="0 0 0.88253281 4.3565019">
			<g transform="translate(-892.25669,88.863024)">
				<g transform="matrix(0,1.0139418,-1.0139418,0,802.48114,-807.2715)">
					<circle class="fill-cta-blue" cx="708.99603" cy="-88.976357" r="0.40846577" />
					<circle class="fill-cta-blue" cx="710.67859" cy="-88.976357" r="0.40846577" />
					<circle class="fill-cta-blue" cx="712.36115" cy="-88.976357" r="0.40846577" />
				</g>
			</g>
		</svg>
	</button>

	<div
		bind:this={menuRef}
		class="absolute bg-white drop-shadow-md z-20 max-h-32 w-48 rounded-md overflow-y-scroll {scrollBarClassesVertical}"
		class:hidden={!isMenuVisible}
	>
		<ul class="flex flex-col text-left">
			<slot />
		</ul>
	</div>
</div>
