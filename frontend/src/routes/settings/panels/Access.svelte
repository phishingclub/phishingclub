<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import Button from '$lib/components/Button.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';

	let loaded = false;
	let ssoForm = null;
	let isSSOModalVisible = false;
	let isSSODeleteAlertVisible = false;
	let updateSSOError = '';
	let isSubmitting = false;
	let isSSOEnabled = false;

	const providerOptions = [
		{ value: 'entra', label: 'Microsoft Entra ID' },
		{ value: 'oidc', label: 'Generic OpenID Connect' }
	];

	const defaultSSOFormValues = () => ({
		providerType: 'entra',
		clientID: null,
		tenantID: null,
		redirectURL: null,
		clientSecret: null,
		issuerURL: null,
		scopes: null,
		acrValues: null,
		exclusiveLogin: false
	});

	let ssoSettingsFormValues = defaultSSOFormValues();

	const ssoAuthPath = (providerType) =>
		providerType === 'oidc' ? '/api/v1/sso/oidc/auth' : '/api/v1/sso/entra-id/auth';

	const onProviderChange = (value) => {
		const providerType = value || ssoSettingsFormValues.providerType;
		ssoSettingsFormValues.providerType = providerType;
		ssoSettingsFormValues.redirectURL = `${location.origin}${ssoAuthPath(providerType)}`;
	};

	onMount(async () => {
		try {
			await refreshSSO();
			if (!ssoSettingsFormValues.redirectURL) {
				ssoSettingsFormValues.redirectURL = `${location.origin}${ssoAuthPath(
					ssoSettingsFormValues.providerType
				)}`;
			}
		} finally {
			loaded = true;
		}
	});

	async function refreshSSO() {
		try {
			const res = await api.option.get('sso_login');
			if (!res.success) {
				throw res.error;
			}
			const sso = JSON.parse(res.data.value);
			sso.clientSecret = '';
			ssoSettingsFormValues = { ...defaultSSOFormValues(), ...sso };
			if (!ssoSettingsFormValues.providerType) {
				ssoSettingsFormValues.providerType = 'entra';
			}
			isSSOEnabled = sso.enabled;
		} catch (e) {
			console.error('failed to get SSO configuration', e);
		}
	}

	const onSubmitSSO = async () => {
		updateSSOError = '';
		isSubmitting = true;
		try {
			const res = await api.sso.upsert(ssoSettingsFormValues);
			if (!res.success) {
				updateSSOError = res.error;
				return;
			}
			closeSSOModal();
			refreshSSO();
		} catch (e) {
			addToast('Failed to update SSO configuration', 'Error');
			console.error('failed to update SSO configuration', e);
		} finally {
			isSubmitting = false;
		}
	};

	const openSSOModal = async (e) => {
		e.preventDefault();
		isSSOModalVisible = true;
	};

	const closeSSOModal = () => {
		updateSSOError = '';
		ssoSettingsFormValues = defaultSSOFormValues();
		isSSOModalVisible = false;
	};

	const onClickDisableSSO = async () => {
		const action = api.sso.upsert({
			clientID: '',
			tenantID: '',
			redirectURL: '',
			clientSecret: ''
		});
		action
			.then((res) => {
				if (!res.success) {
					throw res.error;
				}
				refreshSSO();
			})
			.catch((e) => {
				console.error('failed to remove SSO configuration:', e);
			});
		return action;
	};
</script>

{#if !loaded}
	<SettingsLoading />
{:else}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Single Sign-On">
		<div class="bg-gray-50 rounded-md p-3">
			{#if isSSOEnabled}
				<p class="text-sm font-medium text-green-600">
					<span class="inline-block w-2 h-2 rounded-full bg-green-500 mr-2"></span>
					Enabled
				</p>
			{:else}
				<p class="text-sm text-gray-600">
					<span class="inline-block w-2 h-2 rounded-full bg-gray-400 mr-2"></span>
					Disabled
				</p>
			{/if}
		</div>
		<svelte:fragment slot="footer">
			{#if isSSOEnabled}
				<Button
					size={'large'}
					on:click={() => {
						isSSODeleteAlertVisible = true;
					}}>Disable SSO</Button
				>
			{:else}
				<Button size={'large'} on:click={openSSOModal}>Configure SSO</Button>
			{/if}
		</svelte:fragment>
	</SettingsCard>
</div>
{/if}

{#if isSSOModalVisible}
	<Modal bind:visible={isSSOModalVisible} headerText="SSO configuration" onClose={closeSSOModal}>
		<FormGrid on:submit={onSubmitSSO} bind:bindTo={ssoForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<TextFieldSelect
						id="ssoProvider"
						required
						options={providerOptions}
						bind:value={ssoSettingsFormValues.providerType}
						onSelect={onProviderChange}>Provider</TextFieldSelect
					>
					{#if ssoSettingsFormValues.providerType === 'oidc'}
						<TextField
							required
							type="url"
							bind:value={ssoSettingsFormValues.issuerURL}
							placeholder="https://keycloak.example.com/realms/myrealm">Issuer URL</TextField
						>
						<TextField
							required
							bind:value={ssoSettingsFormValues.clientID}
							placeholder="e.g., phishingclub">Client ID</TextField
						>
						<TextField
							optional
							bind:value={ssoSettingsFormValues.scopes}
							placeholder="openid profile email">Scopes</TextField
						>
					{:else}
						<TextField
							required
							bind:value={ssoSettingsFormValues.clientID}
							placeholder="e.g., 8adf8e7c-d3ef-4a1b-b6c5-12345678abcd">Client ID</TextField
						>
						<TextField
							required
							bind:value={ssoSettingsFormValues.tenantID}
							placeholder="e.g., contoso.onmicrosoft.com">Tenant ID</TextField
						>
					{/if}
				</FormColumn>
				<FormColumn>
					<TextField
						required
						type="url"
						bind:value={ssoSettingsFormValues.redirectURL}
						placeholder="https://your-domain.com/auth/callback">Redirect URL</TextField
					>

					<PasswordField
						required={ssoSettingsFormValues.providerType !== 'oidc'}
						optional={ssoSettingsFormValues.providerType === 'oidc'}
						bind:value={ssoSettingsFormValues.clientSecret}
						placeholder="Enter your client secret">Client Secret</PasswordField
					>
					{#if ssoSettingsFormValues.providerType === 'oidc'}
						<TextField optional bind:value={ssoSettingsFormValues.acrValues} placeholder="e.g., mfa"
							>ACR Values</TextField
						>
					{/if}
					<CheckboxField
						inline
						bind:value={ssoSettingsFormValues.exclusiveLogin}
						toolTipText="Disable password login while SSO is enabled.">Exclusive SSO</CheckboxField
					>
				</FormColumn>
				<FormError message={updateSSOError} />
			</FormColumns>
			<FormFooter closeModal={closeSSOModal} okText="Enable SSO" closeText="Cancel" {isSubmitting} />
		</FormGrid>
	</Modal>
{/if}

{#if isSSODeleteAlertVisible}
	<DeleteAlert
		list={[
			'SSO will be disabled',
			'Configuration will be deleted',
			'SSO users will no longer be able to log in',
			'Be sure there is a administrative user without SSO'
		]}
		confirm
		name={'SSO configuration'}
		onClick={() => onClickDisableSSO()}
		bind:isVisible={isSSODeleteAlertVisible}
	/>
{/if}
