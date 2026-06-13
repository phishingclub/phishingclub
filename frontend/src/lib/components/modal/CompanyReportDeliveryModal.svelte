<script>
	import { addToast } from '$lib/store/toast';
	import Modal from '../Modal.svelte';
	import Alert from '../Alert.svelte';
	import TextField from '../TextField.svelte';
	import TextFieldSelect from '../TextFieldSelect.svelte';
	import FormButton from '../FormButton.svelte';
	import SimpleCodeEditor from '../editor/SimpleCodeEditor.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { fetchAllRows } from '$lib/utils/api-utils.js';

	// external
	export let visible = false;
	/** @type {{ id: string, name: string } | null} */
	export let company = null;
	// when true the modal edits the global default config (no company)
	export let isGlobal = false;

	// the scope id passed to the api: a company id, or null for the global default
	$: companyId = isGlobal ? null : (company?.id ?? null);
	$: ready = isGlobal || !!company;

	// defaults used by the server when no subject or body is set; mirrored here so
	// the editor shows what will actually be sent. keep in sync with the backend
	// defaultReportEmailSubject and defaultReportEmailBody constants.
	const DEFAULT_EMAIL_SUBJECT = 'Campaign report: {{.CampaignName}}';
	const DEFAULT_EMAIL_BODY =
		'<p>The phishing simulation report for <strong>{{.CampaignName}}</strong> is attached.</p>';

	// local state
	let config = null;
	let pdfEnabled = false;
	let recipientGroupOptions = [];
	let smtpOptions = [];
	// full smtp rows kept so the From hint can show host and username
	let smtpByID = {};

	let isLoading = false;
	let isSaving = false;
	let isDeleting = false;
	let isDeleteAlertVisible = false;

	// form values
	let form = {
		enabled: false,
		sendOnFinish: false,
		recipientGroupID: '',
		smtpConfigurationID: '',
		senderEmail: '',
		emailSubject: '',
		emailBody: ''
	};

	// reactive: reload when modal opens
	$: {
		if (visible && ready) {
			loadAll();
		}
	}

	// reactive: clean up when modal closes
	$: {
		if (!visible) {
			resetState();
		}
	}

	const resetState = () => {
		config = null;
		isDeleteAlertVisible = false;
		form = {
			enabled: false,
			sendOnFinish: false,
			recipientGroupID: '',
			smtpConfigurationID: '',
			senderEmail: '',
			emailSubject: '',
			emailBody: ''
		};
	};

	const loadAll = async () => {
		isLoading = true;
		try {
			await Promise.all([loadPdfEnabled(), loadConfig(), loadGroups(), loadSmtp()]);
		} finally {
			isLoading = false;
		}
	};

	const loadPdfEnabled = async () => {
		try {
			const res = await api.option.get('report_pdf_enabled');
			pdfEnabled = res.success && res.data?.value === 'true';
		} catch (_) {
			pdfEnabled = false;
		}
	};

	const loadConfig = async () => {
		try {
			const res = await api.company.reportConfig.getByCompanyID(companyId);
			if (res && res.success && res.data) {
				config = res.data;
				form = {
					enabled: !!config.enabled,
					sendOnFinish: !!config.sendOnFinish,
					recipientGroupID: config.recipientGroupID ?? '',
					smtpConfigurationID: config.smtpConfigurationID ?? '',
					senderEmail: config.senderEmail ?? '',
					emailSubject: config.emailSubject ?? '',
					emailBody: config.emailBody ?? ''
				};
			} else {
				config = null;
			}
		} catch (e) {
			console.error('failed to load report config', e);
			config = null;
		}
		// on the global default, show the built-in defaults when nothing is set so
		// the user can see and edit what will be used. for a company, leave the
		// fields empty so they inherit the global default when unset.
		if (isGlobal) {
			if (!form.emailSubject) {
				form.emailSubject = DEFAULT_EMAIL_SUBJECT;
			}
			if (!form.emailBody) {
				form.emailBody = DEFAULT_EMAIL_BODY;
			}
		}
	};

	const loadGroups = async () => {
		try {
			const groups = await fetchAllRows((options) => {
				return api.recipient.getAllGroups(options, companyId);
			});
			recipientGroupOptions = groups.map((g) => ({ value: g.id, label: g.name }));
		} catch (e) {
			console.error('failed to load recipient groups', e);
			recipientGroupOptions = [];
		}
	};

	const loadSmtp = async () => {
		try {
			const configs = await fetchAllRows((options) => {
				return api.smtpConfiguration.getAll(options, companyId);
			});
			smtpOptions = configs.map((s) => ({ value: s.id, label: s.name }));
			smtpByID = configs.reduce((acc, s) => {
				acc[s.id] = s;
				return acc;
			}, {});
		} catch (e) {
			console.error('failed to load smtp configurations', e);
			smtpOptions = [];
			smtpByID = {};
		}
	};

	const onSave = async () => {
		isSaving = true;
		try {
			const res = await api.company.reportConfig.upsert(companyId, {
				enabled: form.enabled,
				sendOnFinish: form.sendOnFinish,
				recipientGroupID: form.recipientGroupID || null,
				smtpConfigurationID: form.smtpConfigurationID || null,
				senderEmail: form.senderEmail || null,
				emailSubject: form.emailSubject || '',
				emailBody: form.emailBody || ''
			});
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to save report delivery', 'Error');
				return;
			}
			config = res.data;
			addToast('Report delivery saved', 'Success');
			visible = false;
		} catch (e) {
			console.error('failed to save report config', e);
			addToast('Failed to save report delivery', 'Error');
		} finally {
			isSaving = false;
		}
	};

	const onConfirmDelete = async () => {
		isDeleting = true;
		try {
			const res = await api.company.reportConfig.delete(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to delete report delivery', 'Error');
				return { success: false };
			}
			resetState();
			addToast('Report delivery deleted', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to delete report config', e);
			addToast('Failed to delete report delivery', 'Error');
			return { success: false };
		} finally {
			isDeleting = false;
		}
	};

	const formatDate = (dateStr) => {
		if (!dateStr) return 'Never';
		try {
			return new Date(dateStr).toLocaleString();
		} catch {
			return dateStr;
		}
	};

	$: selectedSmtp = form.smtpConfigurationID ? smtpByID[form.smtpConfigurationID] : null;

	$: isBusy = isSaving || isDeleting;
</script>

<Modal headerText={isGlobal ? 'Default Report Delivery' : 'Report Delivery'} bind:visible>
	<div class="w-[640px] p-6 space-y-6">
		{#if isLoading}
			<div class="flex items-center justify-center py-10">
				<p class="text-gray-500 dark:text-gray-400">Loading...</p>
			</div>
		{:else if !pdfEnabled}
			<div
				class="rounded-md border border-amber-400 dark:border-amber-500/60 bg-amber-50 dark:bg-amber-900/20 p-4"
			>
				<p class="text-sm text-amber-700 dark:text-amber-400">
					PDF report generation is disabled. Enable it under Settings → Reports before configuring
					automatic delivery.
				</p>
			</div>
		{:else}
			<p class="text-sm text-gray-500 dark:text-gray-400">
				Email the campaign report PDF to a recipient group, on demand or automatically when a
				campaign is closed.
				{#if isGlobal}
					These are the global defaults used for any field a company does not set itself.
				{:else}
					Leave a field empty to use the global default. Enabling delivery is decided here, per
					company.
				{/if}
			</p>

			{#if config && !isGlobal}
				<div class="flex items-center gap-2 text-sm">
					<span class="text-gray-500 dark:text-gray-500">Last sent</span>
					<span class="text-gray-800 dark:text-gray-200">{formatDate(config.lastSentAt)}</span>
				</div>
			{/if}

			{#if !isGlobal}
				<!-- enabled toggle -->
				<div class="flex items-center justify-between">
					<div>
						<p class="text-sm font-medium text-gray-700 dark:text-gray-300">Enabled</p>
						<p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
							Turn report delivery on for this company.
						</p>
					</div>
					<button
						type="button"
						role="switch"
						aria-checked={form.enabled}
						disabled={isBusy}
						on:click={() => (form.enabled = !form.enabled)}
						class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none disabled:opacity-50
							{form.enabled ? 'bg-cta-blue dark:bg-highlight-blue/80' : 'bg-gray-300 dark:bg-gray-600'}"
					>
						<span
							class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200
								{form.enabled ? 'translate-x-5' : 'translate-x-0'}"
						/>
					</button>
				</div>

				<!-- send on finish toggle -->
				<div class="flex items-center justify-between">
					<div>
						<p class="text-sm font-medium text-gray-700 dark:text-gray-300">
							Send automatically on finish
						</p>
						<p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
							Deliver the report when a campaign is closed. When off, send on demand only.
						</p>
					</div>
					<button
						type="button"
						role="switch"
						aria-checked={form.sendOnFinish}
						disabled={isBusy || !form.enabled}
						on:click={() => (form.sendOnFinish = !form.sendOnFinish)}
						class="relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed
							{form.sendOnFinish ? 'bg-cta-blue dark:bg-highlight-blue/80' : 'bg-gray-300 dark:bg-gray-600'}"
					>
						<span
							class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition duration-200
								{form.sendOnFinish ? 'translate-x-5' : 'translate-x-0'}"
						/>
					</button>
				</div>
			{/if}

			<!-- recipient group -->
			<TextFieldSelect
				id="report-recipient-group"
				placeholder="Select a recipient group"
				bind:value={form.recipientGroupID}
				options={recipientGroupOptions}>Recipient group</TextFieldSelect
			>

			<!-- smtp configuration -->
			<TextFieldSelect
				id="report-smtp"
				placeholder="Select an SMTP configuration"
				bind:value={form.smtpConfigurationID}
				options={smtpOptions}>SMTP configuration</TextFieldSelect
			>
			{#if selectedSmtp}
				<div
					class="-mt-2 rounded-md bg-gray-50 dark:bg-gray-900/40 border border-gray-200 dark:border-gray-700/60 px-3 py-2 text-xs text-gray-500 dark:text-gray-400 space-y-0.5"
				>
					<p>
						Host <span class="text-gray-700 dark:text-gray-300 font-mono">{selectedSmtp.host}</span>
					</p>
					{#if selectedSmtp.username}
						<p>
							Username
							<span class="text-gray-700 dark:text-gray-300 font-mono">{selectedSmtp.username}</span>
						</p>
					{/if}
				</div>
			{/if}

			<!-- sender email (the From address) -->
			<TextField
				width="full"
				bind:value={form.senderEmail}
				toolTipText={'Used as the From address. Supports a display name, e.g. Acme Reports <noreply@acme.example.com>'}
				>Sender (From)</TextField
			>

			<!-- email subject -->
			<TextField width="full" bind:value={form.emailSubject}>Email subject</TextField>

			<!-- email body -->
			<div class="flex flex-col py-2">
				<p class="font-semibold text-slate-600 dark:text-gray-400 py-2">Email body</p>
				<SimpleCodeEditor
					bind:value={form.emailBody}
					language="html"
					height="small"
					showVimToggle={true}
					showExpandButton={true}
				/>
				<p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
					Subject and body support the same variables as the report template, such as {'{{.CompanyName}}'},
					{' '}{'{{.CampaignName}}'} and {'{{.ReportDate}}'}.
					{isGlobal
						? 'The default is shown and can be edited.'
						: 'Leave empty to use the global default.'}
				</p>
			</div>

			<!-- actions -->
			<div
				class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex gap-3 justify-end items-center"
			>
				{#if config}
					<button
						type="button"
						disabled={isBusy}
						on:click={() => (isDeleteAlertVisible = true)}
						class="bg-red-600 dark:bg-red-700/80 hover:bg-red-500 dark:hover:bg-red-600/80 text-sm uppercase font-bold px-4 py-2 text-white rounded-md disabled:opacity-50 transition-colors duration-200"
					>
						Delete
					</button>
				{/if}
				<FormButton size="medium" isSubmitting={isSaving} on:click={onSave}>Save</FormButton>
			</div>
		{/if}
	</div>
</Modal>

<Alert
	headline="Delete Report Delivery"
	bind:visible={isDeleteAlertVisible}
	onConfirm={onConfirmDelete}
	verification="delete"
>
	<p>
		Are you sure you want to delete the report delivery configuration for
		<strong>{isGlobal ? 'the global default' : company?.name}</strong>?
	</p>
</Alert>
