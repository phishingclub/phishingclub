<script>
	// Copies a program as a draft, optionally into another company.
	// Set program to open the dialog, it is set back to null on close.
	// Fires "created" when the copy exists and "done" with { id, companyID }
	// when the copy is in the current company and should be opened for editing.
	import { createEventDispatcher } from 'svelte';
	import Note from '$lib/components/Note.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import { addToast } from '$lib/store/toast';
	import { plural } from '$lib/utils/programText.js';

	/** @type {{ id: string, name: string, companyID?: string|null, endAt?: string|null } | null} */
	export let program = null;
	/** @type {{ id: string, name: string }[]} */
	export let companies = [];
	export let contextCompanyID = '';

	const dispatch = createEventDispatcher();

	const cloneKindLabels = {
		template: 'Template',
		group: 'Recipient group',
		denyPage: 'Deny page',
		evasionPage: 'Evasion page',
		allowDeny: 'Allow or deny list',
		webhook: 'Webhook'
	};
	const sharedCompanyLabel = 'Shared (no company)';
	const noteClass =
		'text-sm text-slate-500 dark:text-gray-400 max-w-sm transition-colors duration-200';

	let isVisible = false;
	let isCloning = false;
	let cloneError = '';
	// the server refused the name, cleared once the name changes
	let cloneNameTaken = false;
	// the copy is a draft without an end, hadEnd tells the source had one
	let cloneSource = { id: null, name: '', hadEnd: false };
	let cloneName = '';
	let cloneCompany = '';
	let cloneResult = null;
	let openedFor = null;

	$: cloneName, (cloneNameTaken = false);
	$: companyOptions = [sharedCompanyLabel, ...companies.map((c) => c.name)];
	const companyIDByName = (name) =>
		name && name !== sharedCompanyLabel ? companies.find((c) => c.name === name)?.id || null : null;
	const companyNameByID = (id) =>
		id ? companies.find((c) => c.id === id)?.name || 'another company' : sharedCompanyLabel;

	$: if (program && program !== openedFor) {
		openFor(program);
	}

	const openFor = (source) => {
		openedFor = source;
		cloneSource = { id: source.id, name: source.name, hadEnd: !!source.endAt };
		cloneName = `${source.name} (Copy)`.slice(0, 64);
		// the copy goes to the source company unless that company is not listed
		const sourceCompany = companyNameByID(source.companyID || null);
		cloneCompany = companyOptions.includes(sourceCompany)
			? sourceCompany
			: companyNameByID(contextCompanyID || null);
		cloneError = '';
		cloneResult = null;
		isVisible = true;
	};

	const closeCloneModal = () => {
		isVisible = false;
		cloneResult = null;
		cloneError = '';
		openedFor = null;
		program = null;
	};

	const onClone = async () => {
		// a second submit while the copy is made is ignored
		if (isCloning) return;
		if (cloneResult) {
			openClone();
			return;
		}
		if (!cloneName || !cloneName.trim()) {
			cloneError = 'Enter a name';
			return;
		}
		if (companies.length && !companyOptions.includes(cloneCompany)) {
			cloneError = 'Choose a company from the list';
			return;
		}
		try {
			isCloning = true;
			cloneError = '';
			const companyID = companies.length ? companyIDByName(cloneCompany) : contextCompanyID || null;
			const res = await api.program.clone(cloneSource.id, cloneName.trim(), companyID);
			if (!res.success) {
				// a taken name marks the name field, the message shows below the form
				const nameError = /^name: (.+)$/.exec(String(res.error ?? ''));
				cloneNameTaken = !!nameError;
				cloneError = nameError ? `Name ${nameError[1]}` : res.error || 'Failed to copy the program';
				return;
			}
			cloneResult = {
				id: res.data?.id,
				companyID,
				dropped: res.data?.dropped || []
			};
			dispatch('created', { id: cloneResult.id, companyID });
			// nothing to report, go straight to the copy
			if (!cloneResult.dropped.length) {
				openClone();
			}
		} catch (e) {
			cloneError = 'Failed to copy the program';
			console.error('failed to copy program', e);
		} finally {
			isCloning = false;
		}
	};

	// the copy can only be edited here when it is in the current company
	$: cloneInContext =
		!!cloneResult && (cloneResult.companyID || null) === (contextCompanyID || null);

	const openClone = () => {
		const result = cloneResult;
		if (!result) return;
		if ((result.companyID || null) !== (contextCompanyID || null)) {
			closeCloneModal();
			addToast(`Copy saved as a draft in ${companyNameByID(result.companyID)}`, 'Success');
			return;
		}
		closeCloneModal();
		addToast('Copy saved as a draft', 'Success');
		dispatch('done', { id: result.id, companyID: result.companyID });
	};
</script>

<Modal
	headerText="Copy Program"
	bind:visible={isVisible}
	onClose={closeCloneModal}
	isSubmitting={isCloning}
>
	<FormGrid on:submit={onClone} isSubmitting={isCloning} novalidate>
		<FormColumns>
			<FormColumn>
				{#if !cloneResult}
					<TextField
						id="program-clone-name"
						minLength={1}
						maxLength={64}
						error={(!!cloneError && !cloneName) || cloneNameTaken}
						toolTipText="The copy is saved as a draft with the same templates and settings. It has no results and sends nothing until you start it."
						bind:value={cloneName}>Name</TextField
					>
					{#if companies.length}
						<TextFieldSelect
							id="program-clone-company"
							bind:value={cloneCompany}
							toolTipText="Copying to another company leaves out its recipient groups and anything that company cannot use. You pick replacements before you start."
							options={companyOptions}
						>
							Company
						</TextFieldSelect>
					{/if}
					{#if cloneSource.hadEnd}
						<p id="program-clone-no-end" class={noteClass}>
							The copy has no end date. Set one in the copy before you start it.
						</p>
					{/if}
				{:else}
					<Note boxed role="status" class="max-w-sm">
						Saved as a draft. {plural(cloneResult.dropped.length, 'item was', 'items were')} left out
						because {companyNameByID(cloneResult.companyID)} cannot use {cloneResult.dropped
							.length === 1
							? 'it'
							: 'them'}.
					</Note>
					<div
						id="program-clone-dropped"
						class="w-full max-w-sm max-h-96 overflow-y-auto grid grid-cols-[minmax(0,1fr)_auto] gap-x-6 gap-y-2 py-4 text-sm [&>*]:min-w-0"
					>
						{#each cloneResult.dropped as item}
							<span class="text-pc-darkblue dark:text-white [overflow-wrap:anywhere]"
								>{item.name || item.id}</span
							>
							<span class="text-slate-500 dark:text-gray-400"
								>{cloneKindLabels[item.kind] || item.kind}</span
							>
						{/each}
					</div>
					<p class={noteClass}>
						{cloneInContext
							? 'Choose replacements in the copy before you start it.'
							: `Switch to ${companyNameByID(cloneResult.companyID)} to update the copy and choose replacements.`}
					</p>
				{/if}
			</FormColumn>
		</FormColumns>
		<FormError message={cloneError} />
		<FormFooter
			closeModal={closeCloneModal}
			isSubmitting={isCloning}
			closeText={cloneResult ? 'Close' : 'Cancel'}
			okText={cloneResult ? (cloneInContext ? 'Update' : 'Done') : 'Create'}
		/>
	</FormGrid>
</Modal>
