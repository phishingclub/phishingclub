<script>
	import '../app.css';
	import { onMount, tick } from 'svelte';
	import { page } from '$app/stores';
	import { beforeNavigate, goto } from '$app/navigation';

	import { Session } from '$lib/service/session';
	import { AppStateService } from '$lib/service/appState';
	import { UserService } from '$lib/service/user';
	import Toast from '$lib/components/Toast.svelte';
	import { API } from '$lib/api/api.js';
	import ProfileMenu from '$lib/components/header/ProfileMenu.svelte';
	import Loader from '$lib/components/Loader.svelte';
	import MobileMenu from '$lib/components/header/MobileMenu.svelte';
	import RootLoader from '$lib/components/RootLoader.svelte';
	import ChangeCompanyModal from '$lib/components/modal/ChangeCompanyModal.svelte';
	import CommandPalette from '$lib/components/modal/CommandPalette.svelte';
	import DesktopMenu from '$lib/components/header/DesktopMenu.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import Header from '$lib/components/header/Header.svelte';
	import { setupTheme, setupOSThemeListener } from '$lib/theme.js';
	import { displayMode } from '$lib/store/displayMode';

	// services
	const session = Session.instance;
	const appState = AppStateService.instance;
	const api = API.instance;

	// debug / dev
	const isDevelopement = false;

	// local state
	let loginStatus = AppStateService.LOGIN.UNKNOWN;
	let installState = AppStateService.INSTALL.UNKNOWN;

	let isProfileMenuVisible = false;
	let isMobileMenuVisible = false;
	let isChangeCompanyModalVisible = false;
	let isCommandPaletteVisible = false;
	let isReady = false;

	// pin state for menu
	let isMenuPinned = false;

	// cookie helpers
	function getCookie(name) {
		const value = `; ${document.cookie}`;
		const parts = value.split(`; ${name}=`);
		if (parts.length === 2) return parts.pop().split(';').shift();
	}
	function setCookie(name, value, days = 365) {
		const expires = new Date(Date.now() + days * 864e5).toUTCString();
		document.cookie = `${name}=${value}; expires=${expires}; path=/`;
	}

	beforeNavigate((beforeNavigate) => {
		if (!beforeNavigate.to) {
			return;
		}
		const toInstall = beforeNavigate.to.route.id === '/install';
		if (appState.isLoggedIn() && !appState.isInstalled() && !toInstall) {
			console.warn('navigation away from install cancelled');
			beforeNavigate.cancel();
		}
	});

	onMount(() => {
		// initialize theme system
		setupTheme();
		setupOSThemeListener();

		const pinned = getCookie('menuPinned');
		isMenuPinned = pinned === 'true';

		(async () => {
			// handle session
			// if the user already has a active session, then the session will be
			// restored and the user will be logged in
			try {
				if (!session.isRunning) {
					await session.start();
				}
			} catch (e) {
				console.error(e);
			}

			setInterval(
				async () => {
					await checkForUpdate();
				},
				1000 * 60 * 60
			);
		})();

		const appStateUnsubscribe = appState.subscribe((s) => {
			// sync any changes related to view functionality
			let loginChanged = false;
			let installChanged = false;
			// sync local state with app state on changes
			if (s.loginStatus != loginStatus) {
				loginStatus = s.loginStatus;
				loginChanged = true;
			}
			if (s.installStatus != installState) {
				installState = s.installStatus;
				installChanged = true;
			}
			if (s.isReady != isReady) {
				isReady = s.isReady;
			}
			// if the user is logged out and not on the login page
			// goto the login page
			if (
				loginChanged &&
				loginStatus === AppStateService.LOGIN.LOGGED_OUT &&
				$page.url.pathname !== '/login/'
			) {
				// tick or goto wont work
				tick().then(() => goto('/login/'));
				return;
			}
			// if the user is logged in and the application is not installed
			// goto the install page
			if (
				(loginChanged || installChanged) &&
				loginStatus === AppStateService.LOGIN.LOGGED_IN &&
				installState === AppStateService.INSTALL.NOT_INSTALLED &&
				$page.url.pathname !== '/install/'
			) {
				// tick or goto wont work
				tick().then(() => goto('/install/'));
				goto('/install/');
				return;
			}

			// if the user is logged in, check for updates and load display mode
			if (loginChanged && loginStatus === AppStateService.LOGIN.LOGGED_IN) {
				(async () => {
					try {
						await checkForUpdate();
						await loadDisplayMode();
					} catch (e) {
						console.error(e);
					}
				})();
			}
			// if the user is logged in and the application is istalled
			// AND the user is on the login or install page or /
			// goto the dashboard
			if (
				(loginChanged || installChanged) &&
				loginStatus === AppStateService.LOGIN.LOGGED_IN &&
				installState === AppStateService.INSTALL.INSTALLED &&
				($page.url.pathname === '/login/' ||
					$page.url.pathname === '/install/' ||
					$page.url.pathname === '/')
			) {
				console.log(s.loginStatus);
				console.log('layout: navigating to /dashboard/');
				// tick fixes weird bug where goto does not work inside this subscribe
				if (isDevelopement) {
					console.log('dev: Skipping navigation');
				} else {
					tick().then(() => goto('/dashboard/'));
				}
				return;
			}
		});

		// on unmount
		return () => {
			console.log('layout: unmounting');
			// stop listening for sessions
			try {
				session.stop();
			} catch (e) {
				console.error('tried to stop session but failed', e);
			}
			appStateUnsubscribe();
		};
	});

	// component logic
	const checkForUpdate = async () => {
		try {
			const res = await api.application.isUpdateAvailableCached();
			if (!res.success) {
				throw res.error;
			}
			appState.setIsUpdateAvailable(res.data.updateAvailable);
		} catch (e) {
			console.error('failed to check for update', e);
		}
	};

	const loadDisplayMode = async () => {
		try {
			const res = await api.option.get('display_mode');
			if (res.success && res.data.value) {
				displayMode.setMode(res.data.value);
			}
		} catch (e) {
			console.error('failed to load display mode', e);
		}
	};

	const logout = async () => {
		try {
			showIsLoading();
			const res = await UserService.instance.logout();
			if (!res.success) {
				throw res.error;
			}
			localStorage.clear();
			appState.clearContext();
			goto('/login/');
		} catch (e) {
			console.error('failed to logout', e);
		} finally {
			isMobileMenuVisible = false;
			isProfileMenuVisible = false;
			hideIsLoading();
		}
		return false; // cancel navigation
	};

	const toggleChangeCompanyModal = async () => {
		isChangeCompanyModalVisible = !isChangeCompanyModalVisible;
	};

	// pin toggle handler
	let desktopMenuRef;
	function handlePinToggle() {
		if (isMenuPinned) {
			isMenuPinned = false;
			setCookie('menuPinned', 'false');
			desktopMenuRef?.collapseMenu();
		} else {
			isMenuPinned = true;
			setCookie('menuPinned', 'true');
		}
	}
</script>

<div class="flex flex-col min-w-[768px]">
	<!-- global components -->
	<Loader />
	<Toast />
	<ChangeCompanyModal bind:visible={isChangeCompanyModalVisible} />
	<CommandPalette bind:visible={isCommandPaletteVisible} {toggleChangeCompanyModal} />
	<!-- VIEW -->
	{#if !isReady}
		<RootLoader />
	{:else if loginStatus === AppStateService.LOGIN.LOGGED_OUT && $page.route.id === '/login'}
		<slot />
	{:else if loginStatus === AppStateService.LOGIN.LOGGED_IN && $page.route.id !== '/login'}
		<Header bind:isProfileMenuVisible bind:isMobileMenuVisible {toggleChangeCompanyModal} />
		{#if installState === AppStateService.INSTALL.INSTALLED}
			<DesktopMenu
				bind:this={desktopMenuRef}
				isPinned={isMenuPinned}
				on:pinToggle={handlePinToggle}
			/>
		{/if}
		<div class={`grid grid-cols-12 mr-10 mt-4 ${isMenuPinned ? 'menu-pinned ml-52' : 'ml-24'}`}>
			{#if installState === AppStateService.INSTALL.INSTALLED}
				<MobileMenu
					bind:visible={isMobileMenuVisible}
					onClickLogout={logout}
					{toggleChangeCompanyModal}
				/>
				<ProfileMenu {logout} bind:visible={isProfileMenuVisible} {toggleChangeCompanyModal} />
			{/if}
			<div class="col-start-1 col-end-13 row-start-1">
				<slot />
			</div>
		</div>
	{/if}
</div>

<style>
	:global(body, table th) {
		user-select: none;

		/* font-family: "Phudu"; */
		font-family: 'Titillium Web';
		min-width: 768px;
		overflow-x: auto;
	}
	:global(table) {
		user-select: text;
	}
	.menu-pinned {
		grid-template-columns: 10rem 1fr !important;
	}
</style>
