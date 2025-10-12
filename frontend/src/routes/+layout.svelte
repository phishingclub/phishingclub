<script>
	import '../app.css';
	import { onMount, tick } from 'svelte';
	import { page } from '$app/stores';
	import { beforeNavigate, goto } from '$app/navigation';
	import DeveloperPanel from '$lib/components/DeveloperPanel.svelte';
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
	import DesktopMenu from '$lib/components/header/DesktopMenu.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import Header from '$lib/components/header/Header.svelte';
	import CompanyBanner from '$lib/components/header/CompanyBanner.svelte';
	import { setupTheme, setupOSThemeListener } from '$lib/theme.js';
	// Removed feature flags import - no longer needed

	// services
	const session = Session.instance;
	const appState = AppStateService.instance;
	const api = API.instance;

	// local state
	let loginStatus = AppStateService.LOGIN.UNKNOWN;
	let installState = AppStateService.INSTALL.UNKNOWN;

	let user = {
		name: '',
		username: '',
		role: ''
	};
	let isProfileMenuVisible = false;
	let isMobileMenuVisible = false;
	let isChangeCompanyModalVisible = false;
	let isReady = false;

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
			// sync any changes to user and scope changes
			user = {
				name: s.user.name,
				username: s.user.username,
				role: s.user.role
			};
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

			// if the user is logged in, check for updates
			if (loginChanged && loginStatus === AppStateService.LOGIN.LOGGED_IN) {
				(async () => {
					try {
						await checkForUpdate();
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
				tick().then(() => goto('/dashboard/'));
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
</script>

<div class="flex flex-col min-w-[768px]">
	<!-- global components -->
	<DeveloperPanel />
	<Loader />
	<Toast />
	<ChangeCompanyModal bind:visible={isChangeCompanyModalVisible} />
	<!-- VIEW -->
	{#if !isReady}
		<RootLoader />
	{:else if loginStatus === AppStateService.LOGIN.LOGGED_OUT && $page.route.id === '/login'}
		<slot />
	{:else if loginStatus === AppStateService.LOGIN.LOGGED_IN && $page.route.id !== '/login'}
		<Header bind:isProfileMenuVisible bind:isMobileMenuVisible {toggleChangeCompanyModal} />
		{#if installState === AppStateService.INSTALL.INSTALLED}
			<DesktopMenu />
		{/if}
		<div class="grid grid-cols-12 lg:ml-24 ml-8 mr-10 mt-4">
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
</style>
