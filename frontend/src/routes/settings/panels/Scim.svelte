<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SettingsLoading from '$lib/components/SettingsLoading.svelte';
	import Form from '$lib/components/Form.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	let loaded = false;

	// SCIM provisioning: single global domain that serves the SCIM endpoints
	let scimDomain = '';
	let scimDomainOptions = [{ value: '', label: '— Disabled —' }];
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

	async function setScimRetention() {
		try {
			const days = parseInt(scimRetentionDays, 10);
			if (isNaN(days) || days < 0) {
				addToast('Retention days must be zero or positive', 'Error');
				await refreshScimRetention();
				return;
			}
			const res = await api.option.setScimRetentionDays(days);
			if (res.success) {
				addToast('SCIM retention updated', 'Success');
			} else {
				addToast(res.error || 'Failed to update SCIM retention', 'Error');
				await refreshScimRetention();
			}
		} catch (e) {
			addToast('Failed to update SCIM retention', 'Error');
			console.error(e);
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
			<Form>
				<TextFieldSelect
					id="scimDomain"
					bind:value={scimDomain}
					onSelect={setScimDomain}
					options={scimDomainOptions}>SCIM domain</TextFieldSelect
				>
				<TextField
					id="scimRetentionDays"
					type="number"
					min="0"
					width="small"
					bind:value={scimRetentionDays}
					onBlur={setScimRetention}
					toolTipText="Days a disabled recipient is kept before removal. 0 removes on the next prune."
					>Retention (days)</TextField
				>
			</Form>
			<p class="mt-2 text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
				How long deprovisioned recipients are kept (disabled) before being permanently removed.
			</p>
		</SettingsCard>
	</div>
{/if}
