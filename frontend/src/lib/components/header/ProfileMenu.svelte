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
</script>

{#if visible}
	<nav
		id="profile-menu"
		class="lg:flex flex-col h-fit lg:col-start-10 lg:col-span-3 row-start-1 xl:col-start-11 xl:col-span-2 2xl:col-start-11 2xl:col-span-2 sticky top-20 z-30"
	>
		<div
			class="flex flex-col bg-gradient-to-b from-cta-blue to-indigo-500 rounded-md"
			transition:fade={{ duration: 150 }}
		>
			{#each topMenu as item}
				<a
					class="pl-5 py-2 text-white last:rounded-md first:rounded-t-md"
					class:hover:shadow-md={$page.url.pathname !== item.route}
					class:hover:bg-highlight-blue={$page.url.pathname !== item.route}
					class:bg-active-blue={$page.url.pathname === item.route}
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
				class="bg-white uppercase font-bold hover:bg-pc-lightblue py-2 mx-4 my-4 rounded-md"
			>
				<p class="text-cta-blue py px-8">Log Out</p>
			</button>
		</div>
	</nav>
{/if}
