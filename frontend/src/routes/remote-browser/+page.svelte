<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import { newTableURLParams } from '$lib/service/tableURLParams.js';
	import Headline from '$lib/components/Headline.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableUpdateButton from '$lib/components/table/TableUpdateButton.svelte';
	import TableDeleteButton from '$lib/components/table/TableDeleteButton2.svelte';
	import TableCopyButton from '$lib/components/table/TableCopyButton.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import { addToast } from '$lib/store/toast';
	import { AppStateService } from '$lib/service/appState';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import TableCellAction from '$lib/components/table/TableCellAction.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';
	import { getModalText } from '$lib/utils/common';
	import DeleteAlert from '$lib/components/modal/DeleteAlert.svelte';
	import AutoRefresh from '$lib/components/AutoRefresh.svelte';
	import BigButton from '$lib/components/BigButton.svelte';
	import TableDropDownEllipsis from '$lib/components/table/TableDropDownEllipsis.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import RemoteBrowserEditor from '$lib/components/remote-browser/RemoteBrowserEditor.svelte';

	const appStateService = AppStateService.instance;

	// form state
	let formValues = {
		id: null,
		name: '',
		description: '',
		script: defaultScript(),
		config: JSON.stringify(
			{ mode: 'local', remote: '', proxy: '', headless: true, timeout: 300000 },
			null,
			2
		)
	};
	let isSubmitting = false;
	let formError = '';
	let savedScript = defaultScript();

	// table state
	const tableURLParams = newTableURLParams();
	let contextCompanyID = null;
	let items = [];
	let hasNextPage = true;
	let isTableLoading = false;
	let featureDisabled = false;

	// modal state
	let isModalVisible = false;
	let modalMode = null;
	let modalText = '';

	// delete state
	let isDeleteAlertVisible = false;
	let deleteValues = { id: null, name: null };

	$: modalText = getModalText('Remote Browser', modalMode);

	function defaultScript() {
		return `var s = newSession({ headless: true });

var IN = {
    credentials: 'credentials',
    otp:         'otp'
};

var OUT = {
    ready:       'ready',
    otpRequired: 'otp_required',
    done:        'done',
    failed:      'failed'
};

var SEL = {
    username: 'input[name="username"]',
    password: 'input[name="password"]',
    otp:      'input[name="otp"]',
    submit:   'button[type="submit"]',
    error:    '.error-message'
};

s.navigate('https://portal.example.internal/login');
s.waitVisible(SEL.username);
emit(OUT.ready);

function handleCredentials() {
    return retry({ max: 3, wait: 500 }, function() {
        var creds = waitForEvent(IN.credentials);
        submitData(creds);
        s.sendKeys(SEL.username, creds.username);
        s.sendKeys(SEL.password, creds.password);
        s.click(SEL.submit);

        var r = s.race({
            otp:   { urlContains: '/verify-otp' },
            home:  { urlContains: '/dashboard' },
            error: { visible: SEL.error }
        });
        if (r.key === 'error') { emit(OUT.failed, 'bad_credentials'); return false; }
        return r.key;
    });
}

function handleOTP() {
    return retry({ max: 5 }, function() {
        s.waitVisible(SEL.otp);
        emit(OUT.otpRequired);
        var o = waitForEvent(IN.otp);
        submitData(o);
        s.sendKeys(SEL.otp, o.otp);
        s.click(SEL.submit);

        var r = s.race({
            home:  { urlContains: '/dashboard' },
            error: { visible: SEL.error }
        });
        if (r.key === 'error') { emit(OUT.failed, 'bad_otp'); return false; }
        return r.key;
    });
}

var phase = handleCredentials();
if (phase === 'otp') phase = handleOTP();

if (phase === 'home') {
    s.capture({ domains: ['example.internal'] });
    emit(OUT.done);
}

s.keepAlive();
`;
	}

	onMount(() => {
		const context = appStateService.getContext();
		if (context) contextCompanyID = context.companyID;

		refreshItems();
		tableURLParams.onChange(refreshItems);

		(async () => {
			const editID = $page.url.searchParams.get('edit');
			if (editID) await openUpdateModal(editID);
		})();

		return () => tableURLParams.unsubscribe();
	});

	const refreshItems = async (showLoading = true) => {
		try {
			if (showLoading) isTableLoading = true;
			const res = await api.remoteBrowser.getAllSubset(tableURLParams, contextCompanyID);
			if (res.statusCode === 404) {
				featureDisabled = true;
				return;
			}
			if (res.success) {
				items = res.data.rows ?? res.data ?? [];
				hasNextPage = res.data.hasNextPage ?? false;
			}
		} catch (e) {
			addToast('Failed to load Remote Browsers', 'Error');
			console.error(e);
		} finally {
			if (showLoading) isTableLoading = false;
		}
	};

	const openCreateModal = () => {
		formValues = {
			id: null,
			name: '',
			description: '',
			script: defaultScript(),
			config: JSON.stringify(
				{ mode: 'local', remote: '', proxy: '', headless: true, timeout: 300000 },
				null,
				2
			)
		};
		savedScript = defaultScript();
		formError = '';
		modalMode = 'create';
		isModalVisible = true;
	};

	const openUpdateModal = async (id) => {
		try {
			const res = await api.remoteBrowser.getByID(id);
			if (!res.success) throw res.error;
			const rb = res.data;
			formValues = {
				id: rb.id,
				name: rb.name || '',
				description: rb.description || '',
				script: rb.script || defaultScript(),
				config: rb.config
					? JSON.stringify(rb.config, null, 2)
					: JSON.stringify({ mode: 'local', headless: true, timeout: 300000 }, null, 2)
			};
			savedScript = formValues.script;
			formError = '';
			modalMode = 'update';
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load Remote Browser', 'Error');
			console.error(e);
		}
	};

	const closeModal = () => {
		isModalVisible = false;
		formError = '';
	};

	const onEditorChange = (event) => {
		const { name, description, script, config } = event.detail;
		formValues = { ...formValues, name, description, script, config };
	};

	const openCopyModal = async (id) => {
		try {
			const res = await api.remoteBrowser.getByID(id);
			if (!res.success) throw res.error;
			const rb = res.data;
			formValues = {
				id: null,
				name: rb.name ? `${rb.name} (copy)` : '',
				description: rb.description || '',
				script: rb.script || defaultScript(),
				config: rb.config
					? JSON.stringify(rb.config, null, 2)
					: JSON.stringify({ mode: 'local', headless: true, timeout: 300000 }, null, 2)
			};
			savedScript = formValues.script;
			formError = '';
			modalMode = 'copy';
			isModalVisible = true;
		} catch (e) {
			addToast('Failed to load Remote Browser', 'Error');
			console.error(e);
		}
	};

	const onSubmit = async (event) => {
		isSubmitting = true;
		try {
			const saveOnly = event?.detail?.saveOnly || false;
			if (modalMode === 'create' || modalMode === 'copy') {
				await create();
			} else {
				await update(saveOnly);
			}
		} finally {
			isSubmitting = false;
		}
	};

	const create = async () => {
		try {
			const res = await api.remoteBrowser.create({
				name: formValues.name,
				description: formValues.description,
				script: formValues.script,
				config: JSON.parse(formValues.config),
				companyID: contextCompanyID
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			formError = '';
			addToast('Remote Browser created', 'Success');
			// Stay in the modal - switch to update mode so the user can run/test immediately.
			formValues = { ...formValues, id: res.data.id };
			savedScript = formValues.script;
			modalMode = 'update';
			refreshItems();
		} catch (e) {
			addToast('Failed to create Remote Browser', 'Error');
			console.error(e);
		}
	};

	const update = async (saveOnly = false) => {
		try {
			const res = await api.remoteBrowser.update(formValues.id, {
				name: formValues.name,
				description: formValues.description,
				script: formValues.script,
				config: JSON.parse(formValues.config)
			});
			if (!res.success) {
				formError = res.error;
				return;
			}
			formError = '';
			savedScript = formValues.script;
			addToast(saveOnly ? 'Saved' : 'Remote Browser updated', 'Success');
			if (!saveOnly) {
				closeModal();
				refreshItems();
			}
		} catch (e) {
			addToast(saveOnly ? 'Failed to save' : 'Failed to update Remote Browser', 'Error');
			console.error(e);
		}
	};

	const onClickDelete = async (id) => {
		const res = await api.remoteBrowser.delete(id);
		if (res.success) {
			refreshItems();
			return res;
		}
		throw res.error;
	};
</script>

<HeadTitle title="Remote Browsers" />

<div class="col-start-1 col-end-13 row-start-1 px-4">
	<div class="flex justify-between items-center">
		<div class="flex items-center gap-3">
			<Headline>Remote Browsers</Headline>
			<span
				class="px-2 py-0.5 text-xs font-medium rounded bg-slate-200 text-slate-500 dark:bg-slate-700 dark:text-slate-400"
				>Experimental</span
			>
		</div>
		<AutoRefresh isLoading={false} onRefresh={() => refreshItems(false)} />
	</div>

	{#if featureDisabled}
		<div class="mt-6 rounded-lg border border-slate-700 bg-slate-800/50 px-6 py-8 max-w-xl">
			<p class="text-sm font-semibold text-white mb-1">Remote Browser is not enabled</p>
			<p class="text-sm text-slate-400 mb-3">
				This feature is disabled by default for security reasons. When enabled, the server runs a
				browser under the application process. Any operator with access to the script editor can
				execute arbitrary commands on the host.
			</p>
			<p class="text-sm text-slate-400 mb-4">
				To enable it, set <code class="text-slate-200 bg-slate-700 px-1 rounded">enabled: true</code
				>
				in the <code class="text-slate-200 bg-slate-700 px-1 rounded">remote_browser</code> block of
				<code class="text-slate-200 bg-slate-700 px-1 rounded">config.json</code> and retart the service.
			</p>
			<a
				href="https://phishing.club/guide/remote-browser/#enabling"
				target="_blank"
				rel="noopener noreferrer"
				class="text-sm text-blue-400 hover:text-blue-300 underline">Read the setup guide</a
			>
		</div>
	{:else}
		<BigButton on:click={openCreateModal}>New Remote Browser</BigButton>

		<Table
			columns={[{ column: 'Name', size: 'large' }, { column: 'Description' }]}
			sortable={['Name']}
			hasData={!!items.length}
			{hasNextPage}
			plural="remote browsers"
			pagination={tableURLParams}
			isGhost={isTableLoading}
		>
			{#each items as item (item.id)}
				<TableRow>
					<TableCell>
						<button class="block w-full py-1 text-left" on:click={() => openUpdateModal(item.id)}
							>{item.name}</button
						>
					</TableCell>
					<TableCell>{item.description || ''}</TableCell>
					<TableCellEmpty />
					<TableCellAction>
						<TableDropDownEllipsis>
							<TableUpdateButton on:click={() => openUpdateModal(item.id)} />
							<TableCopyButton title="Copy" on:click={() => openCopyModal(item.id)} />
							<TableDeleteButton
								on:click={() => {
									deleteValues = { id: item.id, name: item.name };
									isDeleteAlertVisible = true;
								}}
							/>
						</TableDropDownEllipsis>
					</TableCellAction>
				</TableRow>
			{/each}
		</Table>
	{/if}
</div>

<!-- Editor modal (always fullscreen) -->
<Modal
	bind:visible={isModalVisible}
	headerText={modalText}
	fullscreen={true}
	onClose={closeModal}
	{isSubmitting}
>
	<FormGrid on:submit={onSubmit} {isSubmitting} {modalMode}>
		<div class="col-span-3 flex flex-col min-h-0 overflow-hidden px-4 py-4">
			<RemoteBrowserEditor
				bind:name={formValues.name}
				bind:description={formValues.description}
				bind:script={formValues.script}
				bind:config={formValues.config}
				id={formValues.id}
				{savedScript}
				on:change={onEditorChange}
			/>
		</div>

		<FormError message={formError} />

		<FormFooter {closeModal} {isSubmitting} okText={modalMode === 'create' ? 'Create' : 'Update'} />
	</FormGrid>
</Modal>

<DeleteAlert
	bind:isVisible={isDeleteAlertVisible}
	name={deleteValues.name}
	onClick={() => onClickDelete(deleteValues.id)}
/>
