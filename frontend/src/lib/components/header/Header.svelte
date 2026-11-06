<script>
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import Logo from './Logo.svelte';
	import ThemeToggle from '../ThemeToggle.svelte';
	import CompanyBanner from './CompanyBanner.svelte';

	const appState = AppStateService.instance;

	export let isProfileMenuVisible = false;
	export let isMobileMenuVisible = false;

	let isUpdateAvailable = false;
	let isInstalled = false;
	let context = {
		current: '',
		companyName: ''
	};
	let username = '';

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
		// create consistent hash
		const hash = username.split('').reduce((acc, char) => {
			return char.charCodeAt(0) + ((acc << 5) - acc);
		}, 0);

		// generate base colors
		const hue = Math.abs(hash) % 360;
		const colors = {
			primary: `hsl(${hue}, 70%, 50%)`,
			secondary: `hsl(${(hue + 120) % 360}, 70%, 50%)`,
			accent: `hsl(${(hue + 240) % 360}, 70%, 50%)`
		};

		// generate pattern parameters
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

<div class="sticky top-0 z-20">
	<CompanyBanner />
	<div
		class="header-container col-span-12 h-16 bg-pc-darkblue dark:bg-gray-800 border-b border-pc-darkblue/20 dark:border-highlight-blue/30 flex justify-between items-center"
	>
		<Logo />
		{#if isInstalled}
			<div class="hidden lg:flex flex-row items-center px-8 h-full justify-self-end">
				{#if isUpdateAvailable}
					<a
						class="flex items-center gap-2 text-sm font-semibold text-white bg-cta-blue hover:bg-active-blue dark:bg-active-blue dark:hover:bg-cta-blue rounded-md px-4 py-2 transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-highlight-blue focus-visible:ring-offset-2 dark:focus-visible:ring-offset-gray-800 fixed bottom-4 right-4 z-30 shadow-lg shadow-black/30"
						href={'/settings/update'}
					>
						<svg
							class="w-4 h-4"
							fill="none"
							stroke="currentColor"
							stroke-width="2"
							viewBox="0 0 24 24"
							aria-hidden="true"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3"
							/>
						</svg>
						<span>Update available</span>
					</a>
				{/if}
				<div class="relative ml-10 flex items-center gap-4">
					<ThemeToggle />
					<button
						id="toggle-profile-menu"
						type="button"
						class="group flex items-center rounded-full pc-focus-dark"
						aria-label="Account menu"
						aria-haspopup="menu"
						aria-expanded={isProfileMenuVisible}
						on:click={() => (isProfileMenuVisible = !isProfileMenuVisible)}
					>
						<!-- main circle with initials -->
						<div
							class="w-10 h-10 rounded-full bg-cta-blue hover:bg-cta-blue/80 dark:bg-active-blue dark:hover:bg-active-blue/80 dark:border dark:border-highlight-blue/40 flex items-center justify-center text-white font-medium relative transition-colors duration-200"
						>
							{initials}

							<div class="absolute -bottom-1 -right-1 w-5 h-5"></div>
						</div>

						<!-- dropdown indicator -->
						<svg
							class="w-4 h-4 ml-2 text-gray-300 dark:text-highlight-blue transition-transform duration-200 group-hover:text-white {isProfileMenuVisible
								? 'rotate-180'
								: ''}"
							aria-hidden="true"
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

			<div class="flex lg:hidden items-center mr-2 sm:mr-4 gap-2 sm:gap-4 shrink-0">
				<ThemeToggle />

				<button
					type="button"
					class="flex items-center justify-center w-11 h-11 rounded-lg hover:bg-white/10 dark:hover:bg-white/10 transition-colors duration-200 pc-focus-dark"
					aria-label="Open menu"
					aria-haspopup="dialog"
					aria-expanded={isMobileMenuVisible}
					on:click={() => (isMobileMenuVisible = !isMobileMenuVisible)}
				>
					<img class="w-6 h-6" src="/mob-menu-button.svg" alt="" />
				</button>
			</div>
		{/if}
	</div>
</div>

<style>
	/* prevent any hover effects on the header */
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
