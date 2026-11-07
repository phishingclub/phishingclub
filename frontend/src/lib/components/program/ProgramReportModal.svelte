<script>
	// Asks for the period of a program PDF report and downloads it.
	// Call open() to show it; when PDF reports are off a notice is shown instead.
	import { api } from '$lib/api/apiProxy.js';
	import Alert from '$lib/components/Alert.svelte';
	import SelectSquare from '$lib/components/SelectSquare.svelte';
	import { browserTimeZone, toZoneLocal } from '$lib/utils/programText.js';

	export let programID;
	export let programName = '';
	export let enabled = false;
	// an anonymous report covers whole calendar months in the program zone
	export let anonymous = false;
	export let timeZone = '';

	const DAY_MS = 86400000;
	const reportPeriods = [
		{ value: '30', label: 'Last 30 days' },
		{ value: '90', label: 'Last 90 days' },
		{ value: '365', label: 'Last 12 months' },
		{ value: 'all', label: 'All time' }
	];

	// the value is the number of whole months before the current month
	const monthPeriods = [
		{ value: 'm1', label: 'Last full month' },
		{ value: 'm3', label: 'Last 3 full months' },
		{ value: 'm12', label: 'Last 12 full months' },
		{ value: 'all', label: 'All time' }
	];

	let isReportAlertVisible = false;
	let isReportDisabledVisible = false;
	let reportPeriod = '30';

	$: periods = anonymous ? monthPeriods : reportPeriods;

	export const open = () => {
		if (!enabled) {
			isReportDisabledVisible = true;
			return;
		}
		reportPeriod = anonymous ? 'm1' : '30';
		isReportAlertVisible = true;
	};

	// the first day of the month a number of months before the current month
	// in the program zone, as "YYYY-MM-DD"
	const monthStart = (monthsBack) => {
		const today = toZoneLocal(new Date(), timeZone || browserTimeZone);
		const [y, m] = today.slice(0, 7).split('-').map(Number);
		return new Date(Date.UTC(y, m - 1 - monthsBack, 1)).toISOString().slice(0, 10);
	};

	// from and to of the chosen period; to is left out for a rolling period
	const periodRange = (period) => {
		if (period === 'all') return { from: '', to: '' };
		if (period.startsWith('m')) {
			return { from: monthStart(parseInt(period.slice(1))), to: monthStart(0) };
		}
		return { from: new Date(Date.now() - parseInt(period) * DAY_MS).toISOString(), to: '' };
	};

	const onConfirmReport = async () => {
		const { from, to } = periodRange(reportPeriod);
		try {
			const res = await api.program.reportPDF(programID, from, to);
			if (!res.success && /not found/i.test(res.error || '')) {
				return { success: false, error: 'PDF reports are not enabled. Enable them in Settings.' };
			}
			return res;
		} catch (e) {
			console.error('failed to generate program report', e);
			return { success: false, error: 'Failed to generate the program report' };
		}
	};
</script>

<Alert
	headline="generate report"
	bind:visible={isReportAlertVisible}
	onConfirm={onConfirmReport}
	ok="Generate"
	tone="affirmative"
>
	<div class="mt-4 text-gray-700 dark:text-gray-200">
		Generate a PDF report for <strong>{programName}</strong>. <br />The report will download
		automatically.
	</div>
	<SelectSquare label="Period" width="small" options={periods} bind:value={reportPeriod} />
	{#if anonymous}
		<p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
			Anonymous reports cover whole calendar months.
		</p>
	{/if}
</Alert>
<Alert
	headline="PDF reports disabled"
	bind:visible={isReportDisabledVisible}
	onConfirm={() => {
		isReportDisabledVisible = false;
		return { success: true };
	}}
	noCancel={true}
	ok="OK"
>
	<div class="mt-4 text-gray-700 dark:text-gray-200">
		PDF report generation is not enabled. <br />Enable it in <strong>Settings</strong> under PDF Reports.
	</div>
</Alert>
