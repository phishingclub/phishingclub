<script>
	import { page } from '$app/stores';
	import { menu, mobileTopMenu } from '$lib/consts/navigation';
	import MenuLink from './MenuLink.svelte';
	import { shouldHideMenuItem } from '$lib/utils/common';

	export let visible = false;
	export let username = '';
	export let onClickLogout;
</script>

{#if visible}
	<div class="fixed top-0 left-0 w-full h-full bg-pc-darkblue z-40 overflow-y-auto pb-4">
		<div class="flex justify-between h-16">
			<img class="w-40 sm:w-40 md:w-42 lg:w-56 ml-4" src="/logo-white.svg" alt="logo" />
			<button class="mr-4 w-14" on:click={() => (visible = !visible)}>
				<img class="w-3/4" src="/mob-menu-close.svg" alt="close mobile menu" />
			</button>
		</div>
		<div>
			<div class="flex flex-col px-4 py-4 rounded-b-xl">
				<div class="flex py-6 border-b-2 border-white mb-4">
					<!-- <div class="bg-slate-50 w-16 h-16 rounded-full" /> -->
					<div>
						<h1 class="font-bold text-3xl ml-6 text-white">{username ?? ''}</h1>
						<button
							on:click={onClickLogout}
							class="bg-cta-blue hover:bg-pc-lightblue uppercase font-bold ml-6 mt-2 py text-white rounded-md"
						>
							<p class="py px-8">Log Out</p>
						</button>
					</div>
				</div>
				<div class="flex flex-col text-white">
					{#each mobileTopMenu as link}
						<a
							class="pl-5 py-2 hover:bg-cta-blue hover:text-white rounded-md"
							class:bg-gray-600={$page.url.pathname === link.route}
							class:hidden={shouldHideMenuItem(link.route)}
							on:click={() => (visible = !visible)}
							target={link.external ? '_blank' : '_self'}
							href={link.route}>{link.label}</a
						>
					{/each}
				</div>
			</div>
		</div>
		<div>
			<div class="flex flex-col bg-cta-blue px-4 pt-4">
				{#each menu as link}
					{#if link.type === 'submenu'}
						<div class="text-white font-semibold text-xl">{link.label}</div>
						{#each link.items as item, i (i)}
							<MenuLink href={item.route} on:click={() => (visible = !visible)}>
								{#if i === 0}
									Overview
								{:else if item.singleLabel}
									{item.singleLabel}
								{:else}
									{item.label}
								{/if}
							</MenuLink>
						{/each}
					{:else}
						<MenuLink href={link.route}>{link.label}</MenuLink>
					{/if}
				{/each}
			</div>
		</div>
	</div>
{/if}
