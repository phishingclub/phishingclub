<script>
	import { page } from '$app/stores';
	import { fade } from 'svelte/transition';
	import { topMenu } from '$lib/consts/navigation';
	import { shouldHideMenuItem } from '$lib/utils/common';
	export let logout;
	export let visible = false;

	const handleClickOutsideNavigation = (event) => {
		const profileMenuElement = document.getElementById('profile-menu');
		const profileToggleElement = document.getElementById('toggle-profile-menu');
		const clickOutsideMenu = profileMenuElement && !profileMenuElement.contains(event.target);
		const clickOutsideToggleButton =
			profileToggleElement && !profileToggleElement.contains(event.target);
		if (clickOutsideMenu && clickOutsideToggleButton) {
			visible = false;
		}
	};

	$: {
		if (visible) {
			// add event listener to listen for a click outside the nav element
			document.addEventListener('click', handleClickOutsideNavigation);
		} else {
			// remove event listener
			document.removeEventListener('click', handleClickOutsideNavigation);
		}
	}

	// Custom transition that only fades in
	function fadeIn(node, { duration = 150 }) {
		return {
			duration,
			css: (t) => `opacity: ${t}`
		};
	}
</script>

{#if visible}
	<nav
		id="profile-menu"
		class="lg:flex flex-col h-fit lg:col-start-10 lg:col-span-3 row-start-1 xl:col-start-11 xl:col-span-2 2xl:col-start-11 2xl:col-span-2 sticky top-20 z-30"
	>
		<div
			class="flex flex-col bg-gradient-to-b from-cta-blue to-indigo-500 dark:from-gray-800 dark:to-gray-700 rounded-md transition-colors duration-200"
			in:fadeIn={{ duration: 150 }}
		>
			{#each topMenu as item}
				<a
					class="pl-5 py-2 text-white last:rounded-md first:rounded-t-md transition-colors duration-200"
					class:hover:shadow-md={$page.url.pathname !== item.route}
					class:hover:bg-highlight-blue={$page.url.pathname !== item.route}
					class:dark:hover:bg-gray-600={$page.url.pathname !== item.route}
					class:bg-active-blue={$page.url.pathname === item.route}
					class:dark:bg-gray-700={$page.url.pathname === item.route}
					class:shadow-md={$page.url.pathname === item.route}
					class:hidden={shouldHideMenuItem(item.route)}
					target={item.external ? '_blank' : '_self'}
					draggable="false"
					on:click={() => {
						visible = false;
					}}
					href={item.route}>{item.label}</a
				>
			{/each}
			<button
				on:click={logout}
				class="bg-white dark:bg-gray-800 uppercase font-bold hover:bg-pc-lightblue dark:hover:bg-gray-700 py-2 mx-4 my-4 rounded-md transition-colors duration-200"
			>
				<p class="text-cta-blue dark:text-gray-100 py px-8 transition-colors duration-200">
					Log Out
				</p>
			</button>
		</div>
	</nav>
{/if}
