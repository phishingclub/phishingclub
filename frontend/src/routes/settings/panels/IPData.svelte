<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Button from '$lib/components/Button.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading';

	let loaded = false;
	let packages = [];
	let busy = {};

	const meta = {
		geoip: {
			title: 'Geo IP Data',
			blurb: 'Maps visitor IPs to a country for country filters.',
			hint: 'Built in, download to refresh.'
		},
		asn: {
			title: 'ASN Data',
			blurb: 'Maps visitor IPs to their network operator for ASN filters.',
			hint: 'Download to enable.'
		}
	};

	const refresh = async () => {
		try {
			const res = await api.ipdata.status();
			if (res.success && res.data) {
				packages = res.data.packages || [];
			}
		} catch (e) {
			console.error('failed to load ip data status', e);
		}
	};

	onMount(async () => {
		showIsLoading();
		try {
			await refresh();
		} finally {
			loaded = true;
			hideIsLoading();
		}
	});

	const download = async (kind) => {
		busy = { ...busy, [kind]: true };
		showIsLoading();
		try {
			const res = await api.ipdata.download(kind);
			if (res.success) {
				addToast('Downloaded latest data', 'Success');
				await refresh();
			} else {
				addToast(res.error || 'Failed to download data', 'Error');
			}
		} catch (e) {
			addToast('Failed to download data', 'Error');
			console.error('failed to download ip data', e);
		} finally {
			busy = { ...busy, [kind]: false };
			hideIsLoading();
		}
	};

	const fmtDate = (s) => {
		if (!s) return '';
		try {
			return new Date(s).toLocaleDateString();
		} catch (_) {
			return s;
		}
	};

	const remove = async (kind) => {
		busy = { ...busy, [kind]: true };
		showIsLoading();
		try {
			const res = await api.ipdata.remove(kind);
			if (res.success) {
				addToast('Removed downloaded data', 'Success');
				await refresh();
			} else {
				addToast(res.error || 'Failed to remove data', 'Error');
			}
		} catch (e) {
			addToast('Failed to remove data', 'Error');
			console.error('failed to remove ip data', e);
		} finally {
			busy = { ...busy, [kind]: false };
			hideIsLoading();
		}
	};
</script>

{#if loaded}
	<div class="flex flex-wrap gap-6">
		{#each packages as p (p.kind)}
			<SettingsCard
				title={meta[p.kind]?.title || p.kind}
				description={[
					meta[p.kind]?.blurb || '',
					!(p.info && p.info.downloaded) && meta[p.kind]?.hint ? meta[p.kind].hint : ''
				]
					.filter(Boolean)
					.join(' ')}
			>
				<div class="mt-2 space-y-4">
					<div class="space-y-1">
						<div class="flex flex-wrap items-center gap-2">
							<span class="text-sm text-gray-500 dark:text-gray-400">Status</span>
							{#if p.info && p.info.downloaded}
								<StatusBadge tone="success">Downloaded</StatusBadge>
							{:else if p.info}
								<StatusBadge tone="warning">Built in</StatusBadge>
							{:else}
								<StatusBadge tone="neutral">Not downloaded</StatusBadge>
							{/if}
							{#if p.updateAvailable}
								<StatusBadge tone="info">Update available</StatusBadge>
							{/if}
						</div>
						{#if p.info}
							<p class="text-sm text-gray-500 dark:text-gray-400 transition-colors duration-200">
								{#if p.info.created}{fmtDate(p.info.created)} ·
								{/if}{(p.info.ipv4Prefixes + p.info.ipv6Prefixes).toLocaleString()} prefixes
							</p>
						{/if}
					</div>
					{#if p.kind === 'geoip' && !(p.info && p.info.downloaded)}
						<div
							class="rounded-md border-l-4 border-amber-500 bg-amber-50 dark:bg-amber-900/20 px-3 py-2 transition-colors duration-200"
						>
							<p class="text-sm text-amber-800 dark:text-amber-200">
								The built in Geo IP data is deprecated and will be removed in a future release.
								Download it to keep country filters working.
							</p>
						</div>
					{/if}
				</div>
				<svelte:fragment slot="footer">
					<div class="flex gap-3">
						{#if p.info && p.info.downloaded}
							<Button
								size="large"
								variant="danger-outline"
								disabled={busy[p.kind]}
								on:click={() => remove(p.kind)}
							>
								Remove
							</Button>
						{/if}
						<Button
							size="large"
							disabled={busy[p.kind]}
							on:click={() => download(p.kind)}
						>
							{p.info && p.info.downloaded ? 'Update' : 'Download'}
						</Button>
					</div>
				</svelte:fragment>
			</SettingsCard>
		{/each}
	</div>
{/if}
