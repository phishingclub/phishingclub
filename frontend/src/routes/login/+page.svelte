<script>
	import { goto } from '$app/navigation';
	import Input from '../../lib/components/Input.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { onMount } from 'svelte';
	import { UserService } from '$lib/service/user';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import { api } from '$lib/api/apiProxy';
	import { addToast } from '$lib/store/toast';
	import { page } from '$app/stores';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { setupTheme, setupOSThemeListener, theme } from '$lib/theme.js';
	import {
		branding,
		loadBranding,
		brandingImageURL,
		brandingDisplayFor,
		BRANDING_SLOT,
		BRANDING_DEFAULTS
	} from '$lib/store/branding';
	import BrandingImage from '$lib/components/BrandingImage.svelte';

	// branding resolves the login logo and side image, falling back to the
	// bundled defaults when no custom image is uploaded
	$: loginLogoSrc =
		$branding.loginLogo === 'custom'
			? brandingImageURL(BRANDING_SLOT.loginLogo, $branding.version)
			: $theme === 'dark'
				? BRANDING_DEFAULTS.loginLogoDark
				: BRANDING_DEFAULTS.loginLogoLight;
	$: loginLogoDisplay = brandingDisplayFor($branding, BRANDING_SLOT.loginLogo);
	// only collapse to the centered layout once branding has loaded, so the
	// two column layout does not shift while the state is still unknown
	$: sideImageRemoved = $branding.loaded && $branding.loginSideImageHidden;
	$: sideImageSrc =
		$branding.loginSideImage === 'custom'
			? brandingImageURL(BRANDING_SLOT.loginSideImage, $branding.version)
			: BRANDING_DEFAULTS.loginSideImage;
	$: sideImageDisplay = brandingDisplayFor($branding, BRANDING_SLOT.loginSideImage);

	// services
	const appState = AppStateService.instance;
	const userService = UserService.instance;

	// local state
	let formValues = {
		username: '',
		password: '',
		mfaTOTP: ''
	};
	let mfaRecoveryFormValues = {
		recoveryCode: ''
	};
	let isMFARecoveryModalVisible = false;
	let isMFAModalVisible = false;
	let loginError = '';
	let mfaError = '';
	let mfaRecoveryLoginError = '';
	let isPasswordShown = false;
	$: inputType = isPasswordShown ? 'text' : 'password';
	let isSSOEnabled = false;
	let ssoProviderType = 'entra';
	let exclusiveLogin = false;

	let isSubmitting = false;

	// hooks
	onMount(() => {
		// initialize theme system
		setupTheme();
		setupOSThemeListener();
		// load branding so a custom logo and side image show before login
		loadBranding();

		// if the user is already logged in, we want to redirect to the dashboard
		if (appState.isLoggedIn()) {
			console.info('login: navigating to /dashboard');
			goto('/dashboard/');
			return;
		}
		refreshIsSSOEnabled();
		if ($page.url.searchParams.get('ssoAuthError')) {
			addToast(ssoErrorMessage($page.url.searchParams.get('reason')), 'Error');
		}
	});

	// maps the reason code from the SSO callback to a message shown to the user
	const ssoErrorMessage = (reason) => {
		switch (reason) {
			case 'not_provisioned':
				return 'SSO login failed: this account does not exist in Phishing Club. An administrator must create it first.';
			case 'email_not_verified':
				return 'SSO login failed: your email is not verified by the identity provider.';
			case 'no_email':
				return 'SSO login failed: the identity provider did not return an email address.';
			case 'mfa_required':
				return 'SSO login failed: multi factor authentication is required by the configured authentication context.';
			default:
				return 'SSO login failed';
		}
	};

	const refreshIsSSOEnabled = async () => {
		showIsLoading();
		try {
			const res = await api.sso.isEnabled();
			if (res.data && res.data.enabled) {
				isSSOEnabled = true;
				ssoProviderType = res.data.providerType || 'entra';
				exclusiveLogin = !!res.data.exclusiveLogin;
			}
		} catch (e) {
			addToast('failed to check sso status', 'Error');
			console.error('failed to check sso status', e);
		} finally {
			hideIsLoading();
		}
	};

	/**
	 * Submit the MFA recovery code
	 * @param {'password'|'totp'|'recovery'} method
	 * @param {Event} event  the submit event
	 */
	const onSubmitLogin = async (method, event) => {
		if (event) {
			event.preventDefault();
		}
		isSubmitting = true;
		showIsLoading();
		try {
			let res;
			switch (method) {
				case 'password': {
					res = await userService.login(formValues.username, formValues.password);
					break;
				}
				case 'totp': {
					res = await userService.login(
						formValues.username,
						formValues.password,
						formValues.mfaTOTP
					);
					break;
				}
				case 'recovery': {
					res = await userService.login(
						formValues.username,
						formValues.password,
						'',
						mfaRecoveryFormValues.recoveryCode
					);
					break;
				}
			}
			switch (res.statusCode) {
				case 200: {
					loginError = '';
					if (res.data.mfa) {
						isMFAModalVisible = true;
						return;
					}
					if (method === 'recovery') {
						alert('WARNING: MFA IS NOW DISABLED');
						return;
					}
					break;
				}
				case 400: {
					// if a mfa token was supplied then the error is for MFA
					if (formValues.mfaTOTP) {
						mfaError = 'Invalid MFA code';
						break;
					}
					if (method === 'recovery') {
						mfaRecoveryLoginError = 'Invalid recovery code';
						break;
					}
					loginError = 'Invalid credentials';
				}
				case 401: {
					loginError = 'Invalid credentials';
					break;
				}
				case 429: {
					if (method === 'recovery') {
						mfaRecoveryLoginError = 'Too many attempts - try again in a few seconds';
						break;
					}
					if (formValues.mfaTOTP) {
						mfaError = 'Too many attempts - try again in a few seconds';
						break;
					}
					loginError = 'Too many attempts - try again in a few seconds';
					// if a mfa token was supplied then the error is for MFA
					break;
				}
				default: {
					if (method === 'recovery') {
						mfaRecoveryLoginError = 'Unknown error';
						break;
					}
					if (formValues.mfaTOTP) {
						mfaError = 'Unknown error';
						break;
					}
					loginError = 'Unknown error';
					break;
				}
			}
		} catch (e) {
			console.error(e);
		} finally {
			isSubmitting = false;
			hideIsLoading();
		}
		return false;
	};
	const showMFARecoveryModal = () => {
		isMFARecoveryModalVisible = true;
	};

	const handleClick = (e) => {
		e.preventDefault();
		isPasswordShown = !isPasswordShown;
	};

	const closeMFAModal = () => {
		isMFAModalVisible = false;
		formValues.mfaTOTP = '';
		mfaError = '';
	};

	const closeMFARecoveryModal = () => {
		isMFARecoveryModalVisible = false;
		mfaRecoveryFormValues.recoveryCode = '';
		mfaRecoveryLoginError = '';
	};

	const openMFARecoveryModal = () => {
		closeMFAModal();
		isMFARecoveryModalVisible = true;
	};
</script>

<HeadTitle title="Sign in" />
<main
	class="h-screen grid grid-cols-1 {sideImageRemoved
		? ''
		: 'lg:grid-cols-2 xl:grid-cols-2 2xl:grid-cols-2'} bg-white dark:bg-gray-900 transition-colors duration-200"
>
	<!-- theme toggle -->
	<div class="fixed top-3 right-6 z-50">
		<ThemeToggle />
	</div>

	<div class="flex items-center justify-center h-full">
		<BrandingImage
			src={loginLogoSrc}
			alt="phishing club logo"
			display={loginLogoDisplay}
			ready={$branding.loaded}
			boxClass="fixed top-6 left-4 z-40 h-24 w-36 sm:w-48 lg:w-60"
		/>
		<div
			class="flex flex-col items-center justify-center p-4 w-full sm:w-full md:w-3/4 lg:w-2/3 xl:w-2/3 2xl:w-2/3 {sideImageRemoved
				? 'lg:max-w-xl'
				: ''}"
		>
			<div class="flex flex-col items-center justify-center w-full p-4">
				<h1
					class="text-4xl md:text-5xl lg:text-5xl xl:text-5xl 2xl:text-5xl font-titilium font-bold uppercase text-pc-darkblue dark:text-white text-center transition-colors duration-200"
				>
					Please sign in
				</h1>
			</div>

			<div class="flex flex-col items-center justify-center w-full p-px md:p-px lg:p-4">
				{#if loginError}
					<div
						role="alert"
						class="mx-4 w-[calc(100%-2rem)] rounded-md bg-message-red dark:bg-red-900/60 border-pc-red dark:border-red-700 border text-center px-4 py-3 font-titilium text-gray-900 dark:text-white transition-colors duration-200"
					>
						{loginError}
					</div>
				{/if}

				<form
					id="login-form"
					on:submit={(e) => onSubmitLogin('password', e)}
					class="flex flex-col items-center justify-center w-full md:p-px lg:p-4"
				>
					{#if !exclusiveLogin}
					<div class="flex flex-col w-full p-4 h-24">
						<label
							for="Username"
							class="text-md font-semibold font-titilium text-pc-darkblue dark:text-gray-300 transition-colors duration-200"
							>Username</label
						>
						<input
							required
							on:keyup={(event) => {
								const t = /** @type {HTMLInputElement} */ (event.target);
								formValues.username = t.value;
							}}
							on:keydown={(event) => {
								if (event.key === 'Enter') {
									onSubmitLogin('password', null);
								}
							}}
							value={formValues.username}
							autocomplete="off"
							tabindex="0"
							type="text"
							id="Username"
							name="Username"
							class="w-full p-2 rounded-md border-2 border-transparent bg-pc-lightblue text-pc-darkblue dark:bg-gray-800 dark:border-gray-600 dark:text-white focus:outline-none focus:ring-0 focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
						/>
					</div>
					<div class="flex flex-col w-full p-4 h-24">
						<label
							for="Password"
							class="text-md font-semibold font-titilium text-pc-darkblue dark:text-gray-300 transition-colors duration-200"
							>Password</label
						>
						<div class="relative flex items-center justify-end">
							<button
								type="button"
								class="pc-focus absolute h-8 w-8 z-10 mr-2 p-1 rounded hover:opacity-70"
								aria-label={isPasswordShown ? 'Hide password' : 'Show password'}
								aria-pressed={isPasswordShown}
								on:click={handleClick}
							>
								{#if isPasswordShown}
									<img
										src="/toggle-view.svg"
										alt=""
										class="dark:filter dark:brightness-0 dark:invert transition-all duration-200"
									/>
								{:else}
									<img
										src="/view.svg"
										alt=""
										class="dark:filter dark:brightness-0 dark:invert transition-all duration-200"
									/>
								{/if}
							</button>
							<input
								required
								on:keyup={(event) => {
									const t = /** @type {HTMLInputElement} */ (event.target);
									formValues.password = t.value;
								}}
								on:keydown={(event) => {
									if (event.key === 'Enter') {
										onSubmitLogin('password', null);
									}
								}}
								value={formValues.password}
								autocomplete="off"
								tabindex="0"
								type={inputType}
								id="Password"
								name="Password"
								class="relative w-full p-2 pr-12 rounded-md border-2 border-transparent bg-pc-lightblue text-pc-darkblue dark:bg-gray-800 dark:border-gray-600 dark:text-white focus:outline-none focus:ring-0 focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
							/>
						</div>
					</div>
					{/if}
					{#if !exclusiveLogin}
						<div class="flex flex-col items-center justify-center w-full p-4 mt-8 h-16">
							<button
								type="submit"
								disabled={isSubmitting}
								class="pc-focus w-full p-2 text-white text-2xl bg-cta-blue hover:bg-active-blue dark:hover:bg-highlight-blue rounded-lg font-phudu font-bold transition-colors duration-200 disabled:opacity-50 dark:disabled:opacity-60 disabled:cursor-not-allowed"
								>Login</button
							>
						</div>
					{/if}
					{#if isSSOEnabled}
						<div class="flex justify-center w-full p-4 {exclusiveLogin ? 'mt-8' : 'mt-2'}">
							<a
								href={ssoProviderType === 'oidc'
									? '/api/v1/sso/oidc/login'
									: '/api/v1/sso/entra-id/login'}
								class="font-titilium font-semibold text-cta-blue dark:text-highlight-blue hover:underline transition-colors duration-200"
							>
								Sign in with SSO
							</a>
						</div>
					{/if}
				</form>
				<Modal headerText={'Multifactor check'} visible={isMFAModalVisible} onClose={closeMFAModal}>
					<FormGrid
						on:submit={(e) => {
							onSubmitLogin('totp', e);
						}}
					>
						<FormColumns>
							<FormColumn>
								<p
									class="text-center text-lg text-gray-900 dark:text-white transition-colors duration-200"
								>
									Enter the MFA code from your authenticator app
								</p>
								<Input fieldName={'MFA Code'} type="text" bind:value={formValues.mfaTOTP} />

								<div class="flex flex-col">
									<p
										class="text-sm text-gray-700 dark:text-gray-300 transition-colors duration-200"
									>
										No access to your authenticator app? Use a recovery code <button
											type="button"
											on:click={showMFARecoveryModal}
											class="pc-focus rounded-sm text-cta-blue dark:text-highlight-blue hover:underline"
											>here</button
										>
									</p>
								</div>
							</FormColumn>
						</FormColumns>
						<FormError message={mfaError} />
						<div
							class="row-start-7 py-4 row-span-2 col-start-1 col-span-3 border-t-2 dark:border-gray-600 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end transition-colors duration-200"
						>
							<FormButton>Verify</FormButton>
						</div>
					</FormGrid>
				</Modal>
				<Modal
					headerText={'MFA Recovery'}
					visible={isMFARecoveryModalVisible}
					onClose={closeMFARecoveryModal}
				>
					<FormGrid
						on:submit={(e) => {
							onSubmitLogin('recovery', e);
						}}
					>
						<FormColumns>
							<FormColumn>
								<div class="sm:px-8 py-4">
									<p
										class="text-center text-lg text-gray-900 dark:text-white transition-colors duration-200"
									>
										Enter one of your MFA recovery codes
									</p>
									<Input
										fieldName={'MFA Recovery Code'}
										type="text"
										bind:value={mfaRecoveryFormValues.recoveryCode}
									/>
								</div>
							</FormColumn>
						</FormColumns>

						<FormError message={mfaRecoveryLoginError} />
						<div
							class="row-start-7 py-4 row-span-2 col-start-1 col-span-3 border-t-2 dark:border-gray-600 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end transition-colors duration-200"
						>
							<FormButton>Verify</FormButton>
						</div>
					</FormGrid>
				</Modal>
			</div>
		</div>
	</div>
	{#if !sideImageRemoved}
		<div class="hidden lg:block h-full">
			<BrandingImage
				src={sideImageSrc}
				alt="login side image"
				display={sideImageDisplay}
				ready={$branding.loaded}
				boxClass="w-full h-full"
			/>
		</div>
	{/if}
</main>
