<script>
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import Logo from './Logo.svelte';
	import ThemeToggle from '../ThemeToggle.svelte';

	const appState = AppStateService.instance;

	export let isProfileMenuVisible = false;
	export let isMobileMenuVisible = false;
	export let toggleChangeCompanyModal;

	let isUpdateAvailable = false;
	let isInstalled = false;
	let context = {
		current: '',
		companyName: ''
	};
	let username = '';

	/*
	let updateURL = 'https://user.phishing.club/downloads';
	if (import.meta.env.DEV) {
		updateURL = 'https://localhost:8009/downloads';
	}
	*/

	onMount(() => {
		const unsub = appState.subscribe((s) => {
			context = {
				current: s.context.current,
				companyName: s.context.companyName
			};
			isInstalled = s.installStatus === AppStateService.INSTALL.INSTALLED;
			const u = appState?.getUser();
			if (u.username) {
				username = u.username;
			}
			isUpdateAvailable = s.isUpdateAvailable;
		});
		return () => {
			unsub();
		};
	});

	// check if there is a context in local storage
	if (!context.companyName) {
		try {
			const ctxString = localStorage.getItem('context');
			const ctx = JSON.parse(ctxString);
			appState.setCompanyContext(ctx.id, ctx.name);
		} catch (e) {
			// do nothing failure to parse is expected if there is nothing
		}
	}

	function getInitials(username) {
		return username
			.split(' ')
			.map((word) => word.charAt(0))
			.join('')
			.toUpperCase()
			.slice(0, 2);
	}

	function profilePattern(username) {
		// Create consistent hash
		const hash = username.split('').reduce((acc, char) => {
			return char.charCodeAt(0) + ((acc << 5) - acc);
		}, 0);

		// Generate base colors
		const hue = Math.abs(hash) % 360;
		const colors = {
			primary: `hsl(${hue}, 70%, 50%)`,
			secondary: `hsl(${(hue + 120) % 360}, 70%, 50%)`,
			accent: `hsl(${(hue + 240) % 360}, 70%, 50%)`
		};

		// Generate pattern parameters
		const params = {
			rotation: hash % 360,
			segments: 6 + (hash % 6),
			waves: 3 + (hash % 4),
			amplitude: 5 + (hash % 10)
		};

		return { colors, params };
	}

	function generatePath(params, radius = 25, centerX = 25, centerY = 25) {
		let path = '';
		const points = [];
		const steps = 100;

		for (let i = 0; i <= steps; i++) {
			const angle = (i / steps) * Math.PI * 2;
			const segment = (angle * params.segments) % (Math.PI * 2);
			const wave = Math.sin(angle * params.waves) * params.amplitude;
			const r = radius + wave;

			const x = centerX + r * Math.cos(angle + params.rotation * (Math.PI / 180));
			const y = centerY + r * Math.sin(angle + params.rotation * (Math.PI / 180));

			points.push({ x, y });
			path += i === 0 ? `M ${x} ${y}` : ` L ${x} ${y}`;
		}

		return { path, points };
	}

	$: pattern = profilePattern(username);
	$: initials = getInitials(username || 'U');
</script>

<div
	class="header-container sticky top-0 z-20 col-span-12 h-16 bg-pc-darkblue dark:bg-gray-800 border-b border-pc-darkblue/20 dark:border-gray-700 flex justify-between items-center"
>
	<Logo />
	{#if isInstalled}
		<div class="hidden lg:flex flex-row items-center px-8 h-full justify-self-end">
			{#if context.current === AppStateService.CONTEXT.COMPANY}
				<p class="text-slate-300 dark:text-gray-300 uppercase font-bold text-lg mr-4">
					{context.companyName}
				</p>
			{/if}
			<button
				class="rounded-md h-3/4 px-8 text-white bg-indigo-500 hover:bg-cta-blue dark:bg-indigo-600 dark:hover:bg-indigo-700 uppercase font-semibold mr-4 transition-colors duration-200"
				on:click={toggleChangeCompanyModal}
			>
				Change company
			</button>

			{#if isUpdateAvailable}
				<a
					class="flex items-center gap-2 mr-8 text-lg font-medium text-white bg-gradient-to-r from-indigo-500 to-purple-500 dark:from-indigo-600 dark:to-purple-600 rounded-md px-4 py-2 transition-all duration-300 transform hover:-translate-y-0.5 focus:outline-none focus:ring-2 focus:ring-indigo-400 focus:ring-offset-2 dark:focus:ring-offset-gray-800 active:scale-95 fixed bottom-4 right-2 shadow-md shadow-black dark:shadow-gray-900"
					href={'/settings/update'}
				>
					<span class="">✨</span>
					<span>Update Available</span>
				</a>
			{/if}
			<div class="relative ml-10 flex items-center gap-4">
				<ThemeToggle />
				<button
					id="toggle-profile-menu"
					class="group flex items-center"
					on:click={() => (isProfileMenuVisible = !isProfileMenuVisible)}
				>
					<!-- Main Circle with Initials -->
					<div
						class="w-10 h-10 rounded-full bg-cta-blue hover:bg-indigo-500 dark:bg-indigo-600 dark:hover:bg-indigo-700 flex items-center justify-center text-white font-medium relative transition-colors duration-200"
					>
						{initials}

						<div class="absolute -bottom-1 -right-1 w-5 h-5"></div>
					</div>

					<!-- Dropdown Indicator -->
					<svg
						class="w-4 h-4 ml-2 text-gray-300 dark:text-gray-400 transition-transform duration-200 group-hover:text-white"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M19 9l-7 7-7-7"
						/>
					</svg>
				</button>
			</div>
		</div>

		<div class="flex lg:hidden items-center mr-4 gap-4">
			<div
				class="flex items-center justify-center w-10 h-10 rounded-lg hover:bg-white/10 transition-colors duration-200"
			>
				<ThemeToggle />
			</div>
			<button
				class="rounded-md px-3 py-2 text-white bg-indigo-500 hover:bg-cta-blue dark:bg-indigo-600 dark:hover:bg-indigo-700 uppercase font-semibold text-xs transition-colors duration-200"
				on:click={toggleChangeCompanyModal}
			>
				Change company
			</button>
			<button
				class="flex items-center justify-center w-10 h-10 rounded-lg hover:bg-white/10 transition-colors duration-200"
				on:click={() => (isMobileMenuVisible = !isMobileMenuVisible)}
			>
				<img class="w-6 h-6" src="/mob-menu-button.svg" alt="toggle mobile menu" />
			</button>
		</div>
	{/if}
</div>

<style>
	/* Prevent any hover effects on the header */
	.header-container {
		background-color: #0b2063 !important;
	}
	.header-container:hover {
		background-color: #0b2063 !important;
	}
	:global(.dark) .header-container {
		background-color: #1f2937 !important;
	}
	:global(.dark) .header-container:hover {
		background-color: #1f2937 !important;
	}
</style>
