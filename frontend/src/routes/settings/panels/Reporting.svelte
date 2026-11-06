<script>
	// ways to track phishing reported by users. the reporting inbox is the first one.
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import Button from '$lib/components/Button.svelte';
	import CompanyReportInboxModal from '$lib/components/modal/CompanyReportInboxModal.svelte';
	import ReportInboxStatus from '$lib/components/company/ReportInboxStatus.svelte';

	// global report inbox, only covers global campaigns. null when not configured.
	let reportInbox = null;
	let isReportInboxModalVisible = false;
	let loaded = false;

	onMount(async () => {
		showIsLoading();
		try {
			const res = await api.company.reportInbox.getByCompanyID(null);
			if (res && res.success && res.data) {
				reportInbox = res.data;
			} else if (!res || res.statusCode !== 404) {
				addToast('Failed to load reporting inbox', 'Error');
			}
		} catch (e) {
			console.error('failed to load reporting inbox', e);
		} finally {
			loaded = true;
			hideIsLoading();
		}
	});
</script>

{#if loaded}
<div class="flex flex-wrap gap-6">
	<SettingsCard title="Reporting Inbox" description="Set up automated report tracking for global campaigns with Microsoft 365.">
		<div class="mt-2">
			<ReportInboxStatus inbox={reportInbox} />
		</div>
		<svelte:fragment slot="footer">
			<Button size={'large'} on:click={() => (isReportInboxModalVisible = true)}
				>{reportInbox ? 'Manage' : 'Configure'}</Button
			>
		</svelte:fragment>
	</SettingsCard>
</div>
{/if}

<CompanyReportInboxModal
	bind:visible={isReportInboxModalVisible}
	isGlobal={true}
	on:change={(e) => (reportInbox = e.detail)}
/>
