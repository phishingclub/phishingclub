// sample data and insertable variables of the program report template. The
// sample matches the one the server previews a program report PDF with.

const line = (Label, Value) => ({ Label, Value });
const pct = (part, whole) => `${Math.round((part * 100) / whole)}%`;
// a result card as the server builds it: with a base its sub line is the
// share of that base, such as "26% of sent", without one it has no sub line
const card = (Label, value, Tone, base = null, baseLabel = '') => ({
	Label,
	Value: String(value),
	Sub: base ? `${pct(Math.min(value, base), base)} ${baseLabel}` : '',
	Tone,
	Withheld: false
});
// an outcome chart, a share of the recipients sent to
const outcome = (Label, value, Tone, sent = 118) => ({
	Label,
	Value: String(value),
	Sub: '',
	Tone,
	Withheld: false,
	Rate: (value * 100) / sent,
	HasRate: true
});
// a month or template row: counts with their percent of the sends, then the
// training sends, started with its percent of the training sends and
// completed with its percent of started. A count the row does not hold is "-".
const row = (name, sent, clicked, submitted, reported, tSent, tStarted, tCompleted) => {
	const count = (v) => (v === null ? '-' : String(v));
	const share = (part, whole) => (part === null || !whole ? '' : pct(part, whole));
	return {
		Cells: [
			name,
			count(sent),
			count(clicked),
			count(submitted),
			count(reported),
			count(tSent),
			count(tStarted),
			count(tCompleted)
		],
		Pcts: [
			'',
			'',
			share(clicked, sent),
			share(submitted, sent),
			share(reported, sent),
			'',
			share(tStarted, tSent),
			share(tCompleted, tStarted)
		]
	};
};
const bar = (rate, max) => ({
	Text: `${rate.toFixed(0)}%`,
	Height: (rate * 100) / max,
	Withheld: false
});

export const programReportSampleData = {
	Name: 'Sample Program',
	CompanyName: 'World Corp',
	Generated: new Date().toLocaleString(),
	Period: '1 Oct 2025 to 31 Dec 2025',
	Started: '1 Sep 2025',
	Closed: '',
	Anonymous: false,
	HiddenText: 'Hidden',
	MinGroup: 5,
	RepeatOffenderMonths: 12,
	Details: [
		line('Status', 'Active'),
		line('Anonymous', 'No'),
		line('Templates', '4: Password expiry, Parcel notice, Invoice reminder, Security training'),
		line('Template order', 'Random order per recipient'),
		line('After the last template', 'Start over'),
		line('Time between templates', '2 weeks'),
		line('Spread sends over', '1 week'),
		line('Sending days', 'Mon, Tue, Wed, Thu, Fri'),
		line('Sending hours', '09:00 to 16:00'),
		line('Skip dates', 'None'),
		line('First template to new members', 'After 2 weeks'),
		line('Start', '1 Sep 2025 09:00'),
		line('End', 'Runs until closed'),
		line('Groups', 'All staff')
	],
	Results: [
		card('Recipients', 120, 'neutral'),
		card('Sent', 118, 'info'),
		card('Clicked link', 31, 'warning', 118, 'of sent'),
		card('Data submitted', 11, 'danger', 118, 'of sent'),
		card('Reported', 46, 'safe', 118, 'of sent'),
		card('Training started', 34, 'training', 41, 'of training sent'),
		card('Training completed', 29, 'safe', 34, 'of started'),
		card('Repeat offenders', 6, 'danger')
	],
	Outcome: [
		outcome('Clicked link', 31, 'warning'),
		outcome('Data submitted', 11, 'danger'),
		outcome('Reported', 46, 'safe')
	],
	HasPhishing: true,
	HasTraining: true,
	HasSends: true,
	Templates: [
		row('Password expiry', 120, 24, 8, 25, null, null, null),
		row('Parcel notice', 98, 11, 3, 27, null, null, null),
		row('Invoice reminder', 18, 2, 1, 5, null, null, null),
		row('Security training', null, null, null, null, 41, 34, 29)
	],
	Months: [
		row('Oct 2025 (partial)', 80, 15, 5, 16, 12, 9, 7),
		row('Nov 2025', 78, 12, 4, 19, 14, 12, 10),
		row('Dec 2025', 78, 10, 3, 22, 15, 13, 12)
	],
	Trend: [
		{
			Label: 'Oct 2025',
			Partial: true,
			Clicked: bar((15 * 100) / 80, 30),
			Reported: bar((16 * 100) / 80, 30)
		},
		{
			Label: 'Nov 2025',
			Partial: false,
			Clicked: bar((12 * 100) / 78, 30),
			Reported: bar((19 * 100) / 78, 30)
		},
		{
			Label: 'Dec 2025',
			Partial: false,
			Clicked: bar((10 * 100) / 78, 30),
			Reported: bar((22 * 100) / 78, 30)
		}
	],
	TrendMax: 30
};

const linesLoop = (field) => `{{range .${field}}}<div>{{.Label}}: {{.Value}}</div>{{end}}`;

const cardsLoop = (field) =>
	`<div class="mcards">{{range .${field}}}
  <div class="mcard c-{{.Tone}}">
    <div class="mcard-label">{{.Label}}</div>
    <div class="mcard-value{{if .Withheld}} hidden{{end}}">{{.Value}}</div>
    <div class="mcard-sub">{{.Sub}}</div>
  </div>
{{end}}</div>`;

// a table of month or template rows, each count with its percent of the sends.
// The phishing columns come with phishing, the training columns with training.
const resultTable = (field, first) => `<table>
  <tr><th>${first}</th>{{if .HasPhishing}}<th>Sent</th><th>Clicked</th><th>Submitted</th><th>Reported</th>{{end}}{{if .HasTraining}}<th>Training sent</th><th>Training started</th><th>Training completed</th>{{end}}</tr>
  {{range .${field}}}{{$pcts := .Pcts}}<tr>{{range $i, $c := .Cells}}<td>{{$c}}{{with index $pcts $i}} ({{.}}){{end}}</td>{{end}}</tr>{{end}}
</table>`;

export const programReportCategories = {
	'Program Info': [
		{ label: 'Program Name', text: '{{.Name}}' },
		{ label: 'Company Name (if)', text: '{{if .CompanyName}}{{.CompanyName}}{{end}}' },
		{ label: 'Period', text: '{{.Period}}' },
		{ label: 'Generated', text: '{{.Generated}}' },
		{ label: 'Started (if)', text: '{{if .Started}}{{.Started}}{{end}}' },
		{ label: 'Closed (if)', text: '{{if .Closed}}{{.Closed}}{{end}}' },
		{
			label: 'Anonymous note (if)',
			text: `{{if .Anonymous}}Anonymous program. Results that could single out fewer than {{.MinGroup}} recipients are shown as {{.HiddenText}}.{{end}}`
		},
		{ label: 'Program details (loop)', text: linesLoop('Details') },
		{ label: 'Repeat offender months', text: '{{.RepeatOffenderMonths}}' },
		{ label: 'Has phishing (if)', text: '{{if .HasPhishing}}{{end}}' },
		{ label: 'Has training (if)', text: '{{if .HasTraining}}{{end}}' },
		{ label: 'Has sends in period (if)', text: '{{if .HasSends}}{{end}}' }
	],
	Results: [
		{ label: 'Results (loop)', text: cardsLoop('Results') },
		{
			label: 'Outcome rates (loop)',
			text: `{{range .Outcome}}<div>{{.Label}}: {{if .Withheld}}{{.Value}}{{else if .HasRate}}{{printf "%.0f" .Rate}}% of sent{{else}}-{{end}}</div>{{end}}`
		}
	],
	Tables: [
		{
			label: 'Trend chart data (loop)',
			text: `{{range .Trend}}<div>{{.Label}}{{if .Partial}} (partial){{end}}: Click Rate {{.Clicked.Text}}, Report Rate {{.Reported.Text}}</div>{{end}}`
		},
		{ label: 'By template (loop)', text: resultTable('Templates', 'Template') },
		{ label: 'By month (loop)', text: resultTable('Months', 'Month') }
	]
};

// the editor kind of a stored report template row: phishing, training or program
export const reportRowKind = (row) =>
	row?.isProgram ? 'program' : row?.isTraining ? 'training' : 'phishing';

// the API name of an editor kind
export const reportApiKind = (kind) => (kind === 'phishing' ? 'simulation' : kind);
