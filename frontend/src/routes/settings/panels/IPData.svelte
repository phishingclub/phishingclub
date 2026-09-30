<script>
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import Button from '$lib/components/Button.svelte';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading';

	let loaded = false;
	let packages = [];
	let busy = {};

	const meta = {
		geoip: {
			title: 'Geo IP Data',
			blurb: 'Maps visitor IPs to a country for country filters. Built in, download to refresh.'
		},
		asn: {
			title: 'ASN Data',
			blurb: 'Maps visitor IPs to their network operator for ASN filters. Download to enable.'
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
			<SettingsCard title={meta[p.kind]?.title || p.kind}>
				<div class="space-y-4">
					<p class="text-gray-600 dark:text-gray-300 text-sm transition-colors duration-200">
						{meta[p.kind]?.blurb || ''}
					</p>
					<div class="space-y-1">
						<p
							class="text-sm font-medium transition-colors duration-200"
							class:text-green-600={p.info && p.info.downloaded}
							class:dark:text-green-400={p.info && p.info.downloaded}
							class:text-gray-500={!(p.info && p.info.downloaded)}
							class:dark:text-gray-400={!(p.info && p.info.downloaded)}
						>
							{#if p.info && p.info.downloaded}
								Downloaded
							{:else if p.info}
								Built in
							{:else}
								Not downloaded
							{/if}
						</p>
						{#if p.info}
							<p class="text-sm text-gray-500 dark:text-gray-400 transition-colors duration-200">
								{#if p.info.created}{fmtDate(p.info.created)} ·
								{/if}{(p.info.ipv4Prefixes + p.info.ipv6Prefixes).toLocaleString()} prefixes
							</p>
						{/if}
						{#if p.updateAvailable}
							<p
								class="text-sm text-cta-blue dark:text-highlight-blue transition-colors duration-200"
							>
								Update available
							</p>
						{/if}
					</div>
				</div>
				<svelte:fragment slot="footer">
					<div class="flex gap-3">
						{#if p.info && p.info.downloaded}
							<Button
								size="medium"
								backgroundColor="bg-red-600"
								disabled={busy[p.kind]}
								on:click={() => remove(p.kind)}
							>
								Remove
							</Button>
						{/if}
						<Button size="medium" disabled={busy[p.kind]} on:click={() => download(p.kind)}>
							{p.info && p.info.downloaded ? 'Update' : 'Download'}
						</Button>
					</div>
				</svelte:fragment>
			</SettingsCard>
		{/each}
	</div>
{/if}
