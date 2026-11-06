<script>
	import { page } from '$app/stores';
	import { api } from '$lib/api/apiProxy.js';
	import { onMount } from 'svelte';
	import BetaTag from '$lib/components/BetaTag.svelte';
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
	import InstalledResourceBadge from '$lib/components/InstalledResourceBadge.svelte';
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
		return `const s = newSession();

s.navigate("https://example.com/login");

s.states({
    error: () => s.visible(".error"),
    login: () => s.visible("input[name='email']"),
    done:  () => s.visible(".logout")
}).run({
    login: () => {
        const creds = waitForEvent("credentials");
        submitData(creds);
        s.sendKeys("input[name='email']", creds.username);
        s.sendKeys("input[name='password']", creds.password);
        s.click("button[type='submit']");
    },
    error: () => {
        emit("failed", {});
    },
    done: (loop) => {
        s.capture({ domains: ["example.com"] });
        emit("done", {});
        loop.stop();
    }
});

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

<main>
	<!-- fixed row height on wide screens so the taller refresh select does not push the title down -->
	<div class="flex flex-wrap items-center justify-between gap-3 md:h-[2.625rem] md:flex-nowrap">
		<div class="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0">
			<Headline docSlug="remote-browser">Remote Browsers</Headline>
			<BetaTag label="Experimental" />
		</div>
		<AutoRefresh isLoading={false} onRefresh={() => refreshItems(false)} />
	</div>

	{#if featureDisabled}
		<div
			class="mt-6 rounded-lg border border-gray-200 bg-gray-50 px-6 py-8 max-w-xl dark:border-slate-700 dark:bg-slate-800/50"
		>
			<p class="text-sm font-semibold text-gray-800 dark:text-white mb-1">
				Remote Browser is not enabled
			</p>
			<p class="text-sm text-gray-600 dark:text-slate-400 mb-3">
				This feature is disabled by default for security reasons. When enabled, the server runs a
				browser under the application process. Any operator with access to the script editor can
				execute arbitrary commands on the host.
			</p>
			<p class="text-sm text-gray-600 dark:text-slate-400 mb-4">
				To enable it, set <code class="rounded bg-gray-200 px-1 text-gray-800 dark:bg-slate-700 dark:text-slate-200">enabled: true</code
				>
				in the <code class="rounded bg-gray-200 px-1 text-gray-800 dark:bg-slate-700 dark:text-slate-200">remote_browser</code> block of
				<code class="rounded bg-gray-200 px-1 text-gray-800 dark:bg-slate-700 dark:text-slate-200">config.json</code> and restart the service.
			</p>
			<a
				href="https://phishing.club/guide/remote-browser/#enabling"
				target="_blank"
				rel="noopener noreferrer"
				class="pc-focus rounded-sm text-sm text-cta-blue dark:text-highlight-blue hover:underline">Read the setup guide</a
			>
		</div>
	{:else}
		<BigButton on:click={openCreateModal}>New Remote Browser</BigButton>

		<Table
			reserveSelect
			columns={[{ column: 'Name', size: 'large' }, { column: 'Description', size: 'large' }]}
			sortable={['Name']}
			hasData={!!items.length}
			{hasNextPage}
			plural="remote browsers"
			pagination={tableURLParams}
			isGhost={isTableLoading}
		>
			{#each items as item (item.id)}
				<TableRow>
					<TableCell wrap>
						<button class="block w-full py-1 text-left" on:click={() => openUpdateModal(item.id)}
							>{item.name}</button
						>
					</TableCell>
					<TableCell value={item.description} />
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
</main>

<!-- Editor modal (always fullscreen) -->
<Modal
	bind:visible={isModalVisible}
	headerText={modalText}
	fullscreen={true}
	onClose={closeModal}
	{isSubmitting}
>
	<FormGrid on:submit={onSubmit} {isSubmitting} {modalMode}>
		<!-- one scroll area when stacked; on wide screens the editor fills the height -->
		<div class="flex flex-col min-h-0 overflow-y-auto lg:overflow-hidden py-4">
			{#if modalMode === 'update'}
				<div class="flex-none mb-3">
					<InstalledResourceBadge
						resourceType="remotebrowser"
						resourceId={formValues.id}
						variant="notice"
					/>
				</div>
			{/if}
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

		<FormFooter {closeModal} {isSubmitting} />
	</FormGrid>
</Modal>

<DeleteAlert
	bind:isVisible={isDeleteAlertVisible}
	name={deleteValues.name}
	onClick={() => onClickDelete(deleteValues.id)}
/>
