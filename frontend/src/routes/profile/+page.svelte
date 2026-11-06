<script>
	import { api } from '$lib/api/apiProxy.js';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { UserService } from '$lib/service/user';
	import FormButton from '$lib/components/FormButton.svelte';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Form from '$lib/components/Form.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Button from '$lib/components/Button.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import Headline from '$lib/components/Headline.svelte';
	import { hideIsLoading, showIsLoading } from '$lib/store/loading';
	import Alert from '$lib/components/Alert.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import { onClickCopy } from '$lib/utils/common';

	// same footer row as SettingsCard so profile and settings buttons line up
	const footerClass = 'mt-auto pt-6 flex justify-end';
	const noteClass =
		'bg-gray-50 dark:bg-gray-900/40 p-4 rounded-md text-sm text-gray-600 dark:text-gray-300';

	// services
	const userService = UserService.instance;

	// bindings
	const changeNameFormValues = {
		fullname: null
	};

	const changeUsernameFormValues = {
		username: null
	};

	const changePasswordFormValues = {
		currentPassword: null,
		newPassword: null,
		repeatNewPassword: null
	};

	let apiKey = '';
	let apiKeyTemp = '';

	// local state
	let isInitiallyLoaded = false;
	let isAPIModalVisible = false;
	let isNewAPIKeyModalVisible = false;
	let isDeleteAPIKeyModalVisible = false;
	let isMFASetupModalVisible = false;
	let isDisableMFAModalVisible = false;
	let isMFAEnabled = false;
	let mfaSetupVerified = false;

	let changeNameError = '';
	let changeUsernameError = '';
	let changePasswordError = '';
	let mfaSetupError = '';
	let mfaVerifyError = '';
	let mfaDisableError = '';
	let apiKeyError = '';

	let mfaSetupFormValues = {
		password: '',
		verificationCode: ''
	};
	let mfaDisableFormValues = {
		totpToken: ''
	};
	let mfaValues = {
		totpCode: '',
		totpURL: '',
		totpRecoveryCode: ''
	};

	let hasPassword = false;

	// component logic
	const resetMFAValues = () => {
		mfaSetupFormValues.password = '';
		mfaSetupFormValues.verificationCode = '';
		mfaValues.totpCode = '';
		mfaValues.totpURL = '';
		mfaValues.toptRecoveryCode = '';
		mfaDisableFormValues.totpToken = '';
	};

	// hooks
	onMount(async () => {
		showIsLoading();
		await refreshUser();
		await refreshIsMFAEnabled();
		await refreshMaskeAPIKey();
		hideIsLoading();
		isInitiallyLoaded = true;
	});

	// component logic
	const refreshUser = async () => {
		try {
			// ping to get the latest user info from the session details
			const res = await api.session.ping();
			if (!res.success) {
				throw res.error;
			}
			const res2 = await api.user.getByID(res.data.userID);
			if (!res.success) {
				throw res.error;
			}
			changeUsernameFormValues.username = res2.data.username;
			changeNameFormValues.fullname = res2.data.name;
			hasPassword = !!res2.data.hasPassword;
			return;
		} catch (e) {
			addToast('Failed to load user', 'Error');
			console.error('failed to load user', e);
		}
	};

	const onClickChangeUsername = async () => {
		changeUsernameError = '';
		try {
			const res = await userService.changeUsername(changeUsernameFormValues.username);
			if (!res.success) {
				changeUsernameError = res.error;
				throw res.error;
			}
			changeUsernameError = '';
		} catch (e) {
			console.error('failed to change username', e);
			return false;
		}
		return true;
	};

	const onClickChangeFullname = async () => {
		changeNameError = '';
		try {
			const res = await userService.changeFullname(changeNameFormValues.fullname);
			if (!res.success) {
				changeNameError = res.error;
				throw res.error;
			}
			changeNameError = '';
		} catch (e) {
			console.error('failed to change name', e);
			return false;
		}
		return true;
	};

	const onClickChangePassword = async () => {
		changePasswordError = '';
		// check if the new password and repeated password match
		if (changePasswordFormValues.newPassword !== changePasswordFormValues.repeatNewPassword) {
			changePasswordError = 'Current and repeated password do not match.';
			return;
		}
		try {
			const res = await api.user.changePassword(
				changePasswordFormValues.currentPassword,
				changePasswordFormValues.newPassword
			);
			if (!res.success) {
				changePasswordError = res.error;
				return;
			}
			addToast('Password changed - login required', 'Success');
			userService.clear();
			console.info('profile: changed password - navigating to login');
			goto('/login/');
		} catch (e) {
			addToast('Failed to change password', 'Error');
			console.error('failed to change password', e);
		}
	};

	const closeMFASetupModal = () => {
		isMFASetupModalVisible = false;
		mfaSetupVerified = false;
		resetMFAValues();
	};

	const refreshIsMFAEnabled = async () => {
		try {
			const res = await api.user.isTOTPMFAEnabled();
			if (!res.success) {
				throw res.error;
			}
			isMFAEnabled = res.data.enabled;
			return;
		} catch (err) {
			console.error('failed to load MFA status', err);
			addToast('Failed to load MFA status', 'Error');
		}
	};

	const onClickSetupTOTPMFA = async () => {
		mfaSetupError = '';
		try {
			const res = await api.user.setupTOTPMFA(mfaSetupFormValues.password);
			if (!res.success) {
				mfaSetupError = res.error;
				return;
			}
			isMFASetupModalVisible = true;
			mfaValues.totpCode = res.data.base32;
			mfaValues.totpURL = res.data.url;
			mfaValues.totpRecoveryCode = res.data.recoveryCode;

			// send a POST request to get the QR code
			// TODO move this to the API client
			const qrResponse = await fetch(`/api/v1/qr/totp`, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({
					url: res.data.url
				})
			});
			if (!qrResponse.ok) {
				throw new Error('failed to get QR code');
			}
			const blob = await qrResponse.blob();
			const imageBlob = URL.createObjectURL(blob);
			const img = document.getElementById('totp-qr-code');
			if (!(img instanceof HTMLImageElement)) {
				throw new Error('failed to get img element');
			}
			img.src = imageBlob;
		} catch (e) {
			addToast('Failed to setup OTP MFA', 'Error');
			console.error('failed to setup OTP MFA', e);
		}
	};

	const refreshMaskeAPIKey = async () => {
		try {
			// ping to get the latest user info from the session details
			const res = await api.user.getAPIKeyMasked();
			if (!res.success) {
				throw res.error;
			}
			apiKey = res.data.apiKey;
			return;
		} catch (e) {
			addToast('Failed to get API key', 'Error');
			console.error('failed to get API key', e);
		}
	};

	const onClickCreateAPIKey = async () => {
		apiKeyError = '';
		// if no api key exists, create it and show the copy alert
		if (!apiKey.length) {
			try {
				await createAPIKey();
				isAPIModalVisible = true;
			} catch (e) {}
		} else {
			// if a key already exist, it will be overwritten, show a warning
			isNewAPIKeyModalVisible = true;
		}
	};

	const createAPIKey = async () => {
		try {
			const res = await api.user.upsertAPIKey();
			if (!res.success) {
				apiKeyError = res.error;
				return false;
			}
			apiKeyTemp = res.data.apiKey;
			addToast('Created API key', 'Success');
		} catch (e) {
			addToast('Failed to create API key', 'Error');
			console.error('failed to create API key', e);
			throw e;
		}
	};

	const openShowRemoveAPIKeyModal = (e) => {
		e.preventDefault();
		isDeleteAPIKeyModalVisible = true;
	};

	const closeShowRemoveAPIKeyModal = () => {
		isDeleteAPIKeyModalVisible = false;
	};

	const removeAPIKey = async () => {
		apiKeyError = '';
		try {
			const res = await api.user.removeAPIKey();
			if (!res.success) {
				apiKeyError = res.error;
				return;
			}
			apiKey = '';
			addToast('Removed API key', 'Success');
		} catch (e) {
			addToast('Failed to remove API key', 'Error');
			console.error('failed to remove API key', e);
		}
	};

	const onClickVerifyTOTPMFA = async () => {
		mfaVerifyError = '';
		try {
			const res = await api.user.setupVerifyTOTPMFA(mfaSetupFormValues.verificationCode);
			if (!res.success) {
				mfaVerifyError = res.error;
				return;
			}
			addToast('MFA setup complete', 'Success');
			isMFAEnabled = true;
			mfaSetupVerified = true;
			mfaValues.totpCode = '';
			mfaValues.totpURL = '';
			refreshIsMFAEnabled();
		} catch (e) {
			addToast('Failed to verify OTP MFA', 'Error');
			console.error('failed to verify OTP MFA', e);
		}
	};

	const onClickDisableMFA = async () => {
		mfaDisableError = '';
		try {
			const res = await api.user.disableTOTPMFA(mfaDisableFormValues.totpToken);
			if (!res.success) {
				mfaDisableError = res.error;
				return;
			}
			addToast('MFA has been disabled', 'Success');
			closeDisableMFAModal();
			isMFAEnabled = false;
			refreshIsMFAEnabled();
		} catch (e) {
			addToast('Failed to disable MFA', 'Error');
			console.error('failed to disable MFA', e);
		}
	};

	const closeDisableMFAModal = () => {
		isDisableMFAModalVisible = false;
		resetMFAValues();
	};
</script>

<HeadTitle title="Profile" />
<main class="pb-8">
	<Headline docSlug="settings">Profile</Headline>
	{#if isInitiallyLoaded}
		<div class="max-w-7xl pt-4 space-y-8">
			<!-- Profile and Password Section -->
			<div class="flex flex-wrap gap-6">
				<SettingsCard title="Account Details" description="Your username and display name.">
					<Form
						fullHeight
						fullWidth
						on:submit={async () => {
							const res = await Promise.all([onClickChangeUsername(), onClickChangeFullname()]);
							if (res.some((r) => !r)) {
								addToast('Failed to update profile', 'Error');
								return;
							}
							addToast('Profile updated', 'Success');
						}}
					>
						<div>
							<TextField
								required
								minLength={1}
								maxLength={64}
								width="full"
								pattern="[a-zA-Z0-9]+"
								bind:value={changeUsernameFormValues.username}>Username</TextField
							>
							<TextField
								required
								minLength={1}
								maxLength={64}
								width="full"
								bind:value={changeNameFormValues.fullname}>Name</TextField
							>
							<FormError message={changeUsernameError} />
							<FormError message={changeNameError} />
						</div>
						<div class={footerClass}>
							<FormButton size={'large'}>Save Changes</FormButton>
						</div>
					</Form>
				</SettingsCard>

				<SettingsCard
					title="Password Settings"
					description={hasPassword ? 'Change the password you sign in with.' : ''}
				>
					{#if hasPassword}
						<Form fullHeight fullWidth on:submit={onClickChangePassword}>
							<div>
								<PasswordField
									required
									minLength={16}
									maxLength={64}
									width="full"
									bind:value={changePasswordFormValues.currentPassword}
									>Current password</PasswordField
								>
								<PasswordField
									required
									minLength={16}
									maxLength={64}
									width="full"
									bind:value={changePasswordFormValues.newPassword}>New password</PasswordField
								>
								<PasswordField
									minLength={16}
									maxLength={64}
									required
									width="full"
									bind:value={changePasswordFormValues.repeatNewPassword}
									>Repeat new password</PasswordField
								>
								<FormError message={changePasswordError} />
							</div>
							<div class={footerClass}>
								<FormButton size={'large'}>Update Password</FormButton>
							</div>
						</Form>
					{:else}
						<p class={noteClass}>This account has no password set and signs in via SSO.</p>
					{/if}
				</SettingsCard>

				<SettingsCard
					title="Multi-Factor Authentication"
					description={!hasPassword
						? ''
						: isMFAEnabled
							? 'Sign in asks for a code from your authenticator app.'
							: 'Confirm your password to set up an authenticator app.'}
				>
					{#if hasPassword}
						{#if isMFAEnabled}
							<div class="flex items-center gap-2 mt-2">
								<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
								<StatusBadge tone="success">MFA is enabled</StatusBadge>
							</div>
							<div class={footerClass}>
								<Button
									size={'large'}
									type="button"
									variant="danger-outline"
									on:click={() => {
										isDisableMFAModalVisible = true;
									}}>Disable MFA</Button
								>
							</div>
						{:else}
							<Form on:submit={onClickSetupTOTPMFA} fullHeight fullWidth>
								<div>
									<div class="flex items-center gap-2 mt-2 mb-2">
										<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
										<StatusBadge tone="warning">MFA is not enabled</StatusBadge>
									</div>
									<PasswordField
										required
										minLength={16}
										maxLength={64}
										width="full"
										bind:value={mfaSetupFormValues.password}>Password</PasswordField
									>
									<FormError message={mfaSetupError} />
								</div>
								<div class={footerClass}>
									<FormButton size={'large'}>Set up MFA</FormButton>
								</div>
							</Form>
						{/if}
					{:else}
						<p class={noteClass}>MFA is managed through your SSO provider.</p>
					{/if}
				</SettingsCard>

				<SettingsCard title="API Access">
					<Form fullHeight fullWidth on:submit={onClickCreateAPIKey}>
						{#if !!apiKey.length}
							<div>
								<TextField readonly width="full" bind:value={apiKey}>
									Access key {apiKey.length ? '(masked)' : ''}
								</TextField>
								<FormError message={apiKeyError} />
							</div>
							<div class={footerClass}>
								<Button
									size={'large'}
									variant="danger-outline"
									type="button"
									on:click={openShowRemoveAPIKeyModal}>Delete Key</Button
								>
							</div>
						{:else}
							<p class="mb-3 text-sm text-gray-600 dark:text-gray-300">
								An API key lets scripts and integrations call the API with your permissions.
							</p>
							<p class={noteClass}>No API key exists.</p>
							<div class={footerClass}>
								<FormButton size={'large'}>Create API Key</FormButton>
							</div>
						{/if}
					</Form>
				</SettingsCard>
			</div>
		</div>
		<!-- Modals -->
		<Modal
			bind:visible={isDisableMFAModalVisible}
			headerText="Disable Multi-Factor Authentication"
			onClose={closeDisableMFAModal}
		>
			<FormGrid on:submit={onClickDisableMFA}>
				<FormColumns>
					<FormColumn>
						<div class="space-y-4 w-full">
							<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
								<p class="text-sm text-gray-700 dark:text-gray-300">
									Disabling MFA will reduce the security of your account. Please confirm this action
									by entering a verification code.
								</p>
							</div>

							<TextField
								required
								bind:value={mfaDisableFormValues.totpToken}
								placeholder="Enter 6-digit code"
							>
								Verification Code
							</TextField>
							<FormError message={mfaDisableError} />
						</div>
					</FormColumn>
				</FormColumns>
				<FormFooter closeModal={closeDisableMFAModal} okText="Disable MFA" />
			</FormGrid>
		</Modal>
		<!--
		bind:visible={isMFASetupModalVisible}
		-->

		<Modal
			headerText="Setup Multi-Factor Authentication"
			bind:visible={isMFASetupModalVisible}
			onClose={closeMFASetupModal}
		>
			{#if !mfaSetupVerified}
				<FormGrid on:submit={onClickVerifyTOTPMFA}>
					<FormColumns>
						<FormColumn>
							<!-- Step 1 -->
							<div class="space-y-6 w-full">
								<div>
									<h3 class="text-xl font-semibold text-gray-700 dark:text-white mb-2">
										1. Scan QR Code
									</h3>
									<p class="text-gray-600 dark:text-gray-300 mb-4">
										Use your authenticator app (like Google Authenticator or Authy) to scan the QR
										code below.
									</p>
									<div class="flex justify-center bg-gray-50 dark:bg-gray-700 p-6 rounded-lg">
										<img class="max-w-[200px]" id="totp-qr-code" alt="QR code for MFA TOTP setup" />
									</div>
								</div>

								<!-- Alternative Method -->
								<div class="border-gray-200 dark:border-gray-600 pt-4">
									<h3 class="text-lg font-medium text-gray-700 dark:text-gray-200 mb-2">
										Can't scan the QR code?
									</h3>
									<p class="text-gray-600 dark:text-gray-300 mb-2">
										Enter this code manually in your authenticator app:
									</p>
									<button
										type="button"
										on:click|preventDefault={() => onClickCopy(mfaValues.totpCode)}
										class="pc-focus flex items-center bg-gray-100 hover:bg-gray-200 dark:bg-gray-700 dark:hover:bg-gray-600 py-2 px-4 rounded-md text-gray-700 dark:text-gray-100 transition-colors"
									>
										<span class="totp-code font-mono">{mfaValues.totpCode}</span>
										<img class="ml-2 w-4 h-4 invert dark:invert-0" src="/icon-copy.svg" alt="copy code" />
									</button>
								</div>

								<!-- Step 2 -->
								<div class="border-t border-gray-200 dark:border-gray-600 pt-4">
									<h3 class="text-xl font-semibold text-gray-700 dark:text-gray-200 mb-2">
										2. Verify Setup
									</h3>
									<p class="text-gray-600 dark:text-gray-300 mb-4">
										Enter the 6-digit code from your authenticator app:
									</p>
									<TextField
										required
										bind:value={mfaSetupFormValues.verificationCode}
										placeholder="Enter 6-digit code"
									/>
									<FormError message={mfaVerifyError} />
								</div>
							</div>
						</FormColumn>
					</FormColumns>
					<FormFooter closeModal={closeMFASetupModal} okText="Verify and Enable" />
				</FormGrid>
			{:else}
				<FormGrid on:submit={closeMFASetupModal}>
					<FormColumns>
						<FormColumn>
							<div class="space-y-6 w-full">
								<div>
									<StatusBadge tone="success">MFA Setup Successful</StatusBadge>
								</div>

								<div>
									<h3 class="text-xl font-semibold text-gray-700 dark:text-white mb-2">
										Save Your Recovery Code
									</h3>
									<p class="text-gray-600 dark:text-gray-300 mb-4">
										Store this recovery code in a safe place. You'll need it if you lose access to
										your authenticator app:
									</p>
									<button
										on:click|preventDefault={() => onClickCopy(mfaValues.totpRecoveryCode)}
										class="pc-focus flex items-center bg-gray-100 hover:bg-gray-200 dark:bg-gray-700 dark:hover:bg-gray-600 py-3 px-4 rounded-md text-gray-700 dark:text-gray-100 w-full mb-4 transition-colors"
									>
										<span class="totp-recoveryCode font-mono">{mfaValues.totpRecoveryCode}</span>
										<img class="ml-2 w-4 h-4 invert dark:invert-0" src="/icon-copy.svg" alt="copy code" />
									</button>

									<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md mb-6">
										<p class="font-semibold text-gray-900 dark:text-white mb-2">Important:</p>
										<p class="text-sm text-gray-700 dark:text-gray-300">
											Without this recovery code, you may lose access to your account if you lose
											your authenticator device.
										</p>
									</div>
								</div>
							</div>
						</FormColumn>
					</FormColumns>
					<FormFooter closeModal={closeMFASetupModal} okText="Done" closeText="Close" />
				</FormGrid>
			{/if}
		</Modal>
	{/if}
	<Alert
		headline={'Remove API key'}
		onConfirm={async () => {
			await removeAPIKey();
			isDeleteAPIKeyModalVisible = true;
			closeShowRemoveAPIKeyModal();
			return { success: true };
		}}
		ok="OK"
		bind:visible={isDeleteAPIKeyModalVisible}
	>
		<SubHeadline>
			<span>Please confirm you want to remove the API key.</span>
		</SubHeadline>
	</Alert>
	<Alert
		headline={'New API Key'}
		onConfirm={async () => {
			await createAPIKey();
			await refreshMaskeAPIKey();
			isNewAPIKeyModalVisible = false;
			isAPIModalVisible = true;
			return { success: true };
		}}
		ok="OK"
		bind:visible={isNewAPIKeyModalVisible}
	>
		<SubHeadline>
			<span>Creating a new API key will deactivate the old one.</span>
		</SubHeadline>
	</Alert>
	<Alert
		headline={'API Key'}
		onConfirm={async () => {
			await refreshMaskeAPIKey();
			apiKeyTemp = '';
			isAPIModalVisible = false;
			return { success: true };
		}}
		ok="OK"
		noCancel={true}
		bind:visible={isAPIModalVisible}
	>
		<SubHeadline>
			<span class="font-bold">Copy this key and keep it safe.</span>
			<br /><br />After closing this dialog it will no longer be possible to see the API key again</SubHeadline
		>
		<TextField
			on:click={(e) => {
				e.preventDefault();
				navigator.clipboard.writeText(apiKeyTemp);
				addToast('API key is copied to clipboard', 'Info');
			}}
			readonly
			value={apiKeyTemp}
			width="full">Click to copy</TextField
		>
	</Alert>
</main>
