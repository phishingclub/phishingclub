<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import Form from '$lib/components/Form.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	let loaded = false;
	let scimError = '';
	let isSaving = false;

	// SCIM provisioning: single global domain that serves the SCIM endpoints
	let scimDomain = '';
	let scimDomainOptions = [{ value: '', label: '- Disabled -' }];
	// retention window (days) before a SCIM-disabled recipient is pruned
	let scimRetentionDays = 30;

	onMount(async () => {
		try {
			await refreshScimDomain();
			await refreshScimRetention();
		} finally {
			loaded = true;
		}
	});

	async function refreshScimRetention() {
		try {
			const res = await api.option.getScimRetentionDays();
			if (res.success) {
				scimRetentionDays = res.data.days;
			}
		} catch (e) {
			console.error('failed to load SCIM retention setting', e);
		}
	}

	// save both SCIM settings together, like the other settings panels, so a
	// change is only applied when the user explicitly clicks Save
	async function saveScim() {
		scimError = '';
		const days = parseInt(scimRetentionDays, 10);
		if (isNaN(days) || days < 0) {
			scimError = 'Retention days must be zero or positive';
			return;
		}
		isSaving = true;
		try {
			const domainRes = await api.option.setScimDomain(scimDomain);
			if (!domainRes.success) {
				scimError = domainRes.error || 'Failed to update SCIM domain';
				await refreshScimDomain();
				return;
			}
			const retentionRes = await api.option.setScimRetentionDays(days);
			if (!retentionRes.success) {
				scimError = retentionRes.error || 'Failed to update SCIM retention';
				await refreshScimRetention();
				return;
			}
			addToast('SCIM settings updated', 'Success');
		} catch (e) {
			scimError = 'Failed to update SCIM settings';
			console.error('failed to update SCIM settings', e);
		} finally {
			isSaving = false;
		}
	}

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
					{ value: '', label: '- Disabled -' },
					...names.map((n) => ({ value: n, label: n }))
				];
			}
		} catch (e) {
			console.error('failed to load SCIM domain settings', e);
		}
	}

</script>

{#if !loaded}
	<SettingsLoading />
{:else}
	<div class="flex flex-wrap gap-6">
		<SettingsCard title="SCIM Provisioning">
			<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				Global domain that serves SCIM provisioning. Must be publicly reachable on 443 with a valid
				certificate; prefer a dedicated domain not used for campaigns.
			</p>
			<Form on:submit={saveScim} fullWidth>
				<TextFieldSelect
					id="scimDomain"
					bind:value={scimDomain}
					options={scimDomainOptions}>SCIM domain</TextFieldSelect
				>
				<TextField
					id="scimRetentionDays"
					type="number"
					min="0"
					width="small"
					bind:value={scimRetentionDays}
					toolTipText="Days a disabled recipient is kept before removal. 0 removes on the next prune."
					>Retention (days)</TextField
				>
				<p class="mt-2 text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
					How long deprovisioned recipients are kept (disabled) before being permanently removed.
				</p>
				<FormError message={scimError} />
				<div class="mt-6 flex justify-end">
					<FormButton size={'medium'} isSubmitting={isSaving}>Save Changes</FormButton>
				</div>
			</Form>
		</SettingsCard>
	</div>
{/if}
