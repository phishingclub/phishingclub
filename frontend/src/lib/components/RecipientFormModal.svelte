<script>
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { getModalText } from '$lib/utils/common';
	import { LANGUAGES } from '$lib/utils/languages.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';

	// the company the recipient is created in, null for shared
	export let companyID = null;
	// toast shown after a create
	export let createdText = 'Recipient created';
	// runs after a create with the new recipient id, a returned string is shown as the form error
	/** @type {(id: string) => Promise<string>} */
	export let afterCreate = async () => '';
	// runs after a successful save with the saved values
	/** @type {(mode: string, values: object) => void} */
	export let onSaved = () => {};

	const emptyValues = () => ({
		id: null,
		email: '',
		phone: '',
		extraIdentifier: '',
		firstName: '',
		lastName: '',
		position: '',
		department: '',
		city: '',
		country: '',
		misc: '',
		language: ''
	});

	// the language list for the recipient form, None clears it
	const languageOptions = [
		{ value: '', label: 'None' },
		...LANGUAGES.map((l) => ({ value: l.code, label: l.name }))
	];

	let form = null;
	let values = emptyValues();
	let error = '';
	let mode = 'create';
	let visible = false;
	let isSubmitting = false;

	$: headerText = getModalText('recipient', mode);

	export const openCreate = () => {
		mode = 'create';
		values = emptyValues();
		error = '';
		visible = true;
	};

	/** @param {string} id */
	export const openUpdate = (id) => openExisting('update', id);

	/** @param {string} id */
	export const openCopy = (id) => openExisting('copy', id);

	const openExisting = async (nextMode, id) => {
		try {
			showIsLoading();
			const res = await api.recipient.getByID(id);
			if (!res.success) {
				throw res.error;
			}
			const r = res.data;
			mode = nextMode;
			values = {
				id: r.id,
				email: r.email,
				phone: r.phone,
				extraIdentifier: r.extraIdentifier,
				firstName: r.firstName,
				lastName: r.lastName,
				position: r.position,
				department: r.department,
				city: r.city,
				country: r.country,
				misc: r.misc,
				language: r.language ?? ''
			};
			error = '';
			visible = true;
		} catch (e) {
			addToast('Failed to get recipient', 'Error');
			console.error('failed to get recipient', e);
		} finally {
			hideIsLoading();
		}
	};

	const close = () => {
		visible = false;
		error = '';
		values = emptyValues();
		form?.reset();
	};

	const fields = () => ({
		email: values.email,
		phone: values.phone,
		extraIdentifier: values.extraIdentifier,
		firstName: values.firstName,
		lastName: values.lastName,
		position: values.position,
		department: values.department,
		city: values.city,
		country: values.country,
		misc: values.misc,
		language: values.language ?? '',
		companyID
	});

	const onSubmit = async () => {
		error = '';
		isSubmitting = true;
		const submitMode = mode;
		try {
			if (submitMode === 'update') {
				const res = await api.recipient.update({ id: values.id, ...fields() });
				if (!res.success) {
					error = res.error;
					return;
				}
				addToast('Recipient updated', 'Success');
			} else {
				const res = await api.recipient.create(fields());
				if (!res.success) {
					error = res.error;
					return;
				}
				const afterError = await afterCreate(res.data.id);
				if (afterError) {
					error = afterError;
					return;
				}
				addToast(createdText, 'Success');
			}
			const saved = { ...values };
			close();
			onSaved(submitMode, saved);
		} catch (e) {
			const action = submitMode === 'update' ? 'update' : 'create';
			addToast(`Failed to ${action} recipient`, 'Error');
			console.error(`failed to ${action} recipient`, e);
		} finally {
			isSubmitting = false;
		}
	};
</script>

<Modal {headerText} bind:visible onClose={close} {isSubmitting}>
	<FormGrid on:submit={onSubmit} bind:bindTo={form} {isSubmitting}>
		<FormColumns>
			<FormColumn>
				<TextField
					required
					minLength={1}
					maxLength={255}
					type="email"
					bind:value={values.email}
					placeholder="e.g. bob@example.test"
					width="medium"
					readonly={mode === 'update'}>Email</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.firstName}
					placeholder="e.g. Bob"
					optional>First name</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.lastName}
					placeholder="e.g. Jensen"
					optional>Last name</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.position}
					placeholder="e.g. CEO"
					optional>Position</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.department}
					placeholder="e.g. Sales"
					optional>Department</TextField
				>
			</FormColumn>
			<FormColumn>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.phone}
					placeholder="e.g. +45 555 555 5555"
					optional>Phone</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.extraIdentifier}
					placeholder="e.g. 4982347283947"
					toolTipText="Optional extra identifier"
					optional>Extra identifier</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.city}
					placeholder="e.g. Copenhagen"
					optional>City</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.country}
					placeholder="e.g. Denmark"
					optional>Country</TextField
				>
				<TextField
					minLength={1}
					maxLength={127}
					bind:value={values.misc}
					placeholder="e.g. VIP"
					toolTipText="Any extra information"
					optional>Miscellaneous</TextField
				>
				<TextFieldSelect
					id="recipient-language"
					bind:value={values.language}
					optional
					placeholder="None"
					toolTipText="The language the recipient reads. A campaign with Language by Language sends it when every template has it."
					options={languageOptions}>Language</TextFieldSelect
				>
			</FormColumn>
		</FormColumns>
		<FormError message={error} />
		<FormFooter closeModal={close} {isSubmitting} />
	</FormGrid>
</Modal>
