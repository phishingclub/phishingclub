<script>
	import { addToast } from '$lib/store/toast';
	import Modal from '../Modal.svelte';
	import Alert from '../Alert.svelte';
	import TextField from '../TextField.svelte';
	import SelectSquare from '../SelectSquare.svelte';
	import FormSection from '../form/FormSection.svelte';
	import FormError from '../FormError.svelte';
	import FormButton from '../FormButton.svelte';
	import Button from '../Button.svelte';
	import Note from '../Note.svelte';
	import { api } from '$lib/api/apiProxy.js';

	// external
	export let visible = false;
	/** @type {{ id: string, name: string } | null} */
	export let company = null;
	// when true the modal edits the global defaults (no company)
	export let isGlobal = false;

	// the scope id passed to the api: a company id, or null for the global defaults
	$: companyId = isGlobal ? null : (company?.id ?? null);
	$: ready = isGlobal || !!company;

	// the values the new campaign form starts with when nothing is set
	const BUILT_IN = {
		isAnonymous: false,
		saveSubmittedData: true
	};
	const MAX_DAYS = 3650;

	// local state
	let config = null;
	// the stored global row, used on a company to show what an unset field uses
	let globalConfig = null;
	let isLoading = false;
	let isSaving = false;
	let isDeleting = false;
	let isDeleteAlertVisible = false;
	let formError = '';

	// day fields hold text from the inputs, empty means not set
	let form = emptyForm();

	function emptyForm() {
		return {
			closeAfterDays: '',
			anonymizeAfterDays: '',
			dataAnonymizeAfterDays: '',
			isAnonymous: null,
			saveSubmittedData: null
		};
	}

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

	// an anonymous campaign never saves submitted data. this level is anonymous when it
	// says Yes itself, or when it follows a default that says Yes
	$: ownAnonymous = form.isAnonymous === true;
	$: effectiveAnonymous = form.isAnonymous ?? inheritedValue('isAnonymous', globalConfig);
	$: saveDataLocked = effectiveAnonymous === true;

	// the Yes or No shown on each choice: this level's own value, else the value it follows
	let anonymousChoice = false;
	let saveDataChoice = true;
	$: anonymousChoice = form.isAnonymous ?? inheritedValue('isAnonymous', globalConfig);
	$: saveDataChoice = saveDataLocked
		? false
		: (form.saveSubmittedData ?? inheritedValue('saveSubmittedData', globalConfig));

	const resetState = () => {
		config = null;
		globalConfig = null;
		formError = '';
		isDeleteAlertVisible = false;
		form = emptyForm();
	};

	const dayText = (v) => (v === null || v === undefined ? '' : String(v));

	const loadAll = async () => {
		isLoading = true;
		formError = '';
		try {
			const [own, global] = await Promise.all([
				api.company.campaignDefaults.getByCompanyID(companyId),
				isGlobal ? null : api.company.campaignDefaults.getByCompanyID(null)
			]);
			config = own?.success && own.data ? own.data : null;
			globalConfig = global?.success && global.data ? global.data : null;
			form = config
				? {
						closeAfterDays: dayText(config.closeAfterDays),
						anonymizeAfterDays: dayText(config.anonymizeAfterDays),
						dataAnonymizeAfterDays: dayText(config.dataAnonymizeAfterDays),
						isAnonymous: config.isAnonymous ?? null,
						saveSubmittedData: config.saveSubmittedData ?? null
					}
				: emptyForm();
		} catch (e) {
			console.error('failed to load campaign defaults', e);
			config = null;
			globalConfig = null;
		} finally {
			isLoading = false;
		}
	};

	// what an unset day field uses, shown as the input placeholder
	const inheritedDays = (field, globalRow) => {
		if (isGlobal) return 'Not set';
		const v = globalRow?.[field];
		return v === null || v === undefined ? 'Global: not set' : `Global: ${v} days`;
	};

	// the value a choice follows when this level does not set it: on a company the
	// global value, else the built in value. an anonymous campaign is handled by the lock
	const inheritedValue = (field, globalRow) => {
		if (!isGlobal) {
			const v = globalRow?.[field];
			if (v !== null && v !== undefined) return v;
		}
		return BUILT_IN[field];
	};

	// where the followed value comes from, as shown in the line under a choice
	const inheritedSource = (field, globalRow) => {
		if (isGlobal) return 'built in default';
		const v = globalRow?.[field];
		return v !== null && v !== undefined ? 'global default' : 'built in default';
	};

	const yesNoOptions = [
		{ label: 'Yes', value: true },
		{ label: 'No', value: false }
	];
	const upperFirst = (t) => t.charAt(0).toUpperCase() + t.slice(1);
	const yesNo = (v) => (v ? 'Yes' : 'No');
	const linkClass =
		'pc-focus rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';

	$: anonymousSource = inheritedSource('isAnonymous', globalConfig);
	$: saveDataSource = inheritedSource('saveSubmittedData', globalConfig);

	// parse a day field, returns undefined when the text is not a valid number of days
	const parseDays = (text) => {
		const s = String(text ?? '').trim();
		if (s === '') return null;
		if (!/^\d+$/.test(s)) return undefined;
		const n = Number(s);
		return n <= MAX_DAYS ? n : undefined;
	};

	const onSave = async () => {
		formError = '';
		const closeAfterDays = parseDays(form.closeAfterDays);
		const anonymizeAfterDays = parseDays(form.anonymizeAfterDays);
		const dataAnonymizeAfterDays = parseDays(form.dataAnonymizeAfterDays);
		if (
			closeAfterDays === undefined ||
			anonymizeAfterDays === undefined ||
			dataAnonymizeAfterDays === undefined
		) {
			formError = `Days must be a whole number from 0 to ${MAX_DAYS}, or empty.`;
			return;
		}
		isSaving = true;
		try {
			const res = await api.company.campaignDefaults.upsert(companyId, {
				closeAfterDays,
				anonymizeAfterDays,
				dataAnonymizeAfterDays,
				isAnonymous: form.isAnonymous,
				// a level that is anonymous itself can not also save data, so a Yes
				// is dropped there. Otherwise the stored choice is kept, also while
				// an anonymous global default locks it.
				saveSubmittedData:
					ownAnonymous && form.saveSubmittedData === true ? null : form.saveSubmittedData
			});
			if (!res || !res.success) {
				formError = res?.error ?? 'Failed to save campaign defaults';
				return;
			}
			config = res.data;
			addToast('Campaign defaults saved', 'Success');
			visible = false;
		} catch (e) {
			console.error('failed to save campaign defaults', e);
			addToast('Failed to save campaign defaults', 'Error');
		} finally {
			isSaving = false;
		}
	};

	const onConfirmDelete = async () => {
		isDeleting = true;
		try {
			const res = await api.company.campaignDefaults.delete(companyId);
			if (!res || !res.success) {
				addToast(res?.error ?? 'Failed to delete campaign defaults', 'Error');
				return { success: false };
			}
			config = null;
			form = emptyForm();
			formError = '';
			addToast('Campaign defaults deleted', 'Success');
			return { success: true };
		} catch (e) {
			console.error('failed to delete campaign defaults', e);
			addToast('Failed to delete campaign defaults', 'Error');
			return { success: false };
		} finally {
			isDeleting = false;
		}
	};

	$: isBusy = isSaving || isDeleting;
</script>

<Modal headerText={isGlobal ? 'Default campaign settings' : 'Campaign defaults'} bind:visible>
	<div class="w-[640px] max-w-full py-4 sm:p-6 space-y-6">
		{#if isLoading}
			<div class="flex items-center justify-center py-10">
				<p class="text-gray-500 dark:text-gray-400">Loading...</p>
			</div>
		{:else}
			<p class="text-sm text-gray-500 dark:text-gray-400">
				Values a new campaign starts with. They can be changed or cleared in each campaign, and
				existing campaigns are never changed.
				{#if isGlobal}
					Companies use these unless they set their own.
				{:else}
					Leave a day field empty to use the global default.
				{/if}
			</p>

			<FormSection title="Dates">
				<p class="text-sm text-gray-500 dark:text-gray-400">
					Dates are filled in from these days and follow the schedule until you edit them.
				</p>
				<div>
					<TextField
						id="campaign-defaults-close"
						type="number"
						min={0}
						max={MAX_DAYS}
						disabled={isBusy}
						placeholder={inheritedDays('closeAfterDays', globalConfig)}
						bind:value={form.closeAfterDays}>Close after (days)</TextField
					>
					<p class="text-xs text-gray-500 dark:text-gray-400">
						Days after the last send.
					</p>
				</div>
				<div>
					<TextField
						id="campaign-defaults-anonymize"
						type="number"
						min={0}
						max={MAX_DAYS}
						disabled={isBusy}
						placeholder={inheritedDays('anonymizeAfterDays', globalConfig)}
						bind:value={form.anonymizeAfterDays}>Anonymize all data after (days)</TextField
					>
					<p class="text-xs text-gray-500 dark:text-gray-400">
						Days after close. Not used by anonymous campaigns, they are anonymized when closed.
					</p>
				</div>
				<div>
					<TextField
						id="campaign-defaults-data-anonymize"
						type="number"
						min={0}
						max={MAX_DAYS}
						disabled={isBusy}
						placeholder={inheritedDays('dataAnonymizeAfterDays', globalConfig)}
						bind:value={form.dataAnonymizeAfterDays}
						>Anonymize submitted data after (days)</TextField
					>
					<p class="text-xs text-gray-500 dark:text-gray-400">
						Days after close. Not used by anonymous campaigns.
					</p>
				</div>
			</FormSection>

			<FormSection title="Data">
				<SelectSquare
					label="Anonymous campaign?"
					toolTipText="No names, emails, IPs or entered data are stored."
					disabled={isBusy}
					options={yesNoOptions}
					bind:value={anonymousChoice}
					onChange={() => (form.isAnonymous = anonymousChoice)}
				/>
				<div class="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
					{#if form.isAnonymous === null}
						<span class="text-sm text-slate-600 dark:text-gray-300"
							>{upperFirst(anonymousSource)}</span
						>
					{:else}
						<span class="text-sm text-slate-600 dark:text-gray-300"
							>Overrides the {anonymousSource} ({yesNo(
								inheritedValue('isAnonymous', globalConfig)
							)})</span
						>
						<button type="button" class={linkClass} on:click={() => (form.isAnonymous = null)}
							>Use {anonymousSource}</button
						>
					{/if}
				</div>

				<SelectSquare
					label="Save submitted data?"
					toolTipText="Consider privacy when saving data."
					disabled={isBusy || saveDataLocked}
					options={yesNoOptions}
					bind:value={saveDataChoice}
					onChange={() => (form.saveSubmittedData = saveDataChoice)}
				/>
				{#if saveDataLocked}
					{#if ownAnonymous}
						<Note tone="info">Anonymous campaigns never save submitted data.</Note>
					{:else}
						<Note tone="info">
							New campaigns for this company are anonymous (global default), so submitted data is
							never saved.
						</Note>
					{/if}
				{:else}
					<div class="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
						{#if form.saveSubmittedData === null}
							<span class="text-sm text-slate-600 dark:text-gray-300"
								>{upperFirst(saveDataSource)}</span
							>
						{:else}
							<span class="text-sm text-slate-600 dark:text-gray-300"
								>Overrides the {saveDataSource} ({yesNo(
									inheritedValue('saveSubmittedData', globalConfig)
								)})</span
							>
							<button
								type="button"
								class={linkClass}
								on:click={() => (form.saveSubmittedData = null)}>Use {saveDataSource}</button
							>
						{/if}
					</div>
				{/if}
			</FormSection>

			<FormError message={formError} />

			<!-- actions -->
			<div
				class="border-t border-gray-200 dark:border-gray-700/60 pt-4 flex gap-3 justify-end items-center"
			>
				{#if config}
					<Button
						size="medium"
						variant="danger-outline"
						disabled={isBusy}
						on:click={() => (isDeleteAlertVisible = true)}
					>
						Delete
					</Button>
				{/if}
				<FormButton size="medium" isSubmitting={isSaving} on:click={onSave}>Save</FormButton>
			</div>
		{/if}
	</div>
</Modal>

<Alert
	headline="Delete Campaign Defaults"
	bind:visible={isDeleteAlertVisible}
	onConfirm={onConfirmDelete}
	verification="delete"
>
	<p>
		Are you sure you want to delete the campaign defaults for
		<strong>{isGlobal ? 'the global default' : company?.name}</strong>?
		{isGlobal
			? 'New campaigns then start with the built in values.'
			: 'New campaigns then use the global defaults.'}
	</p>
</Alert>
