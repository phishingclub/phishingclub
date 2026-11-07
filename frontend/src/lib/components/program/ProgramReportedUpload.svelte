<script>
	// Uploads a CSV of reported emails for a program.
	// Call pick() to choose a file; fires "uploaded" after the server took it.
	// An anonymous program takes no upload, its reports come from the report inbox.
	import { createEventDispatcher } from 'svelte';
	import papaparse from 'papaparse';
	import { api } from '$lib/api/apiProxy.js';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';
	import { HIDDEN_TIP } from '$lib/utils/programText.js';

	export let programID;
	export let anonymous = false;

	const dispatch = createEventDispatcher();
	const NO_DATE = 'No date column';

	let reportedFileInput;
	let isReportedModalVisible = false;
	let isReportedSubmitting = false;
	let reportedFile = null;
	let reportedHeaders = [];
	let reportedPreview = [];
	let reportedEmailColumn = '';
	let reportedDateColumn = '';
	let reportedResult = null;

	// each header as a unique label: an empty header is "Column N" and a
	// repeated one gets a number, so a picked label maps back to one index
	const headerLabels = (headers) => {
		const used = new Set([NO_DATE]);
		return headers.map((h, i) => {
			const base = String(h ?? '').trim() || `Column ${i + 1}`;
			let label = base;
			for (let n = 2; used.has(label); n++) label = `${base} (${n})`;
			used.add(label);
			return label;
		});
	};

	export const pick = () => {
		if (anonymous) return;
		reportedFileInput?.click();
	};

	const onPickReportedCSV = (event) => {
		const file = event.target.files?.[0];
		if (!file) return;
		if (!file.name.toLowerCase().endsWith('.csv')) {
			addToast('Please select a CSV file', 'Error');
			event.target.value = '';
			return;
		}
		papaparse.parse(file, {
			preview: 5,
			skipEmptyLines: true,
			complete: (res) => {
				event.target.value = '';
				if (!res.data || res.data.length < 2) {
					addToast('CSV file must have headers and at least one data row', 'Error');
					return;
				}
				reportedFile = file;
				reportedHeaders = headerLabels(res.data[0]);
				reportedPreview = res.data.slice(1);
				reportedEmailColumn =
					reportedHeaders.find((h) => /e-?mail/i.test(h)) ?? reportedHeaders[0] ?? '';
				reportedDateColumn = reportedHeaders.find((h) => /date|time/i.test(h)) ?? NO_DATE;
				reportedResult = null;
				isReportedModalVisible = true;
			},
			error: (error) => {
				addToast(`Failed to parse CSV: ${error.message}`, 'Error');
				event.target.value = '';
			}
		});
	};

	const closeReportedModal = () => {
		isReportedModalVisible = false;
		reportedFile = null;
		reportedHeaders = [];
		reportedPreview = [];
		reportedEmailColumn = '';
		reportedDateColumn = '';
		reportedResult = null;
	};

	const onConfirmReported = async () => {
		if (!reportedFile) return;
		const emailIdx = reportedHeaders.indexOf(reportedEmailColumn);
		if (emailIdx < 0) {
			addToast('Select the email column', 'Error');
			return;
		}
		const dateIdx = reportedHeaders.indexOf(reportedDateColumn);
		try {
			isReportedSubmitting = true;
			showIsLoading();
			const res = await api.program.uploadReportedCSV(
				programID,
				reportedFile,
				emailIdx,
				dateIdx >= 0 ? dateIdx : null
			);
			if (!res.success) {
				addToast(`Upload failed: ${res.error || 'unknown error'}`, 'Error');
				return;
			}
			reportedResult = res.data;
			dispatch('uploaded');
		} catch (e) {
			console.error('failed to upload reported csv', e);
			addToast('Failed to upload the CSV', 'Error');
		} finally {
			isReportedSubmitting = false;
			hideIsLoading();
		}
	};
</script>

<input
	type="file"
	accept=".csv"
	class="hidden"
	bind:this={reportedFileInput}
	on:change={onPickReportedCSV}
/>

<Modal
	headerText="Import reported emails"
	visible={isReportedModalVisible}
	onClose={closeReportedModal}
	isSubmitting={isReportedSubmitting}
	noAutoFocus={true}
>
	{#if reportedResult}
		<FormGrid on:submit={closeReportedModal}>
			<FormColumns>
				<FormColumn>
					<div class="grid grid-cols-[160px_1fr] gap-y-3">
						{#each [['Matched', reportedResult.matched, 'Rows that found a sent email'], ['Recorded', reportedResult.recorded, 'New reports saved'], ['Already reported', reportedResult.alreadyReported, 'Reported before, left as is'], ['Not matched', reportedResult.unmatched, 'No enrolled person or no email']] as [label, value, help]}
							<span class="text-grayblue-dark font-medium">{label}:</span>
							<span class="text-pc-darkblue dark:text-white">
								{#if value == null}
									<span title={HIDDEN_TIP} class="cursor-help underline decoration-dotted"
										>Hidden</span
									>
								{:else}
									{value}
								{/if}
								<span class="text-xs text-slate-500 dark:text-gray-400 ml-2">{help}</span>
							</span>
						{/each}
					</div>
				</FormColumn>
			</FormColumns>
			<FormFooter closeModal={closeReportedModal} closeText="Close" okText="Done" />
		</FormGrid>
	{:else}
		<FormGrid on:submit={onConfirmReported} isSubmitting={isReportedSubmitting}>
			<FormColumns>
				<FormColumn>
					<p class="text-sm text-gray-600 dark:text-gray-300 mb-4 transition-colors duration-200">
						Pick the column with the email addresses. A date column is optional; without it the
						report goes on each person's latest email.
					</p>
					<TextFieldSelect
						required
						id="program-reported-email-column"
						bind:value={reportedEmailColumn}
						options={reportedHeaders}
						placeholder="Select email column...">Email Column</TextFieldSelect
					>
					<TextFieldSelect
						id="program-reported-date-column"
						bind:value={reportedDateColumn}
						options={[NO_DATE, ...reportedHeaders]}
						placeholder="Select date column...">Date Column</TextFieldSelect
					>
				</FormColumn>
				<FormColumn overflowX={true}>
					<Table
						columns={reportedHeaders.map((h) => ({ column: h, size: 'small' }))}
						hasData={!!reportedPreview.length}
						hasNextPage={false}
						hasActions={false}
						plural="rows"
					>
						{#each reportedPreview as row}
							<TableRow>
								{#each row as cell}
									<TableCell value={cell} />
								{/each}
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<FormFooter
				closeModal={closeReportedModal}
				isSubmitting={isReportedSubmitting}
				okText="Upload"
			/>
		</FormGrid>
	{/if}
</Modal>
