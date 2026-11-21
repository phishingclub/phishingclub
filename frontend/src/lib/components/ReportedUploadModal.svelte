<script>
	// Imports a CSV of reported emails for a campaign or a program.
	// Call pick() to choose a file. upload(file, emailColumn, dateColumn) sends
	// it and returns the API response with
	// {processed, skipped, alreadyReported, failed, errors: [{line, reason}]}.
	// onImported(data) runs after a successful import, before the result shows.
	import papaparse from 'papaparse';
	import Modal from '$lib/components/Modal.svelte';
	import FormGrid from '$lib/components/FormGrid.svelte';
	import FormColumns from '$lib/components/FormColumns.svelte';
	import FormColumn from '$lib/components/FormColumn.svelte';
	import FormFooter from '$lib/components/FormFooter.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import Button from '$lib/components/Button.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';
	import TableCellEmpty from '$lib/components/table/TableCellEmpty.svelte';
	import { addToast } from '$lib/store/toast';
	import { showIsLoading, hideIsLoading } from '$lib/store/loading.js';

	/** @type {(file: File, emailColumn: number, dateColumn: number|null) => Promise<any>} */
	export let upload;
	/** @type {(data: any) => Promise<void>|void} */
	export let onImported = () => {};
	// without a date column each report lands on the latest send
	export let dateOptional = false;
	export let id = 'reported';

	// the date select entry that sends no date column
	const NO_DATE = 'No date column';

	let fileInput;
	let isVisible = false;
	let isSubmitting = false;
	let file = null;
	let preview = [];
	// option labels for the column selects, one per column in file order, so the
	// label position is the column index sent to the server
	let columnLabels = [];
	let emailColumn = '';
	let dateColumn = '';
	// set when an import finished with failed rows, switches the modal to a result view
	let result = null;
	// whole file error from the server, shown in the form
	let error = '';
	$: errorsHeading = (() => {
		if (!result) return '';
		const shown = result.errors.length;
		const total = result.failed;
		if (total > shown) {
			return `Some rows could not be imported. First ${shown} of ${total} errors:`;
		}
		return `Some rows could not be imported. ${total === 1 ? 'Error' : 'Errors'}:`;
	})();

	export const pick = () => {
		fileInput?.click();
	};

	// build one unique label per column. Empty or repeated headers get the
	// column number added so each label points at exactly one column.
	const toColumnLabels = (headers) => {
		const counts = {};
		for (const h of headers) {
			const name = String(h ?? '').trim();
			counts[name] = (counts[name] || 0) + 1;
		}
		const used = new Set(dateOptional ? [NO_DATE] : []);
		return headers.map((h, i) => {
			const name = String(h ?? '').trim();
			let label = name;
			if (!name) {
				label = `(column ${i + 1})`;
			} else if (counts[name] > 1) {
				label = `${name} (column ${i + 1})`;
			}
			// a header could already look like a generated label, keep labels unique
			while (used.has(label)) {
				label = `${label} (column ${i + 1})`;
			}
			used.add(label);
			return label;
		});
	};

	const onPick = (event) => {
		const picked = event.target.files?.[0];
		if (!picked) return;

		if (!picked.name.toLowerCase().endsWith('.csv')) {
			addToast('Please select a CSV file', 'Error');
			event.target.value = '';
			return;
		}

		// parse csv file to extract headers and preview
		papaparse.parse(picked, {
			preview: 5, // preview first 5 rows
			skipEmptyLines: true,
			// the server only splits on commas, so the preview must too
			delimiter: ',',
			complete: (results) => {
				if (!results.data || results.data.length < 2) {
					addToast('CSV file must have headers and at least one data row', 'Error');
					event.target.value = '';
					return;
				}

				file = picked;
				preview = results.data.slice(1); // remaining rows are data preview
				columnLabels = toColumnLabels(results.data[0]);
				emailColumn = '';
				dateColumn = dateOptional ? NO_DATE : '';
				result = null;
				error = '';
				isVisible = true;

				// clear the file input
				event.target.value = '';
			},
			error: (err) => {
				addToast(`Failed to parse CSV: ${err.message}`, 'Error');
				event.target.value = '';
			}
		});
	};

	const close = () => {
		isVisible = false;
		file = null;
		preview = [];
		columnLabels = [];
		emailColumn = '';
		dateColumn = '';
		result = null;
		error = '';
	};

	const onConfirm = async () => {
		if (!file) return;
		error = '';

		// the selected label maps back to its column index
		const emailIndex = columnLabels.indexOf(emailColumn);
		const dateIndex = columnLabels.indexOf(dateColumn);
		if (emailIndex < 0) {
			error = dateOptional
				? 'Select the email column from the list'
				: 'Select both the email and the date column from the list';
			return;
		}
		if (dateIndex < 0 && !(dateOptional && dateColumn === NO_DATE)) {
			error = dateOptional
				? 'Select the date column from the list'
				: 'Select both the email and the date column from the list';
			return;
		}

		try {
			isSubmitting = true;
			showIsLoading();
			const res = await upload(file, emailIndex, dateIndex >= 0 ? dateIndex : null);
			if (!res.success) {
				error = res.error || `Import failed (HTTP ${res.statusCode})`;
				return;
			}
			const data = res.data || {};
			await onImported(data);

			const processed = data.processed || 0;
			const already = data.alreadyReported || 0;
			const failed = data.failed || 0;
			if (failed === 0) {
				let msg = `Marked ${processed} ${processed === 1 ? 'recipient' : 'recipients'} as reported`;
				if (already > 0) {
					msg += `, ${already} already reported`;
				}
				addToast(msg, 'Success');
				close();
				return;
			}
			result = {
				processed,
				alreadyReported: already,
				failed,
				errors: Array.isArray(data.errors) ? data.errors : []
			};
		} catch (e) {
			console.error('Upload error:', e);
			error = 'Network error: failed to upload the CSV file';
		} finally {
			isSubmitting = false;
			hideIsLoading();
		}
	};
</script>

<input
	bind:this={fileInput}
	type="file"
	accept=".csv"
	class="hidden"
	tabindex="-1"
	aria-hidden="true"
	on:change={onPick}
/>

<Modal
	headerText="Import reported emails"
	visible={isVisible}
	onClose={close}
	{isSubmitting}
	noAutoFocus={true}
>
	{#if result}
		<div class="p-6 max-h-[80vh] overflow-y-auto text-gray-700 dark:text-gray-300">
			<div class="space-y-6">
				<ul class="space-y-1">
					<li>Marked as reported: {result.processed}</li>
					{#if result.alreadyReported > 0}
						<li>Already reported: {result.alreadyReported}</li>
					{/if}
					<li>Could not be imported: {result.failed}</li>
				</ul>

				<div class="border-t border-gray-200 dark:border-gray-700 pt-6">
					{#if result.errors.length > 0}
						<p class="text-sm">{errorsHeading}</p>
						<div
							class="mt-3 bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-2 space-y-1"
						>
							{#each result.errors as err}
								<div class="py-2 px-3 text-sm text-gray-900 dark:text-gray-100 break-words">
									<span class="font-semibold">Line {err.line}:</span>
									{err.reason}
								</div>
							{/each}
						</div>
					{:else}
						<p class="text-sm">Some rows could not be imported.</p>
					{/if}
					<p class="mt-3 text-sm text-gray-500 dark:text-gray-400">
						Fix these rows in the file and import it again. Rows already marked as reported are not
						counted twice.
					</p>
				</div>
			</div>
			<div class="mt-6 flex justify-end">
				<Button on:click={close}>Close</Button>
			</div>
		</div>
	{:else}
		<FormGrid on:submit={onConfirm} {isSubmitting}>
			<FormColumns>
				<FormColumn>
					<p class="text-sm text-gray-600 dark:text-gray-300 mb-4 transition-colors duration-200">
						{#if dateOptional}
							Pick the columns with the email address and the report date. Without a date column
							the report lands on the latest send.
						{:else}
							Pick the columns with the email address and the report date.
						{/if}
					</p>

					<TextFieldSelect
						required
						id="{id}-email-column"
						bind:value={emailColumn}
						options={columnLabels}
						placeholder="Select email column...">Email Column</TextFieldSelect
					>

					<TextFieldSelect
						required={!dateOptional}
						id="{id}-date-column"
						bind:value={dateColumn}
						options={dateOptional ? [NO_DATE, ...columnLabels] : columnLabels}
						placeholder="Select date column...">Date Column</TextFieldSelect
					>
				</FormColumn>
				<FormColumn overflowX={true}>
					<Table
						columns={columnLabels.map((h) => ({ column: h, size: 'small' }))}
						hasData={!!preview.length}
						plural="rows"
					>
						{#each preview as row}
							<TableRow>
								{#each row as cell}
									<TableCell value={cell} />
								{/each}
								<TableCellEmpty />
							</TableRow>
						{/each}
					</Table>
				</FormColumn>
			</FormColumns>
			<div class="px-1 sm:px-6 pb-4">
				<details class="text-sm text-gray-600 dark:text-gray-300 group">
					<summary
						class="cursor-pointer select-none font-semibold text-pc-darkblue dark:text-white list-none flex items-center gap-2"
					>
						<svg
							class="w-4 h-4 transition-transform group-open:rotate-90"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
							aria-hidden="true"
						>
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
						</svg>
						Supported date formats
					</summary>
					<ul class="mt-2 ml-6 space-y-1 list-disc">
						<li>
							ISO 8601: <code>2024-03-05T14:30:00Z</code>,
							<code>2024-03-05T14:30:00+02:00</code> or <code>2024-03-05T14:30:00</code>. Fractional
							seconds are fine.
						</li>
						<li>
							<code>YYYY-MM-DD HH:MM:SS</code>, <code>YYYY-MM-DD HH:MM</code>,
							<code>YYYY-MM-DD</code>
						</li>
						<li>
							<code>MM/DD/YYYY</code> month first, e.g. <code>3/5/2024 9:05</code>
						</li>
						<li><code>DD-MM-YYYY</code> day first</li>
						<li><code>YYYY/MM/DD</code></li>
					</ul>
					<p class="mt-2">
						The last three take an optional <code>HH:MM</code> or <code>HH:MM:SS</code> time. Leading
						zeros are optional in <code>MM/DD/YYYY</code> and <code>DD-MM-YYYY</code>. A time without
						a zone is read as UTC.
						<code>DD/MM/YYYY</code> with slashes is read month first, so use dashes for day first.
					</p>
				</details>
			</div>
			{#if error}
				<div class="mt-4">
					<FormError message={error} />
				</div>
			{/if}
			<FormFooter closeModal={close} {isSubmitting} okText="Import" />
		</FormGrid>
	{/if}
</Modal>
