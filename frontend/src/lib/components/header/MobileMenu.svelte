<script>
	import { page } from '$app/stores';
	import { menu, mobileTopMenu } from '$lib/consts/navigation';
	import MenuLink from './MenuLink.svelte';
	import { shouldHideMenuItem } from '$lib/utils/common';
	import ThemeToggle from '../ThemeToggle.svelte';

	export let visible = false;
	export let username = '';
	export let onClickLogout;
</script>

{#if visible}
	<!-- Overlay -->
	<button
		class="fixed inset-0 bg-black bg-opacity-50 z-40 cursor-default"
		on:click={() => (visible = false)}
		aria-label="Close mobile menu"
	></button>

	<!-- Mobile Menu -->
	<div
		class="mobile-menu-content fixed top-0 left-0 w-full h-full bg-pc-darkblue dark:bg-gray-900 z-50 overflow-y-auto shadow-xl transition-colors duration-200"
	>
		<!-- Header -->
		<div
			class="mobile-menu-header flex justify-between h-20 items-center bg-pc-darkblue dark:bg-gray-800 px-6"
		>
			<img class="w-40 h-auto" src="/logo-white.svg" alt="logo" />
			<div class="flex items-center gap-4">
				<div
					class="flex items-center justify-center w-12 h-12 rounded-lg hover:bg-white/10 dark:hover:bg-gray-600/30 transition-colors duration-200"
				>
					<ThemeToggle />
				</div>
				<button
					class="flex items-center justify-center w-12 h-12 rounded-lg hover:bg-white/10 dark:hover:bg-gray-600/30 transition-colors duration-200"
					on:click={() => (visible = false)}
				>
					<img class="w-6 h-6" src="/mob-menu-close.svg" alt="close mobile menu" />
				</button>
			</div>
		</div>

		<!-- User Section -->
		<div class="p-6 border-b border-white dark:border-gray-700">
			<h1 class="font-bold text-xl text-white dark:text-gray-100 mb-4">
				{username ?? ''}
			</h1>
			<button
				on:click={onClickLogout}
				class="bg-cta-blue dark:bg-indigo-600 dark:hover:bg-indigo-700 uppercase font-bold py-3 px-6 rounded-md transition-colors duration-200 text-sm text-white"
			>
				Log Out
			</button>
		</div>

		<!-- Top Menu -->
		<div class="p-4">
			<div
				class="bg-gradient-to-b from-cta-blue to-indigo-500 dark:from-gray-800 dark:to-gray-700 rounded-md"
			>
				{#each mobileTopMenu as link}
					<a
						class="block text-center py-4 text-white text-lg font-medium first:rounded-t-md last:rounded-b-md transition-colors duration-200"
						class:bg-active-blue={$page.url.pathname === link.route}
						class:dark:bg-gray-700={$page.url.pathname === link.route}
						class:shadow-md={$page.url.pathname === link.route}
						class:hidden={shouldHideMenuItem(link.route)}
						on:click={() => (visible = false)}
						target={link.external ? '_blank' : '_self'}
						href={link.route}
					>
						{link.label}
					</a>
				{/each}
			</div>
		</div>

		<!-- Main Menu -->
		<div class="p-4 pt-0">
			<div
				class="bg-gradient-to-b from-cta-blue to-indigo-500 dark:from-gray-800 dark:to-gray-700 rounded-md"
			>
				{#each menu as link}
					{#if link.type === 'submenu'}
						<div
							class="text-center py-4 text-white font-semibold text-lg border-b border-white/20 dark:border-gray-600"
						>
							{link.label}
						</div>
						{#each link.items as item, i (i)}
							<a
								class="block text-center py-3 text-white text-base transition-colors duration-200"
								class:last:rounded-b-md={i === link.items.length - 1}
								href={item.route}
								on:click={() => (visible = false)}
							>
								{#if i === 0}
									Overview
								{:else if item.singleLabel}
									{item.singleLabel}
								{:else}
									{item.label}
								{/if}
							</a>
						{/each}
					{:else}
						<a
							class="block text-center py-4 text-white text-lg font-medium transition-colors duration-200"
							href={link.route}
							on:click={() => (visible = false)}
						>
							{link.label}
						</a>
					{/if}
				{/each}
			</div>
		</div>
	</div>
{/if}

<style>
	/* Prevent any hover effects on the mobile menu header */
	:global(.mobile-menu-header) {
		background-color: #0b2063 !important;
	}
	:global(.mobile-menu-header:hover) {
		background-color: #0b2063 !important;
	}
	:global(.dark .mobile-menu-header) {
		background-color: #1f2937 !important;
	}
	:global(.dark .mobile-menu-header:hover) {
		background-color: #1f2937 !important;
	}

	/* Prevent any hover effects on the mobile menu content */
	.mobile-menu-content {
		background-color: #0b2063 !important;
	}
	.mobile-menu-content:hover {
		background-color: #0b2063 !important;
	}
	:global(.dark) .mobile-menu-content {
		background-color: #111827 !important;
	}
	:global(.dark) .mobile-menu-content:hover {
		background-color: #111827 !important;
	}
</style>
