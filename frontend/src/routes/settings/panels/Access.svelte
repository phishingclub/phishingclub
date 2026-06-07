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
	import Form from '$lib/components/Form.svelte';

	let loaded = false;
	let ssoForm = null;
	let isSSOModalVisible = false;
	let isSSODeleteAlertVisible = false;
	let updateSSOError = '';
	let isSubmitting = false;
	let isSSOEnabled = false;
	let ssoSettingsFormValues = {
		clientID: null,
		tenantID: null,
		redirectURL: null,
		clientSecret: null
	};

	// SCIM provisioning: single global domain that serves the SCIM endpoints
	let scimDomain = '';
	let scimDomainOptions = [{ value: '', label: '— Disabled —' }];

	onMount(async () => {
		try {
			await refreshSSO();
			if (!ssoSettingsFormValues.redirectURL) {
				ssoSettingsFormValues.redirectURL = `${location.origin}/api/v1/sso/entra-id/auth`;
			}
			await refreshScimDomain();
		} finally {
			loaded = true;
		}
	});

	async function refreshScimDomain() {
		try {
			// only normal global domains may serve SCIM — exclude AiTM proxy domains
			const [current, domains] = await Promise.all([
				api.option.getScimDomain(),
				api.domain.getAllSubsetWithoutProxies({ perPage: 1000 }, null)
			]);
			if (current.success) {
				scimDomain = current.data.domain || '';
			}
			if (domains.success) {
				const names = (domains.data.rows || []).map((d) => d.name);
				scimDomainOptions = [
					{ value: '', label: '— Disabled —' },
					...names.map((n) => ({ value: n, label: n }))
				];
			}
		} catch (e) {
			console.error('failed to load SCIM domain settings', e);
		}
	}

	async function setScimDomain() {
		try {
			const res = await api.option.setScimDomain(scimDomain);
			if (res.success) {
				addToast(scimDomain ? 'SCIM domain updated' : 'SCIM serving disabled', 'Success');
			} else {
				addToast(res.error || 'Failed to update SCIM domain', 'Error');
				await refreshScimDomain();
			}
		} catch (e) {
			addToast('Failed to update SCIM domain', 'Error');
			console.error(e);
		}
	}

	async function refreshSSO() {
		try {
			const res = await api.option.get('sso_login');
			if (!res.success) {
				throw res.error;
			}
			const sso = JSON.parse(res.data.value);
			sso.clientSecret = '';
			ssoSettingsFormValues = sso;
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
		ssoSettingsFormValues = {
			clientID: null,
			tenantID: null,
			redirectURL: null,
			clientSecret: null
		};
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

	<SettingsCard title="SCIM Provisioning">
		<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
			Global domain that serves SCIM provisioning. Must be publicly reachable on 443 with a valid
			certificate; prefer a dedicated domain not used for campaigns.
		</p>
		<Form>
			<TextFieldSelect
				id="scimDomain"
				bind:value={scimDomain}
				onSelect={setScimDomain}
				options={scimDomainOptions}>SCIM domain</TextFieldSelect
			>
		</Form>
		{#if scimDomain}
			<p class="mt-3 text-xs text-gray-500 dark:text-gray-400 transition-colors duration-200">
				Base URL (each company's full endpoint is shown in its SCIM settings):
			</p>
			<p
				class="text-xs text-gray-600 dark:text-gray-300 font-mono break-all transition-colors duration-200"
			>
				https://{scimDomain}/api/v1/scim/v2/&lt;companyID&gt;
			</p>
		{/if}
	</SettingsCard>
</div>
{/if}

{#if isSSOModalVisible}
	<Modal bind:visible={isSSOModalVisible} headerText="SSO configuration" onClose={closeSSOModal}>
		<div class="mt-4">
			<div>
				<h3 class="text-xl font-semibold text-gray-700 dark:text-white">Microsoft SSO Setup</h3>
				<p class="text-gray-600 dark:text-gray-300 mb-4">
					Configure Single Sign-On with Microsoft Azure AD.
				</p>
			</div>

			<div class="bg-gray-50 dark:bg-gray-700 p-4 rounded-md">
				<p class="font-semibold text-gray-900 dark:text-white mb-2">Important:</p>
				<p class="text-sm text-gray-700 dark:text-gray-300">
					Accounts that login with SSO will no longer be able to use password login.
				</p>
			</div>
		</div>
		<FormGrid on:submit={onSubmitSSO} bind:bindTo={ssoForm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
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
				</FormColumn>
				<FormColumn>
					<TextField
						required
						type="url"
						bind:value={ssoSettingsFormValues.redirectURL}
						placeholder="https://your-domain.com/auth/callback">Redirect URL</TextField
					>

					<PasswordField
						required
						bind:value={ssoSettingsFormValues.clientSecret}
						placeholder="Enter your client secret">Client Secret</PasswordField
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
