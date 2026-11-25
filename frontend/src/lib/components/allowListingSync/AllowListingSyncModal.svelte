<script>
	import { createEventDispatcher } from 'svelte';
	import { addToast } from '$lib/store/toast';
	import { api } from '$lib/api/apiProxy.js';
	import { fetchAllRows } from '$lib/utils/api-utils.js';
	import Modal from '$lib/components/Modal.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import Note from '$lib/components/Note.svelte';
	import { guideURL } from '$lib/consts/docs';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import Button from '$lib/components/Button.svelte';
	import FormButton from '$lib/components/FormButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import FormSection from '$lib/components/form/FormSection.svelte';
	import ToolTip from '$lib/components/ToolTip.svelte';
	import AllowListingSyncStatus from './AllowListingSyncStatus.svelte';
	import {
		LEAD_PRESETS,
		GRACE_PRESETS,
		INTERVAL_PRESETS,
		allowListingScheduleMismatch,
		describeMinutes,
		minuteOptions
	} from '$lib/utils/allowListingSync.js';

	// change fires with the target, or null after delete, so the page can update
	const dispatch = createEventDispatcher();

	export let visible = false;
	// the target scope: a company id, or null for the global target
	export let companyId = null;
	// shown in the texts, empty for the global target
	export let companyName = '';

	$: isGlobal = !companyId;
	$: scopeName = isGlobal ? 'global campaigns' : companyName;

	const tabs = [
		{ id: 'status', label: 'Status' },
		{ id: 'settings', label: 'Settings' },
		{ id: 'runbook', label: 'Runbook' }
	];
	let activeTab = 'status';

	let target = null;
	let isLoading = false;
	let isSaving = false;
	let isRotating = false;
	let isDeleting = false;
	let isCancellingDelete = false;
	let formError = '';

	// token reveal, only filled right after create or rotate
	let revealedToken = '';

	// confirmation dialogs
	let isRotateAlertVisible = false;
	let isDeleteAlertVisible = false;
	let isCancelDeleteAlertVisible = false;
	// remove the values Phishing Club added before the sync is deleted
	let removeValues = true;
	// owned values still in the tenant, read when the delete dialog opens
	let ownedCount = 0;

	// runbook
	let domainOptions = [];
	let runbookDomain = '';
	let runbook = null;
	let runbookError = '';
	let isLoadingRunbook = false;
	let runbookFor = '';
	// the runner domain stored before it was changed in this window
	let replacedRunnerDomain = '';
	// the scripts shown below their step
	let showPermissionScript = false;
	let showRunbookScript = false;

	// a new target starts with the same values the server uses
	const emptyForm = () => ({
		enabled: true,
		leadMinutes: '1440',
		graceMinutes: '1440',
		intervalMinutes: '60'
	});
	let form = emptyForm();

	$: if (visible) {
		loadAll();
	}
	$: if (!visible) {
		resetState();
	}

	const resetState = () => {
		target = null;
		form = emptyForm();
		formError = '';
		revealedToken = '';
		activeTab = 'status';
		runbook = null;
		runbookError = '';
		runbookDomain = '';
		runbookFor = '';
		replacedRunnerDomain = '';
		showPermissionScript = false;
		showRunbookScript = false;
		isRotateAlertVisible = false;
		isDeleteAlertVisible = false;
		isCancelDeleteAlertVisible = false;
	};

	const applyTarget = (data) => {
		target = data;
		form = {
			enabled: !!data.enabled,
			leadMinutes: String(data.leadMinutes ?? 0),
			graceMinutes: String(data.graceMinutes ?? 0),
			intervalMinutes: String(data.intervalMinutes ?? 60)
		};
		if (data.token) {
			revealedToken = data.token;
		}
		if (!runbookDomain && data.runnerDomain && !data.runnerDomainIssue) {
			runbookDomain = data.runnerDomain;
		}
	};

	// the target without the one time token, for the page
	const publicTarget = (data) => {
		if (!data) return null;
		const { token: _, ...rest } = data;
		return rest;
	};

	const loadAll = async () => {
		isLoading = true;
		try {
			await Promise.all([loadTarget(), loadDomains()]);
		} finally {
			isLoading = false;
		}
	};

	const loadTarget = async () => {
		try {
			const res = await api.allowListingSync.get(companyId);
			if (res && res.success && res.data) {
				applyTarget(res.data);
			} else if (res && res.statusCode === 404) {
				target = null;
			} else {
				// an empty form could overwrite the stored settings on save
				addToast('Failed to load allow listing sync', 'Error');
				visible = false;
				return;
			}
		} catch (e) {
			console.error('failed to load allow listing sync', e);
			addToast('Failed to load allow listing sync', 'Error');
			visible = false;
			return;
		}
		dispatch('change', publicTarget(target));
	};

	// global domains, plus the company's own in company view. Proxy domains
	// forward to another site, so the runner can not use them
	const loadDomains = async () => {
		try {
			const rows = await fetchAllRows((options) =>
				api.domain.getAllSubsetWithoutProxies(options, companyId)
			);
			domainOptions = rows.map((d) => d.name).sort();
		} catch (e) {
			console.error('failed to load domains', e);
			domainOptions = [];
		}
	};

	// the form values as numbers for the api
	const payload = () => ({
		enabled: form.enabled,
		leadMinutes: Number(form.leadMinutes),
		graceMinutes: Number(form.graceMinutes),
		intervalMinutes: Number(form.intervalMinutes)
	});

	const onSave = async () => {
		formError = '';
		isSaving = true;
		const isCreate = !target;
		const intervalChanged =
			!isCreate && Number(form.intervalMinutes) !== Number(target.intervalMinutes);
		try {
			const res = await api.allowListingSync.upsert(companyId, payload());
			if (!res || !res.success) {
				formError = res?.error ?? 'Failed to save allow listing sync';
				return;
			}
			applyTarget(res.data);
			dispatch('change', publicTarget(target));
			addToast(
				isCreate
					? 'Allow listing sync set up'
					: intervalChanged
						? 'Allow listing sync saved. Change the schedule in Azure Automation to match.'
						: 'Allow listing sync saved',
				'Success'
			);
			if (isCreate) {
				activeTab = 'runbook';
			}
		} catch (e) {
			console.error('failed to save allow listing sync', e);
			formError = 'Failed to save allow listing sync';
		} finally {
			isSaving = false;
		}
	};

	const onConfirmRotate = async () => {
		isRotating = true;
		try {
			const res = await api.allowListingSync.rotateToken(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to rotate token', 'Error');
				return { success: false };
			}
			applyTarget(res.data);
			dispatch('change', publicTarget(target));
			addToast('Token rotated', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to rotate allow listing token', e);
			addToast('Failed to rotate token', 'Error');
			return { success: false };
		} finally {
			isRotating = false;
		}
	};

	const openDeleteAlert = async () => {
		ownedCount = 0;
		removeValues = true;
		try {
			const res = await api.allowListingSync.getEntries(companyId);
			if (res && res.success) {
				ownedCount = (res.data ?? []).filter((e) => e.state === 'owned').length;
			}
		} catch (e) {
			console.error('failed to load allow listing entries', e);
		}
		isDeleteAlertVisible = true;
	};

	const onConfirmDelete = async () => {
		isDeleting = true;
		try {
			const res = await api.allowListingSync.delete(
				companyId,
				// sent as chosen, the server deletes at once when nothing is left
				// to remove. The count shown can be stale or failed to load.
				removeValues
			);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to delete allow listing sync', 'Error');
				return { success: false };
			}
			// the target comes back when it waits for the values to be removed
			if (res.data?.id) {
				applyTarget(res.data);
				dispatch('change', publicTarget(target));
				addToast('Removing values, the sync is deleted after the next run', 'Success');
				return { success: true };
			}
			resetState();
			dispatch('change', null);
			addToast('Allow listing sync deleted', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to delete allow listing sync', e);
			addToast('Failed to delete allow listing sync', 'Error');
			return { success: false };
		} finally {
			isDeleting = false;
		}
	};

	const onConfirmCancelDelete = async () => {
		isCancellingDelete = true;
		try {
			const res = await api.allowListingSync.cancelDelete(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to cancel the delete', 'Error');
				return { success: false };
			}
			applyTarget(res.data);
			dispatch('change', publicTarget(target));
			addToast('Delete cancelled', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to cancel allow listing sync delete', e);
			addToast('Failed to cancel the delete', 'Error');
			return { success: false };
		} finally {
			isCancellingDelete = false;
		}
	};

	const loadRunbook = async (domain) => {
		if (!domain || domain === runbookFor) return;
		runbookFor = domain;
		runbookError = '';
		// the steps of the last domain stay shown until the new ones are in, so
		// the window does not shrink and grow on each pick
		isLoadingRunbook = true;
		try {
			const res = await api.allowListingSync.getRunbook(companyId, domain);
			if (runbookFor !== domain) return;
			if (!res || !res.success) {
				runbookError = res?.error ?? 'Failed to load the runbook';
				runbook = null;
				return;
			}
			runbook = res.data;
			if (target && domain !== target.runnerDomain) {
				await saveRunnerDomain(domain);
			}
		} catch (e) {
			console.error('failed to load runbook', e);
			runbookError = 'Failed to load the runbook';
			runbook = null;
		} finally {
			if (runbookFor === domain) isLoadingRunbook = false;
		}
	};

	// picking a domain saves it, the runbook downloaded after calls it
	const saveRunnerDomain = async (domain) => {
		const previous = target.runnerDomain;
		const res = await api.allowListingSync.setRunnerDomain(companyId, domain);
		if (!res || !res.success) {
			runbookError = res?.error ?? 'Failed to save the domain';
			runbook = null;
			return;
		}
		// only the domain, unsaved settings in the form stay as they are
		target = {
			...target,
			runnerDomain: res.data.runnerDomain,
			runnerDomainIssue: res.data.runnerDomainIssue
		};
		dispatch('change', publicTarget(target));
		if (previous && previous !== domain) {
			replacedRunnerDomain = replacedRunnerDomain || previous;
		}
		if (replacedRunnerDomain === domain) {
			replacedRunnerDomain = '';
		}
		addToast('Runner domain saved', 'Success');
	};

	$: runnerDomainWarning =
		target?.runnerDomainIssue === 'deleted'
			? `The runner calls ${target.runnerDomain}, which no longer exists. Pick another domain and replace the runbook in Azure Automation.`
			: target?.runnerDomainIssue === 'proxy'
				? `The runner calls ${target.runnerDomain}, which is now a proxy domain. Pick another domain and replace the runbook in Azure Automation.`
				: '';

	$: if (visible && target && runbookDomain) {
		loadRunbook(runbookDomain);
	}

	const copyToClipboard = async (text) => {
		try {
			await navigator.clipboard.writeText(text);
			addToast('Copied to clipboard', 'Success');
		} catch (e) {
			addToast('Failed to copy to clipboard', 'Error');
			console.error('failed to copy to clipboard', e);
		}
	};

	const downloadRunbook = () => {
		if (!runbook) return;
		const blob = new Blob([runbook.runbook], { type: 'text/plain;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = runbook.fileName || 'PhishingClubAllowListingSync.ps1';
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
	};

	// same format as the Datetime component
	const formatDate = (dateStr) => (dateStr ? new Date(dateStr).toLocaleString() : 'Never');

	$: isBusy = isSaving || isRotating || isDeleting || isCancellingDelete;
	$: isEnabledUnsaved = !!target && form.enabled !== !!target.enabled;
	$: leadOptions = minuteOptions(
		LEAD_PRESETS.filter((m) => m >= intervalValue),
		Number(form.leadMinutes)
	);
	$: graceOptions = minuteOptions(
		GRACE_PRESETS.filter((m) => m >= intervalValue),
		Number(form.graceMinutes)
	);
	// a value is only added or removed on a run, so the lead and keep times
	// are at least one interval. A longer interval raises them.
	$: intervalValue = Number(form.intervalMinutes);
	const raiseToInterval = (interval) => {
		const min = Number(interval);
		if (Number(form.leadMinutes) < min) form.leadMinutes = String(min);
		if (Number(form.graceMinutes) < min) form.graceMinutes = String(min);
	};
	$: raiseToInterval(form.intervalMinutes);
	$: intervalOptions = minuteOptions(INTERVAL_PRESETS, target?.intervalMinutes);
	$: isIntervalUnsaved = !!target && intervalValue !== Number(target.intervalMinutes);
	$: scheduleGap = target ? allowListingScheduleMismatch(target) : null;

	// the code block style of the ics preview in the calendar builder
	const preClass =
		'mt-2 max-h-80 text-xs font-mono text-gray-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 rounded-md p-3 overflow-auto whitespace-pre transition-colors duration-200';
	const inputClass =
		'flex-1 min-w-0 px-3 py-2 text-xs rounded-md bg-gray-50 dark:bg-gray-900/60 border border-gray-200 dark:border-gray-700/60 text-gray-700 dark:text-gray-300 font-mono focus:outline-none transition-colors duration-200';
	const copyButtonClass =
		'pc-focus px-3 py-2 text-sm rounded-md border border-gray-300 bg-white hover:bg-gray-100 text-slate-700 dark:bg-gray-700 dark:hover:bg-gray-600 dark:border-gray-500 dark:text-gray-100 transition-colors duration-200';
	// the inline code style of the settings notes, as a button when the value
	// can be copied
	const codeClass =
		'rounded bg-gray-200 px-1 font-mono text-xs text-gray-800 dark:bg-slate-700 dark:text-slate-200';
	const copyCodeClass = `pc-focus inline-flex items-center gap-1 align-baseline hover:bg-gray-300 dark:hover:bg-slate-600 transition-colors duration-200 ${codeClass}`;
	const linkClass =
		'pc-focus inline-flex items-center gap-1 rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';

	// the Automation account variables the runbook reads
	const automationVariables = [
		{ name: 'PhishingClubSyncToken', value: 'the token, encrypted' },
		{ name: 'PhishingClubOrganization', value: 'the tenant domain, such as contoso.onmicrosoft.com' }
	];
</script>

<Modal
	headerText={isGlobal ? 'Global allow listing sync' : 'Allow listing sync'}
	bind:visible
	docSlug="campaigns"
	docAnchor="allow-listing-sync"
>
	<div class="w-[780px] max-w-full py-4 sm:p-6 space-y-6" id="allow-listing-sync-modal">
		{#if isLoading}
			<div class="flex items-center justify-center py-10">
				<p class="text-gray-500 dark:text-gray-400">Loading...</p>
			</div>
		{:else if revealedToken}
			<!-- token reveal, nothing else until dismissed -->
			<div
				id="allow-listing-token-reveal"
				role="status"
				class="rounded-md border border-gray-200 dark:border-gray-700/60 bg-gray-50 dark:bg-gray-800/40 p-4 space-y-3 transition-colors duration-200"
			>
				<p class="text-sm font-semibold text-slate-700 dark:text-gray-200">
					Copy this token now. It will not be shown again.
				</p>
				<div class="flex items-center gap-2">
					<input
						type="text"
						readonly
						aria-label="Token"
						value={revealedToken}
						class="flex-1 min-w-0 px-3 py-2 text-sm rounded-md bg-white dark:bg-gray-900/60 border border-gray-300 dark:border-gray-600 text-gray-800 dark:text-gray-200 font-mono focus:outline-none transition-colors duration-200"
					/>
					<button type="button" on:click={() => copyToClipboard(revealedToken)} class={copyButtonClass}>
						Copy
					</button>
				</div>
				<p class="text-sm text-gray-500 dark:text-gray-400">
					Store it encrypted in the Automation variable
					<span class={codeClass}>PhishingClubSyncToken</span>.
				</p>
				<button
					type="button"
					class="pc-focus rounded-sm text-xs font-medium text-cta-blue dark:text-highlight-blue hover:underline"
					on:click={() => (revealedToken = '')}
				>
					I have copied the token, dismiss
				</button>
			</div>
		{:else}
			{#if target}
				<TabBar
					items={tabs}
					active={activeTab}
					label="Allow listing sync sections"
					on:select={(e) => (activeTab = e.detail)}
				/>
			{/if}

			{#if target && activeTab === 'status'}
				<div
					class="rounded-md border border-gray-200 dark:border-gray-700/60 bg-gray-50 dark:bg-gray-800/40 p-4 space-y-3"
				>
					<AllowListingSyncStatus {target} showError={false} />
					<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2 text-sm">
						<div class="flex justify-between gap-2">
							<span class="text-gray-500">Last report</span>
							<span class="text-gray-800 dark:text-gray-200">{formatDate(target.lastReportAt)}</span>
						</div>
						<div class="flex justify-between gap-2">
							<span class="text-gray-500">Last plan</span>
							<span class="text-gray-800 dark:text-gray-200">{formatDate(target.lastPlanAt)}</span>
						</div>
						<div class="flex justify-between gap-2">
							<span class="text-gray-500">Token prefix</span>
							<span class="text-gray-800 dark:text-gray-200 font-mono"
								>{target.tokenPrefix ? target.tokenPrefix + '...' : '-'}</span
							>
						</div>
						<div class="flex items-center justify-between gap-2">
							<span class="flex items-center text-gray-500">
								Removed in tenant
								<ToolTip>Values Phishing Club added that someone removed in the tenant.</ToolTip>
							</span>
							<span class="text-gray-800 dark:text-gray-200">{target.driftCount ?? 0}</span>
						</div>
					</div>
					{#if target.lastError}
						<div
							class="rounded-md border border-red-300 dark:border-red-500/60 bg-red-50 dark:bg-red-900/20 p-3"
						>
							<p class="text-sm text-red-700 dark:text-red-400 break-words">
								<span class="font-semibold">Last error:</span>
								{target.lastError}
								{#if target.failureCount > 1}
									<span class="text-xs">({target.failureCount} failures in a row)</span>
								{/if}
							</p>
						</div>
					{/if}
					{#if target.lastNote}
						<p class="text-sm text-gray-600 dark:text-gray-300 break-words">
							<span class="font-semibold">Last report:</span>
							{target.lastNote}
						</p>
					{/if}
				</div>

				{#if runnerDomainWarning}
					<Note icon={false} boxed role="status">{runnerDomainWarning}</Note>
				{/if}
				{#if scheduleGap}
					<Note icon={false} boxed role="status">
						The last two reports came {describeMinutes(scheduleGap)} apart, the sync is set to run
						every {describeMinutes(target.intervalMinutes).replace(/^1 /, '')}. Check the schedule of
						the runbook in Azure Automation.
					</Note>
				{/if}

				{#if target.deleteRequestedAt}
					<Note icon={false} boxed role="status">
						Deleted on {formatDate(target.deleteRequestedAt)}. The next sync removes the values
						Phishing Club added, then the sync deletes itself.
					</Note>
				{/if}

				<div
					class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex flex-wrap gap-3 justify-end"
				>
					<Button
						size="auto"
						variant="secondary"
						css="whitespace-nowrap"
						disabled={isBusy}
						on:click={() => (isRotateAlertVisible = true)}
					>
						Rotate Token
					</Button>
					{#if target.deleteRequestedAt}
						<Button
							size="auto"
							variant="secondary"
							css="whitespace-nowrap"
							disabled={isBusy}
							on:click={() => (isCancelDeleteAlertVisible = true)}
						>
							Cancel Delete
						</Button>
					{:else}
						<Button
							size="auto"
							variant="danger-outline"
							css="whitespace-nowrap"
							disabled={isBusy}
							on:click={openDeleteAlert}
						>
							Delete
						</Button>
					{/if}
				</div>
			{:else if !target || activeTab === 'settings'}
				<div id="allow-listing-settings">
					<!-- enabled toggle -->
					<div class="flex items-center justify-between mb-6">
						<div>
							<p class="text-sm font-medium text-gray-700 dark:text-gray-300">Enabled</p>
							{#if isEnabledUnsaved}
								<p
									id="allow-listing-enabled-hint"
									class="text-xs text-gray-500 dark:text-gray-400 mt-0.5"
								>
									Click Save to {form.enabled ? 'turn on' : 'turn off'} the sync.
								</p>
							{/if}
						</div>
						<button
							type="button"
							role="switch"
							aria-checked={form.enabled}
							aria-label="Enabled"
							aria-describedby={isEnabledUnsaved ? 'allow-listing-enabled-hint' : undefined}
							disabled={isBusy}
							on:click={() => (form.enabled = !form.enabled)}
							class="pc-focus relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed
								{form.enabled ? 'bg-cta-blue dark:bg-highlight-blue/80' : 'bg-gray-300 dark:bg-gray-600'}"
						>
							<span
								class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200
									{form.enabled ? 'translate-x-5' : 'translate-x-0'}"
							/>
						</button>
					</div>

					<FormSection title="Timing">
						<div class="grid grid-cols-1 sm:grid-cols-3 gap-x-4">
							<TextFieldSelect
								id="allow-listing-interval"
								size="full"
								containInModal
								bind:value={form.intervalMinutes}
								options={intervalOptions}
								toolTipText="How often the schedule in Azure Automation runs the runbook."
								>Sync every</TextFieldSelect
							>
							<TextFieldSelect
								id="allow-listing-lead"
								size="full"
								containInModal
								bind:value={form.leadMinutes}
								options={leadOptions}>Add before start</TextFieldSelect
							>
							<TextFieldSelect
								id="allow-listing-grace"
								size="full"
								containInModal
								bind:value={form.graceMinutes}
								options={graceOptions}>Keep after close</TextFieldSelect
							>
						</div>
						{#if isIntervalUnsaved}
							<div class="mt-3">
								<Note icon={false} boxed role="status">
									After saving, change the runbook schedule in Azure Automation to every
									{describeMinutes(intervalValue).replace(/^1 /, '')}.
								</Note>
							</div>
						{/if}
					</FormSection>

					<FormError message={formError} />

					<div
						class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex gap-3 justify-end items-center"
					>
						<FormButton size="medium" isSubmitting={isSaving} disabled={isBusy} on:click={onSave}
							>{target ? 'Save' : 'Set up'}</FormButton
						>
					</div>
				</div>
			{:else if activeTab === 'runbook'}
				<div class="space-y-6" id="allow-listing-runbook">
					<div class="space-y-3">
						<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-4 items-end">
							<TextFieldSelect
								id="allow-listing-runbook-domain"
								size="full"
								containInModal
								placeholder={domainOptions.length ? 'Select a domain' : 'No domains'}
								bind:value={runbookDomain}
								options={domainOptions}
								toolTipText="Needs a valid HTTPS certificate.">Domain the runner calls</TextFieldSelect
							>
						</div>
						{#if runnerDomainWarning}
							<Note icon={false} boxed role="status">{runnerDomainWarning}</Note>
						{:else if replacedRunnerDomain}
							<Note icon={false} boxed role="status">
								The domain is written into the runbook script, and the runner is only answered on
								this domain. The runbook in Azure Automation that calls
								<span class="whitespace-nowrap">{replacedRunnerDomain}</span> stops working, replace it
								with the new download.
							</Note>
						{/if}
					</div>

					{#if isLoadingRunbook && !runbook}
						<p class="text-sm text-gray-500 dark:text-gray-400">Loading...</p>
					{:else if runbookError}
						<FormError message={runbookError} />
					{:else if runbook}
						<div class="space-y-1">
							<p class="text-xs font-semibold text-gray-500 dark:text-gray-400">Runner URL</p>
							<div class="flex items-center gap-2">
								<input
									type="text"
									readonly
									aria-label="Runner URL"
									value={runbook.runnerURL}
									class={inputClass}
								/>
								<button
									type="button"
									on:click={() => copyToClipboard(runbook.runnerURL)}
									class={copyButtonClass}
								>
									Copy
								</button>
							</div>
						</div>

						<FormSection title="Setup in Azure Automation">
							<ol
								class="list-decimal pl-5 space-y-4 text-sm text-gray-700 dark:text-gray-300 marker:font-semibold marker:text-gray-500 dark:marker:text-gray-400"
								data-allow-listing-setup
							>
								<li class="pl-1">
									Create an Automation account and turn on its system assigned managed identity.
								</li>
								<li class="pl-1">
									Create a runtime environment on PowerShell 7.4 and add the module
									<button
										type="button"
										class={copyCodeClass}
										title="Copy"
										on:click={() => copyToClipboard(runbook.moduleName)}
										>{runbook.moduleName}<svg
											class="h-3 w-3 shrink-0 text-gray-500 dark:text-gray-400"
											viewBox="0 0 24 24"
											fill="none"
											stroke="currentColor"
											stroke-width="2"
											stroke-linecap="round"
											stroke-linejoin="round"
											aria-hidden="true"
											><rect x="9" y="9" width="11" height="11" rx="1.5" /><path
												d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5"
											/></svg
										></button
									>
									version <span class={codeClass}>{runbook.moduleVersion}</span>, plus
									<span class={codeClass}>PowerShellGet</span> and
									<span class={codeClass}>PackageManagement</span>.
								</li>
								<li class="pl-1">
									<p>
										As a Global Administrator or Privileged Role Administrator, run this script in
										Microsoft Graph PowerShell to give the managed identity its permissions. Replace
										<span class={codeClass}>NAME OF THE AUTOMATION ACCOUNT</span> first.
									</p>
									<div class="mt-2 flex flex-wrap items-center gap-3">
										<button
											type="button"
											class={copyButtonClass}
											on:click={() => copyToClipboard(runbook.permissionScript)}>Copy</button
										>
										<button
											type="button"
											class={linkClass}
											aria-expanded={showPermissionScript}
											on:click={() => (showPermissionScript = !showPermissionScript)}
										>
											{showPermissionScript ? 'Hide script' : 'Show script'}
											<svg
												class="w-4 h-4 transition-transform duration-200"
												class:rotate-180={showPermissionScript}
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
												aria-hidden="true"
												><path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M19 9l-7 7-7-7"
												/></svg
											>
										</button>
									</div>
									{#if showPermissionScript}
										<pre class={preClass}>{runbook.permissionScript}</pre>
									{/if}
								</li>
								<li class="pl-1">
									<p>Add these variables to the Automation account:</p>
									<dl class="mt-2 grid grid-cols-1 sm:grid-cols-[auto_1fr] gap-x-4 gap-y-2">
										{#each automationVariables as v}
											<dt>
												<button
													type="button"
													class={copyCodeClass}
													title="Copy"
													on:click={() => copyToClipboard(v.name)}
													>{v.name}<svg
														class="h-3 w-3 shrink-0 text-gray-500 dark:text-gray-400"
														viewBox="0 0 24 24"
														fill="none"
														stroke="currentColor"
														stroke-width="2"
														stroke-linecap="round"
														stroke-linejoin="round"
														aria-hidden="true"
														><rect x="9" y="9" width="11" height="11" rx="1.5" /><path
															d="M5 15H4.5A1.5 1.5 0 0 1 3 13.5v-9A1.5 1.5 0 0 1 4.5 3h9A1.5 1.5 0 0 1 15 4.5V5"
														/></svg
													></button
												>
											</dt>
											<dd class="text-gray-500 dark:text-gray-400">{v.value}</dd>
										{/each}
									</dl>
								</li>
								<li class="pl-1">
									<p>
										Create a PowerShell runbook in that runtime environment from the script,
										publish it and link it to a
										schedule that runs <span class="font-semibold">{runbook.schedule}</span>.
									</p>
									<div class="mt-2 flex flex-wrap items-center gap-3">
										<Button type="button" size="auto" on:click={downloadRunbook}>Download</Button>
										<button
											type="button"
											class={copyButtonClass}
											on:click={() => copyToClipboard(runbook.runbook)}>Copy</button
										>
										<button
											type="button"
											class={linkClass}
											aria-expanded={showRunbookScript}
											on:click={() => (showRunbookScript = !showRunbookScript)}
										>
											{showRunbookScript ? 'Hide script' : 'Show script'}
											<svg
												class="w-4 h-4 transition-transform duration-200"
												class:rotate-180={showRunbookScript}
												fill="none"
												stroke="currentColor"
												viewBox="0 0 24 24"
												aria-hidden="true"
												><path
													stroke-linecap="round"
													stroke-linejoin="round"
													stroke-width="2"
													d="M19 9l-7 7-7-7"
												/></svg
											>
										</button>
									</div>
									{#if showRunbookScript}
										<pre class="{preClass} max-h-96" data-allow-listing-runbook>{runbook.runbook}</pre>
									{/if}
								</li>
								<li class="pl-1">
									Optional: set <span class={codeClass}>$AllowedSenderIPs</span> and
									<span class={codeClass}>$AllowedDomainSuffixes</span> at the top of the runbook to limit
									what it may add.
								</li>
							</ol>
							<div class="pt-4">
								<a
									class={linkClass}
									href={guideURL('campaigns', 'allow-listing-sync-setup')}
									target="_blank"
									rel="noopener noreferrer">Full setup guide</a
								>
							</div>
						</FormSection>
					{:else}
						<p class="text-sm text-gray-500 dark:text-gray-400">
							Pick a domain to get the runbook.
						</p>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
</Modal>

<Alert
	headline="Rotate Allow Listing Sync Token"
	bind:visible={isRotateAlertVisible}
	onConfirm={onConfirmRotate}
	verification="confirm"
>
	<p>
		Are you sure you want to rotate the allow listing sync token for
		<strong>{scopeName}</strong>?
	</p>
	<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
		The current token stops working at once. Put the new token in the Automation account before the
		next run.
	</p>
</Alert>

<Alert
	headline="Cancel Delete"
	bind:visible={isCancelDeleteAlertVisible}
	onConfirm={onConfirmCancelDelete}
	tone="affirmative"
>
	<p>
		Are you sure you want to cancel the delete of the allow listing sync for
		<strong>{scopeName}</strong>?
	</p>
	<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
		The sync is kept and the next sync adds the values that are needed again.
	</p>
</Alert>

<Alert
	headline="Delete Allow Listing Sync"
	bind:visible={isDeleteAlertVisible}
	onConfirm={onConfirmDelete}
	verification="confirm"
>
	<p>
		Are you sure you want to delete the allow listing sync for
		<strong>{scopeName}</strong>?
	</p>
	<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
		The token stops working and the managed entries and pins are deleted.
	</p>
	{#if ownedCount > 0}
		<div class="mt-4 space-y-2">
			<CheckboxField bind:value={removeValues} id="allow-listing-remove-values" inline>
				Remove the {ownedCount}
				{ownedCount === 1 ? 'value' : 'values'} Phishing Club added from the tenant first
			</CheckboxField>
			<p class="text-sm text-gray-500 dark:text-gray-400">
				{#if removeValues}
					The sync is deleted after the next run has removed them.
				{:else}
					The sync is deleted now and the values stay in the tenant.
				{/if}
			</p>
		</div>
	{/if}
</Alert>
