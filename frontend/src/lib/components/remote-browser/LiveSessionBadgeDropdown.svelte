<script>
	import { STATUS_DOTS } from '$lib/components/StatusBadge.svelte';
	import { onDestroy } from 'svelte';
	import { activeFormElement } from '$lib/store/activeFormElement';
	import { scrollBarClassesVertical } from '$lib/utils/scrollbar';

	export let victimConnected = false;

	let isMenuVisible = false;
	let menuX = 0;
	let menuY = 0;
	let menuRef = null;
	let buttonRef = null;

	const dropdownId = Symbol();

	const unsubscribe = activeFormElement.subscribe((activeId) => {
		isMenuVisible = activeId === dropdownId;
	});

	const toggle = (e) => {
		if (isMenuVisible) {
			activeFormElement.set(null);
		} else {
			document.addEventListener('click', handleClickWhenVisible);
			document.addEventListener('keydown', handleGlobalKeydown);
			activeFormElement.set(dropdownId);

			const viewportHeight = window.innerHeight;
			const viewportWidth = window.innerWidth;
			const buffer = 20;
			const minHeight = 64;
			const maxHeight = 400;
			const gap = 8;

			const buttonRect = buttonRef.getBoundingClientRect();

			const spaceAbove = buttonRect.top - buffer;
			const spaceBelow = viewportHeight - buttonRect.bottom - buffer;
			const shouldShowAbove = spaceBelow < minHeight && spaceAbove > spaceBelow;
			const availableSpace = shouldShowAbove ? spaceAbove : spaceBelow;
			const optimalHeight = Math.min(Math.max(availableSpace, minHeight), maxHeight);

			const menuWidth = 256;
			const spaceOnRight = viewportWidth - buttonRect.right - buffer;
			menuX = spaceOnRight >= menuWidth ? buttonRect.left : buttonRect.right - menuWidth;
			menuX = Math.max(buffer, Math.min(menuX, viewportWidth - menuWidth - buffer));

			if (shouldShowAbove) {
				menuRef.style.visibility = 'hidden';
				menuRef.style.display = 'block';
				const actualMenuHeight = menuRef.scrollHeight;
				menuRef.style.display = '';
				menuRef.style.visibility = '';
				menuY = buttonRect.top - actualMenuHeight - gap;
			} else {
				menuY = buttonRect.bottom + gap;
			}

			menuRef.style = `left: ${menuX}px; top: ${menuY}px; max-height: ${optimalHeight}px`;
		}
	};

	const handleClickWhenVisible = (event) => {
		if (isMenuVisible && menuRef && buttonRef) {
			activeFormElement.set(null);
			event.preventDefault();
			event.stopPropagation();
		}
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
		activeFormElement.update((current) => (current === dropdownId ? null : current));
	};

	onDestroy(_onDestroy);
</script>

<div>
	<button
		bind:this={buttonRef}
		class="pc-focus inline-flex items-center gap-1.5 whitespace-nowrap rounded-sm text-sm leading-5 text-slate-700 transition-colors duration-150 hover:text-cta-blue dark:text-gray-200 dark:hover:text-highlight-blue"
		on:click|stopPropagation|preventDefault={toggle}
		on:keydown={handleKeydown}
	>
		{#if victimConnected}
			<span class="h-2 w-2 shrink-0 rounded-full {STATUS_DOTS.success} motion-safe:animate-pulse"></span>
			Live
		{:else}
			<span class="h-2 w-2 shrink-0 rounded-full {STATUS_DOTS.warning}"></span>
			Active
		{/if}
		<svg class="w-3 h-3 opacity-60" viewBox="0 0 20 20" fill="currentColor">
			<path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
		</svg>
	</button>

	<div
		bind:this={menuRef}
		class="fixed bg-white dark:bg-gray-900/90 drop-shadow-md dark:shadow-gray-900/50 border dark:border-gray-700/60 z-50 w-64 rounded-md overflow-y-scroll transition-colors duration-200 {scrollBarClassesVertical}"
		class:hidden={!isMenuVisible}
	>
		<ul class="flex flex-col text-left">
			<slot />
		</ul>
	</div>
</div>
