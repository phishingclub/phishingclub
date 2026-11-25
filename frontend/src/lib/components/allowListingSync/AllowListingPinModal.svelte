<script>
	import { createEventDispatcher } from 'svelte';
	import { addToast } from '$lib/store/toast';
	import { api } from '$lib/api/apiProxy.js';
	import { fetchAllRows } from '$lib/utils/api-utils.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import RadioOption from '$lib/components/RadioOption.svelte';
	import DateTimeField from '$lib/components/DateTimeField.svelte';
	import { ALLOW_LISTING_KIND_LABELS } from '$lib/utils/allowListingSync.js';

	// created fires after a pin is added
	const dispatch = createEventDispatcher();

	export let visible = false;
	// the target scope: a company id, or null for the global target
	export let companyId = null;

	const KIND_OPTIONS = Object.entries(ALLOW_LISTING_KIND_LABELS).map(([value, label]) => ({
		value,
		label
	}));
	const PLACEHOLDERS = {
		domain: 'mail.example.com',
		ip: '203.0.113.10 or 203.0.113.0/24',
		url: 'login.example.com/*'
	};

	let mode = 'template';
	let templates = [];
	let form = { templateID: '', kind: 'domain', value: '', note: '', expiresAt: null };
	let formError = '';
	let isSubmitting = false;

	$: if (visible) {
		open();
	}

	const open = async () => {
		mode = 'template';
		form = { templateID: '', kind: 'domain', value: '', note: '', expiresAt: null };
		formError = '';
		try {
			const rows = await fetchAllRows((options) =>
				api.campaignTemplate.getAll(options, companyId)
			);
			templates = rows.map((t) => ({ value: t.id, label: t.name }));
		} catch (e) {
			console.error('failed to load templates', e);
			templates = [];
		}
	};

	const close = () => {
		visible = false;
	};

	const onSubmit = async () => {
		formError = '';
		const base = {
			note: form.note.trim(),
			expiresAt: form.expiresAt ? new Date(form.expiresAt).toISOString() : null
		};
		let pin;
		if (mode === 'template') {
			if (!form.templateID) {
				formError = 'Pick a template';
				return;
			}
			pin = { ...base, campaignTemplateID: form.templateID };
		} else {
			if (!form.value.trim()) {
				formError = 'Enter a value';
				return;
			}
			pin = { ...base, kind: form.kind, value: form.value.trim() };
		}
		isSubmitting = true;
		try {
			const res = await api.allowListingSync.createPin(companyId, pin);
			if (!res || !res.success) {
				formError = res?.error ?? 'Failed to add pin';
				return;
			}
			addToast('Pin added', 'Success');
			dispatch('created');
			close();
		} catch (e) {
			console.error('failed to add pin', e);
			formError = 'Failed to add pin';
		} finally {
			isSubmitting = false;
		}
	};
</script>

<Modal
	headerText="Add pin"
	bind:visible
	onClose={close}
	{isSubmitting}
	docSlug="campaigns"
	docAnchor="allow-listing-sync-pins"
>
	<FormGrid on:submit={onSubmit} {isSubmitting}>
		<FormColumns>
			<FormColumn>
				<div
					class="w-full max-w-md space-y-2 mb-4"
					id="allow-listing-pin-mode"
					role="radiogroup"
					aria-label="What to pin"
				>
					<RadioOption
						checked={mode === 'template'}
						label="Template"
						description="Keep every value this template needs, also with no campaign."
						on:change={() => (mode = 'template')}
					/>
					<RadioOption
						checked={mode === 'value'}
						label="Value"
						description="Keep one domain, sending IP or simulation URL."
						on:change={() => (mode = 'value')}
					/>
				</div>
				{#if mode === 'template'}
					<TextFieldSelect
						id="allow-listing-pin-template"
						required
						containInModal
						placeholder={templates.length ? 'Select a template' : 'No templates'}
						bind:value={form.templateID}
						options={templates}>Template</TextFieldSelect
					>
				{:else}
					<TextFieldSelect
						id="allow-listing-pin-kind"
						required
						containInModal
						bind:value={form.kind}
						options={KIND_OPTIONS}>Kind</TextFieldSelect
					>
					<TextField
						id="allow-listing-pin-value"
						required
						maxLength={300}
						placeholder={PLACEHOLDERS[form.kind]}
						bind:value={form.value}>Value</TextField
					>
				{/if}
				<TextField
					id="allow-listing-pin-note"
					optional
					maxLength={255}
					placeholder="Why this is pinned"
					bind:value={form.note}>Note</TextField
				>
				<DateTimeField
					optional
					min={new Date()}
					bind:value={form.expiresAt}
					toolTipText="The pin is not used after this time. Empty keeps it until deleted."
					>Expires</DateTimeField
				>
				<FormError message={formError} />
			</FormColumn>
		</FormColumns>
		<FormFooter closeModal={close} okText="Add pin" {isSubmitting} />
	</FormGrid>
</Modal>
