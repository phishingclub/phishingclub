<script>
	// The allow listing sync of one target: the card that opens its settings,
	// what the next sync does, the values it keeps track of and the pins.
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import Button from '$lib/components/Button.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellLink from '$lib/components/table/TableCellLink.svelte';
	import TableCellCheck from '$lib/components/table/TableCellCheck.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import AllowListingSyncStatus from './AllowListingSyncStatus.svelte';
	import AllowListingSyncModal from './AllowListingSyncModal.svelte';
	import AllowListingPinModal from './AllowListingPinModal.svelte';
	import { kindLabel, allowListingStatus } from '$lib/utils/allowListingSync.js';

	// the target scope: a company id, or null for the global target
	export let companyId = null;
	// shown in the texts, empty for the global target
	export let companyName = '';

	// the most pins one target holds
	const MAX_PINS = 50;

	let target = null;
	let loaded = false;
	let entries = [];
	let preview = { add: [], remove: [], sources: [], sourcesTotal: 0 };
	let pins = [];
	let isLoadingDetails = false;
	let isModalVisible = false;
	let isPinModalVisible = false;
	let isDeletePinVisible = false;
	let pinToDelete = null;

	// loads again when the page moves to another company
	// a value no company id equals, so the first run always loads, also for
	// the global target where the id is empty
	let loadedFor = {};
	$: if (companyId !== loadedFor) {
		loadedFor = companyId;
		target = null;
		entries = [];
		pins = [];
		preview = { add: [], remove: [], sources: [], sourcesTotal: 0 };
		loadTarget();
	}

	// an answer for a company the page has moved away from is dropped
	const loadTarget = async () => {
		const forCompany = companyId;
		try {
			const res = await api.allowListingSync.get(forCompany);
			if (forCompany !== companyId) return;
			if (res && res.success && res.data) {
				target = res.data;
			} else if (res && res.statusCode === 404) {
				target = null;
			} else {
				addToast('Failed to load allow listing sync', 'Error');
			}
		} catch (e) {
			console.error('failed to load allow listing sync', e);
		} finally {
			loaded = true;
		}
		if (target) {
			loadDetails();
		}
	};

	const byKindValue = (a, b) => a.kind.localeCompare(b.kind) || a.value.localeCompare(b.value);

	const loadDetails = async () => {
		const forCompany = companyId;
		isLoadingDetails = true;
		try {
			const [entriesRes, previewRes, pinsRes] = await Promise.all([
				api.allowListingSync.getEntries(forCompany),
				api.allowListingSync.getPreview(forCompany),
				api.allowListingSync.getPins(forCompany)
			]);
			if (forCompany !== companyId) return;
			if (entriesRes.success) {
				entries = [...(entriesRes.data ?? [])].sort(byKindValue);
			}
			if (previewRes.success) {
				preview = {
					add: previewRes.data?.add ?? [],
					remove: previewRes.data?.remove ?? [],
					sources: previewRes.data?.sources ?? [],
					sourcesTotal: previewRes.data?.sourcesTotal ?? 0
				};
			}
			if (pinsRes.success) {
				pins = pinsRes.data ?? [];
			}
			if (!entriesRes.success || !previewRes.success || !pinsRes.success) {
				addToast('Failed to load allow listing sync details', 'Error');
			}
		} catch (e) {
			console.error('failed to load allow listing sync details', e);
			addToast('Failed to load allow listing sync details', 'Error');
		} finally {
			isLoadingDetails = false;
		}
	};

	const onTargetChange = (next) => {
		const hadTarget = !!target;
		target = next;
		if (next) {
			loadDetails();
		} else if (hadTarget) {
			entries = [];
			pins = [];
			preview = { add: [], remove: [], sources: [], sourcesTotal: 0 };
		}
	};

	const openDeletePin = (pin) => {
		pinToDelete = pin;
		isDeletePinVisible = true;
	};

	const onConfirmDeletePin = async () => {
		const res = await api.allowListingSync.deletePin(companyId, pinToDelete.id);
		if (res.success) {
			addToast('Pin deleted', 'Success');
			loadDetails();
		}
		return res;
	};

	// adds first, then removes
	$: changes = [
		...preview.add.map((v) => ({ ...v, change: 'add' })),
		...preview.remove.map((v) => ({ ...v, change: 'remove' }))
	];

	const sourceHref = (s) =>
		s.type === 'campaign' ? `/campaign/${s.id}` : s.type === 'program' ? `/program/${s.id}` : '';
	const sourceTypes = { campaign: 'Campaign', program: 'Program', pin: 'Pin' };
	const valuesText = (values) =>
		(values ?? []).map((v) => `${kindLabel(v.kind)}: ${v.value}`).join('\n');

	const pinName = (p) => (p.campaignTemplateID ? p.campaignTemplateName || 'Template' : p.value);
	const pinKind = (p) => (p.campaignTemplateID ? 'Template' : kindLabel(p.kind));
</script>

<div class="flex flex-wrap gap-6">
	<slot />
	<SettingsCard
		title="Allow Listing Sync"
		description={companyId
			? 'Keep the Microsoft Defender Advanced Delivery lists of the tenant in sync with the campaigns of this company.'
			: 'Keep the Microsoft Defender Advanced Delivery lists of a tenant in sync with the global campaigns.'}
	>
		{#if loaded}
			<div class="mt-2" id="allow-listing-sync-card">
				<AllowListingSyncStatus {target} />
			</div>
		{/if}
		<svelte:fragment slot="footer">
			<FormButton size="medium" on:click={() => (isModalVisible = true)}
				>{target ? 'Manage' : 'Configure'}</FormButton
			>
		</svelte:fragment>
	</SettingsCard>
</div>

{#if target}
	<div class="mt-8" id="allow-listing-next-sync">
		<SubHeadline>Next sync</SubHeadline>
		<p class="text-gray-600 dark:text-gray-300 text-sm mt-1 mb-4">
			What the runner adds and removes on its next run.
		</p>
		<Table
			columns={[
				{ column: 'Change', size: 'small' },
				{ column: 'Kind', size: 'medium' },
				{ column: 'Value', size: 'large' }
			]}
			hasData={!!changes.length}
			hasNextPage={false}
			plural="changes"
			isGhost={isLoadingDetails}
			hasActions={false}
		>
			{#each changes as c (c.change + c.kind + c.value)}
				<TableRow>
					<TableCell>
						<div>
							<StatusBadge tone={c.change === 'add' ? 'info' : 'warning'}
								>{c.change === 'add' ? 'Add' : 'Remove'}</StatusBadge
							>
						</div>
					</TableCell>
					<TableCell value={kindLabel(c.kind)} />
					<TableCell value={c.value} />
				</TableRow>
			{/each}
		</Table>
	</div>

	<div class="mt-4" id="allow-listing-sources">
		<SubHeadline>Needed by</SubHeadline>
		<p class="text-gray-600 dark:text-gray-300 text-sm mt-1 mb-4">
			Campaigns and programs that are scheduled soon, running, paused or closed within the keep
			after close time, and pins.
			{#if preview.sourcesTotal > preview.sources.length}
				Showing the first {preview.sources.length} of {preview.sourcesTotal}.
			{/if}
		</p>
		<Table
			columns={[
				{ column: 'Name', size: 'large' },
				{ column: 'Type', size: 'small' },
				{ column: 'Status', size: 'medium' },
				{ column: 'Values', size: 'small' }
			]}
			hasData={!!preview.sources.length}
			hasNextPage={false}
			plural="campaigns, programs or pins"
			isGhost={isLoadingDetails}
			hasActions={false}
		>
			{#each preview.sources as s (s.type + s.id)}
				{@const st = allowListingStatus(s.status)}
				{@const href = sourceHref(s)}
				<TableRow>
					{#if href}
						<TableCellLink {href} title={s.name} wrap>{s.name}</TableCellLink>
					{:else}
						<TableCell value={s.name} wrap />
					{/if}
					<TableCell value={sourceTypes[s.type] ?? s.type} />
					<TableCell>
						<div>
							<StatusBadge tone={st.tone} title={(s.incomplete ?? []).join('\n')}
								>{st.label}</StatusBadge
							>
						</div>
					</TableCell>
					<TableCell>
						<span title={valuesText(s.values)}>{(s.values ?? []).length}</span>
					</TableCell>
				</TableRow>
			{/each}
		</Table>
	</div>

	<div class="mt-4" id="allow-listing-entries">
		<SubHeadline>Managed entries</SubHeadline>
		<p class="text-gray-600 dark:text-gray-300 text-sm mt-1 mb-4">
			Values in the tenant lists from the last report. Owned values were added by Phishing Club and
			are removed when nothing needs them. Tenant values were there already and are never removed.
		</p>
		<Table
			columns={[
				{ column: 'Kind', size: 'medium' },
				{ column: 'Value', size: 'large' },
				{ column: 'State', size: 'small' },
				{ column: 'Needed', size: 'small', alignText: 'center' },
				{ column: 'Last seen', size: 'small' },
				{ column: 'Removed after', size: 'medium' }
			]}
			hasData={!!entries.length}
			hasNextPage={false}
			plural="entries"
			isGhost={isLoadingDetails}
			hasActions={false}
		>
			{#each entries as e (e.id)}
				<TableRow>
					<TableCell value={kindLabel(e.kind)} />
					<TableCell value={e.value} />
					<TableCell>
						<div>
							{#if e.state === 'owned'}
								<StatusBadge
									tone="info"
									title="Added by Phishing Club, removed when nothing needs it">Owned</StatusBadge
								>
							{:else}
								<StatusBadge tone="neutral" title="Already in the tenant, never removed"
									>Tenant</StatusBadge
								>
							{/if}
						</div>
					</TableCell>
					<TableCellCheck value={e.needed} />
					<TableCell value={e.lastSeenAt} isDate isRelative />
					{#if e.kept}
						<TableCell value="Kept, the rule needs one" />
					{:else}
						<TableCell
							value={e.state === 'owned' && !e.needed ? e.removeAfter : ''}
							isDate
							isRelative
						/>
					{/if}
				</TableRow>
			{/each}
		</Table>
	</div>

	<div class="mt-4" id="allow-listing-pins">
		<SubHeadline>Pins</SubHeadline>
		<p class="text-gray-600 dark:text-gray-300 text-sm mt-1 mb-4">
			Templates and values that are kept in the tenant even when no campaign needs them.
		</p>
		<Table
			columns={[
				{ column: 'Pinned', size: 'large' },
				{ column: 'Kind', size: 'medium' },
				{ column: 'Note', size: 'large' },
				{ column: 'Expires', size: 'medium' },
				{ column: 'Added', size: 'small' }
			]}
			hasData={!!pins.length}
			hasNextPage={false}
			plural="pins"
			isGhost={isLoadingDetails}
		>
			<svelte:fragment slot="toolbar">
				<Button
					variant="secondary"
					size="medium"
					disabled={pins.length >= MAX_PINS}
					title={pins.length >= MAX_PINS ? `A sync holds at most ${MAX_PINS} pins` : undefined}
					on:click={() => (isPinModalVisible = true)}>Add pin</Button
				>
			</svelte:fragment>
			{#each pins as p (p.id)}
				<TableRow>
					<TableCell value={pinName(p)} wrap />
					<TableCell value={pinKind(p)} />
					<TableCell value={p.note} />
					<TableCell value={p.expiresAt} isDate />
					<TableCell value={p.createdAt} isDate isRelative />
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableDeleteButton on:click={() => openDeletePin(p)} />
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	</div>
{/if}

<AllowListingSyncModal
	bind:visible={isModalVisible}
	{companyId}
	{companyName}
	on:change={(e) => onTargetChange(e.detail)}
/>
<AllowListingPinModal bind:visible={isPinModalVisible} {companyId} on:created={loadDetails} />
<DeleteAlert
	name={pinToDelete ? pinName(pinToDelete) : ''}
	list={['The pinned values are removed on a later sync when nothing else needs them']}
	permanent={false}
	onClick={onConfirmDeletePin}
	bind:isVisible={isDeletePinVisible}
/>
