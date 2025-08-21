<script>
	import { goto } from '$app/navigation';
	import Input from '../../lib/components/Input.svelte';
	import CTAbutton from '../../lib/components/CTAbutton.svelte';
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
	let inputType = 'password';
	let isPasswordVisible = true;
	let isSSOEnabled = false;

	let isSubmitting = false;

	// hooks
	onMount(() => {
		// if the user is already logged in, we want to redirect to the dashboard
		if (appState.isLoggedIn()) {
			console.info('login: navigating to /dashboard');
			goto('/dashboard/');
			return;
		}
		refreshIsSSOEnabled();
		if ($page.url.searchParams.get('ssoAuthError')) {
			addToast('SSO login failed', 'Error');
		}
	});

	const refreshIsSSOEnabled = async () => {
		showIsLoading();
		try {
			const res = await api.sso.isEnabled();
			if (res.data) {
				isSSOEnabled = true;
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
		// bug - this fixes a bug where if a user
		// clicks enter inside the button field, the password is shown
		if (e.target.tagName === 'BUTTON') {
			return;
		}
		isPasswordVisible = !isPasswordVisible;
		if (isPasswordVisible) {
			inputType = 'password';
		} else {
			inputType = 'text';
		}
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
	class="h-screen grid-cols-1 grid md:grid-cols-1 lg:grid-cols-2 xl:grid-cols-2 2xl:grid-cols-2"
>
	<div class="flex items-center justify-center h-full">
		<img
			class="fixed center top-6 w-1/4 md:w-1/4 lg:w-1/6 xl:w-1/6 2xl:w-1/6 lg:top-6 lg:left-4 xl:top-6 xl:left-4 2xl:top-6 2xl:left-4"
			src="/logo-blue.svg"
			alt="phishing club logo"
		/>
		<div
			class="flex flex-col items-center justify-center p-4 w-full sm:w-full md:w-3/4 lg:w-2/3 xl:w-2/3 2xl:w-2/3"
		>
			<div class="flex flex-col items-center justify-center w-full p-4">
				<h1
					class="text-4xl md:text-5xl lg:text-5xl xl:text-5xl 2xl:text-5xl font-titilium font-bold uppercase text-pc-darkblue text-center"
				>
					Please sign in
				</h1>
			</div>

			<div class="flex flex-col items-center justify-center w-full p-px md:p-px lg:p-4">
				{#if loginError}
					<div
						class="flex justify-center w-9/10 bg-message-red border-pc-red border text-center py-4 font-titilium"
					>
						{loginError}
					</div>
				{/if}

				<form
					id="login-form"
					on:submit={(e) => onSubmitLogin('password', e)}
					class="flex flex-col items-center justify-center w-full md:p-px lg:p-4"
				>
					<Input
						fieldName={'Username'}
						type="text"
						bind:value={formValues.username}
						submitOnEnter
					/>
					<div class="flex flex-col w-full p-4 h-24">
						<label for="Password" class="text-md font-semibold font-titilium text-pc-darkblue"
							>Password</label
						>
						<div class="relative flex items-center justify-end">
							<button
								type="button"
								class="absolute h-10 w-8 z-10 mr-2 hover:opacity-70"
								tabindex="-1"
								on:click={handleClick}
							>
								{#if isPasswordVisible}
									<img src="/view.svg" alt="view" />
								{:else}
									<img src="/toggle-view.svg" alt="toggle view" />
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
								class="relative w-full p-2 rounded bg-pc-lightblue focus:outline-none focus:ring-0 focus:border-cta-blue focus:border-2"
							/>
						</div>
					</div>
					<CTAbutton disabled={isSubmitting} />
					{#if isSSOEnabled}
						<div class="absolute bottom-12">
							<div class="text-center font-bold">SSO</div>
							<a href="/api/v1/sso/entra-id/login">
								<img src="/ms-login-light.svg" alt="Login with Microsoft" />
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
								<p class="text-center text-lg">Enter the MFA code from your authenticator app</p>
								<Input fieldName={'MFA Code'} type="text" bind:value={formValues.mfaTOTP} />

								<div class="flex flex-col">
									<p class="text-sm">
										Forgot your MFA code? Recover your code <button
											type="button"
											on:click={showMFARecoveryModal}
											class="text-cta-blue underline">here</button
										>
									</p>
								</div>
							</FormColumn>
						</FormColumns>
						<FormError message={mfaError} />
						<div
							class="row-start-7 py-4 row-span-2 col-start-1 col-span-3 border-t-2 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end"
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
								<div class="px-16 py-8">
									<p class="text-center text-lg">Please enter the MFA recovery code</p>
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
							class="row-start-7 py-4 row-span-2 col-start-1 col-span-3 border-t-2 w-full flex flex-row justify-center items-center sm:justify-center md:justify-center lg:justify-end xl:justify-end 2xl:justify-end"
						>
							<FormButton>Verify</FormButton>
						</div>
					</FormGrid>
				</Modal>
			</div>
		</div>
	</div>
	<div class="flex">
		<div class="overflow-hidden hidden sm:hidden md:hidden lg:flex xl:flex 2xl:flex">
			<img class="h-full max-w-fit" src="/login-graphics.svg" alt="lady beign phished" />
		</div>
	</div>
</main>
