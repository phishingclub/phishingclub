<script context="module">
	import * as monacoModule from 'monaco-editor';

	// Registered once for the whole app. The providers are global to the html
	// language, so a module level guard keeps a second editor from adding a
	// duplicate. rb is the client that {{RemoteBrowserScript "name"}} injects into
	// a phishing page, so the features are only offered inside a <script> block.
	let rbFeaturesRegistered = false;

	// One source of truth for both completion and hover.
	const RB_OBJECT_DETAIL = 'Remote Browser client (window.rb)';
	const RB_OBJECT_DOC =
		'`rb` (alias `window.rb`). Client injected by `{{RemoteBrowserScript "name"}}`. Connects the phishing page to the remote browser session over WebSocket.\n\nMethods: `on(event, handler)`, `send(event, data)`, `mountStream(name, element)`.';

	const RB_MEMBERS = [
		{
			name: 'on',
			detail: '(event: string, handler: (data) => void): void',
			insertText: "on('${1:event}', function (data) {\n\t$0\n})",
			doc: 'Subscribe to an event the remote browser script sends with `emit()`.\n\nStream form: `rb.on("stream_start", name, handler)` and `rb.on("stream_stop", name, handler)` fire when a named stream starts or stops.'
		},
		{
			name: 'send',
			detail: '(event: string, data?: any): void',
			insertText: "send('${1:event}'${2:, data})",
			doc: 'Send an event and optional data to the running remote browser script. The script receives it through `waitForEvent()` or `s.on()`.'
		},
		{
			name: 'mountStream',
			detail: '(name: string, element: HTMLElement, opts?: object): void',
			insertText: "mountStream('${1:name}', ${2:element})",
			doc: 'Mount a named element stream the script started with `s.stream(selector, name)` into a page element, for example a captcha. Forwards mouse and keyboard input back to the streamed element.'
		}
	];

	// beforeText is the source from the document start up to a point. True when
	// that point sits inside an open <script> block.
	function rbInScript(beforeText) {
		const openIdx = beforeText.lastIndexOf('<script');
		const closeIdx = beforeText.lastIndexOf('</script');
		return openIdx !== -1 && openIdx >= closeIdx;
	}

	function ensureRbLanguageFeatures() {
		if (rbFeaturesRegistered) return;
		rbFeaturesRegistered = true;
		const Kind = monacoModule.languages.CompletionItemKind;
		const AsSnippet = monacoModule.languages.CompletionItemInsertTextRule.InsertAsSnippet;

		monacoModule.languages.registerCompletionItemProvider('html', {
			triggerCharacters: ['.'],
			provideCompletionItems(model, position) {
				const before = model.getValueInRange({
					startLineNumber: 1,
					startColumn: 1,
					endLineNumber: position.lineNumber,
					endColumn: position.column
				});
				if (!rbInScript(before)) {
					return { suggestions: [] };
				}

				const lineBefore = before.slice(before.lastIndexOf('\n') + 1);
				const word = model.getWordUntilPosition(position);
				const range = {
					startLineNumber: position.lineNumber,
					endLineNumber: position.lineNumber,
					startColumn: word.startColumn,
					endColumn: word.endColumn
				};

				// member access on rb / window.rb / remoteBrowser
				if (/(?:^|[^\w$.])(?:window\s*\.\s*)?(?:rb|remoteBrowser)\s*\.\s*[\w$]*$/.test(lineBefore)) {
					return {
						suggestions: RB_MEMBERS.map((m) => ({
							label: m.name,
							kind: Kind.Method,
							insertText: m.insertText,
							insertTextRules: AsSnippet,
							range,
							detail: m.detail,
							documentation: { value: m.doc }
						}))
					};
				}

				// bare identifier: offer the rb object, but not right after another dot
				const uptoWord = lineBefore.slice(0, lineBefore.length - word.word.length);
				if (/\.\s*$/.test(uptoWord)) {
					return { suggestions: [] };
				}
				return {
					suggestions: [
						{
							label: 'rb',
							kind: Kind.Variable,
							insertText: 'rb',
							range,
							detail: RB_OBJECT_DETAIL,
							documentation: { value: RB_OBJECT_DOC }
						}
					]
				};
			}
		});

		monacoModule.languages.registerHoverProvider('html', {
			provideHover(model, position) {
				const before = model.getValueInRange({
					startLineNumber: 1,
					startColumn: 1,
					endLineNumber: position.lineNumber,
					endColumn: position.column
				});
				if (!rbInScript(before)) return null;

				const w = model.getWordAtPosition(position);
				if (!w) return null;
				const lineContent = model.getLineContent(position.lineNumber);
				const beforeWord = lineContent.slice(0, w.startColumn - 1);
				const range = new monacoModule.Range(
					position.lineNumber,
					w.startColumn,
					position.lineNumber,
					w.endColumn
				);

				// rb / window.rb / remoteBrowser object itself
				if (w.word === 'rb' || w.word === 'remoteBrowser') {
					const endsWithDot = /\.\s*$/.test(beforeWord);
					const isWindowRb = /(?:^|[^\w$])window\s*\.\s*$/.test(beforeWord);
					if (endsWithDot && !isWindowRb) return null;
					return { range, contents: [{ value: RB_OBJECT_DOC }] };
				}

				// a member accessed on rb
				const m = RB_MEMBERS.find((x) => x.name === w.word);
				if (
					m &&
					/(?:^|[^\w$.])(?:window\s*\.\s*)?(?:rb|remoteBrowser)\s*\.\s*$/.test(beforeWord)
				) {
					return {
						range,
						contents: [{ value: '```js\nrb.' + w.word + m.detail + '\n```' }, { value: m.doc }]
					};
				}
				return null;
			}
		});
	}
</script>

<script>
	import { onMount, tick } from 'svelte';
	import * as monaco from 'monaco-editor';
	import { pcDarkTheme } from './monacoTheme.js';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
	import * as vimModule from 'monaco-vim';
	import { BiMap } from '$lib/utils/maps';
	import { previewQR as generateQR } from '$lib/utils/qrPreview';
	import { submitPreviewForm } from '$lib/utils/safePreview.js';
	import { vimModeEnabled } from '$lib/store/vimMode.js';
	import {
		setupVimClipboardIntegration,
		destroyVimClipboardIntegration
	} from '$lib/utils/vimClipboard.js';
	import { displayMode, DISPLAY_MODE } from '$lib/store/displayMode';

	/** @type {'domain'|'page'|'email'|'report'} */
	export let contentType;
	export let value;
	export let baseURL = 'example.test';
	export let domainMap = new BiMap({});
	export let selectedDomain = '';
	export let externalVimMode = null; // allow external control of vim mode
	export let attachments = []; // array of email attachments for inline image preview
	// height of the editor and preview area when not expanded, any CSS length
	export let height = 'max(16rem, min(55vh, calc(100dvh - 20rem)))';
	let localVimMode = externalVimMode !== null ? externalVimMode : $vimModeEnabled;
	let editor = null;
	let previewFrame = null;
	// unique name so the preview form can target this editor's iframe
	let previewFrameName = 'pc-preview-' + Math.random().toString(36).slice(2);
	let previewRenderDelayID = null;
	let previewRenderDelay = 250;
	let isRenderingPreview = false;
	let previousQRCode = '';
	let previousQRHash = 0;

	let isPreviewVisible = false;
	let fileInputRef;
	let shadowContainer = null;
	let vimStatusBar = null;
	let isDestroyed = false;
	let editorContainer = null;
	let isExpanded = false;

	const apiTemplates = [
		{ label: 'Custom Field 1', text: '{{.CustomField1}}' },
		{ label: 'Custom Field 2', text: '{{.CustomField2}}' },
		{ label: 'Custom Field 3', text: '{{.CustomField3}}' },
		{ label: 'Custom Field 4', text: '{{.CustomField4}}' }
	];
	const emailTemplates = [
		{ label: 'Tracker', text: '{{.Tracker}}' },
		{ label: 'Tracking URL', text: '{{.TrackingURL}}' }
	];

	const templates = {
		Email: [
			{ label: 'To', text: '{{.To}}' },
			{ label: 'From', text: '{{.From}}' }
		],
		Recipient: [
			{ label: 'FirstName', text: '{{.FirstName}}' },
			{ label: 'LastName', text: '{{.LastName}}' },
			{ label: 'Email', text: '{{.Email}}' },
			{ label: 'Phone', text: '{{.Phone}}' },
			{ label: 'Position', text: '{{.Position}}' },
			{ label: 'Department', text: '{{.Department}}' },
			{ label: 'City', text: '{{.City}}' },
			{ label: 'Country', text: '{{.Country}}' },
			{ label: 'Misc', text: '{{.Misc}}' }
		],
		'URLs & Tracking': [
			{ label: 'Base URL', text: '{{.BaseURL}}' },
			{ label: 'URL', text: '{{.URL}}' },
			{ label: 'Before Landing Page URL', text: '{{.BeforeLandingPageURL}}' },
			{ label: 'Landing Page URL', text: '{{.LandingPageURL}}' },
			{ label: 'After Landing Page URL', text: '{{.AfterLandingPageURL}}' }
		],
		Functions: [
			{ label: 'URL as QR HTML', text: '{{qr .URL 4}}' },
			{ label: 'URL escape', text: '{{urlEscape "content" }}' },
			{ label: 'Random letters', text: '{{randAlpha 8}}' },
			{ label: 'Random number', text: '{{randInt 1 4}}' },
			{ label: 'Date', text: '{{date "Y-m-d H:i:s" 0}}' },
			{ label: 'Base64', text: '{{base64 "text"}}' },
			{ label: 'Multiply', text: '{{mul 1.0 1.0}}' }
		]
	};

	// device code templates are only available in blackbox (red team phishing) mode
	const deviceCodeTemplates = [
		{ label: 'User Code', text: '{{MicrosoftDeviceCode}}' },
		{ label: 'Verification URL', text: '{{MicrosoftDeviceCodeURL}}' },
		{ label: 'Captured', text: '{{DeviceCodeCaptured}}' }
	];

	$: computedTemplates = (() => {
		const result = { ...templates };
		if ($displayMode === DISPLAY_MODE.BLACKBOX && contentType !== 'report') {
			result['Device Code'] = deviceCodeTemplates;
		}
		if ($displayMode === DISPLAY_MODE.BLACKBOX && contentType === 'page') {
			result['Remote Browser'] = [
				{ label: 'Script', text: '{{RemoteBrowserScript "remote_browser_name"}}' }
			];
		}
		return result;
	})();

	const reportCategories = {
		'Campaign Info': [
			{ label: 'Campaign Name', text: '{{.CampaignName}}' },
			{ label: 'Company Name', text: '{{.CompanyName}}' },
			{ label: 'Report Date', text: '{{.ReportDate}}' },
			{ label: 'Start Date', text: '{{.CampaignStartDate}}' },
			{ label: 'End Date', text: '{{.CampaignEndDate}}' },
			{ label: 'Closed At', text: '{{.CampaignClosedAt}}' },
			{ label: 'Total Targets', text: '{{.TotalTargets}}' }
		],
		Results: [
			{ label: 'Emails Sent', text: '{{.EmailsSent}}' },
			{ label: 'Emails Opened', text: '{{.EmailsOpened}}' },
			{ label: 'Clicked (count)', text: '{{.ResultClicked}}' },
			{ label: 'Clicked (% of total)', text: '{{.ResultClickedPercent}}' },
			{ label: 'Submitted (count)', text: '{{.ResultSubmitted}}' },
			{ label: 'Submitted (% of total)', text: '{{.ResultSubmittedPercent}}' },
			{ label: 'Reported (count)', text: '{{.ResultReported}}' },
			{ label: 'Reported (% of total)', text: '{{.ResultReportedPercent}}' }
		],
		'Conversion & Rates': [
			{ label: 'Opened of sent (%)', text: '{{.OpenedOfSent}}' },
			{ label: 'Clicked of opened (%)', text: '{{.ClickedOfOpened}}' },
			{ label: 'Submitted of clicked (%)', text: '{{.SubmittedOfClicked}}' },
			{ label: 'Sent rate (0-100)', text: '{{.SentRate}}' },
			{ label: 'Open rate (0-100)', text: '{{.OpenRate}}' },
			{ label: 'Click rate (0-100)', text: '{{.ClickRate}}' },
			{ label: 'Submit rate (0-100)', text: '{{.SubmitRate}}' },
			{ label: 'Report rate (0-100)', text: '{{.ReportRate}}' }
		],
		'Awareness Training': [
			{ label: 'Is training campaign', text: '{{.IsTraining}}' },
			{ label: 'Training Started (count)', text: '{{.TrainingStarted}}' },
			{ label: 'Training Started (% of total)', text: '{{.TrainingStartedPercent}}' },
			{ label: 'Training Started rate (0-100)', text: '{{.TrainingStartedRate}}' },
			{ label: 'Training Completed (count)', text: '{{.TrainingCompleted}}' },
			{ label: 'Training Completed (% of total)', text: '{{.TrainingCompletedPercent}}' },
			{ label: 'Training Completed rate (0-100)', text: '{{.TrainingCompletedRate}}' },
			{ label: 'Started of opened (%)', text: '{{.StartedOfOpened}}' },
			{ label: 'Completed of started (%)', text: '{{.CompletedOfStarted}}' }
		],
		'Group Breakdown': [
			{ label: 'Grouped By (default dimension name)', text: '{{.GroupsBy}}' },
			{
				label: 'Group rows: default dimension (loop)',
				text: `{{range .Groups}}
<tr>
  <td>{{.Group}}</td>
  <td>{{.Total}}</td>
  {{if .Suppressed}}
  <td colspan="3">Hidden (group too small)</td>
  {{else}}
  <td>{{if lt .Clicked 0}}-{{else}}{{.Clicked}} ({{.ClickedPercent}}%){{end}}</td>
  <td>{{if lt .Submitted 0}}-{{else}}{{.Submitted}} ({{.SubmittedPercent}}%){{end}}</td>
  <td>{{if lt .Reported 0}}-{{else}}{{.Reported}} ({{.ReportedPercent}}%){{end}}</td>
  {{end}}
</tr>
{{end}}`
			},
			{
				label: 'Group rows: by department (loop)',
				text: `{{range .DepartmentGroups}}
<tr>
  <td>{{.Group}}</td>
  <td>{{.Total}}</td>
  {{if .Suppressed}}
  <td colspan="3">Hidden (group too small)</td>
  {{else}}
  <td>{{if lt .Clicked 0}}-{{else}}{{.Clicked}} ({{.ClickedPercent}}%){{end}}</td>
  <td>{{if lt .Submitted 0}}-{{else}}{{.Submitted}} ({{.SubmittedPercent}}%){{end}}</td>
  <td>{{if lt .Reported 0}}-{{else}}{{.Reported}} ({{.ReportedPercent}}%){{end}}</td>
  {{end}}
</tr>
{{end}}`
			},
			{
				label: 'Group rows: by position (loop)',
				text: `{{range .PositionGroups}}
<tr>
  <td>{{.Group}}</td>
  <td>{{.Total}}</td>
  {{if .Suppressed}}
  <td colspan="3">Hidden (group too small)</td>
  {{else}}
  <td>{{if lt .Clicked 0}}-{{else}}{{.Clicked}} ({{.ClickedPercent}}%){{end}}</td>
  <td>{{if lt .Submitted 0}}-{{else}}{{.Submitted}} ({{.SubmittedPercent}}%){{end}}</td>
  <td>{{if lt .Reported 0}}-{{else}}{{.Reported}} ({{.ReportedPercent}}%){{end}}</td>
  {{end}}
</tr>
{{end}}`
			},
			{
				label: 'Group rows: training (loop)',
				text: `{{range .Groups}}
<tr>
  <td>{{.Group}}</td>
  <td>{{.Total}}</td>
  {{if .Suppressed}}
  <td colspan="3">Hidden (group too small)</td>
  {{else}}
  <td>{{if lt .Clicked 0}}-{{else}}{{.Clicked}} ({{.ClickedPercent}}%){{end}}</td>
  <td>{{if lt .TrainingStarted 0}}-{{else}}{{.TrainingStarted}} ({{.TrainingStartedPercent}}%){{end}}</td>
  <td>{{if lt .TrainingCompleted 0}}-{{else}}{{.TrainingCompleted}} ({{.TrainingCompletedPercent}}%){{end}}</td>
  {{end}}
</tr>
{{end}}`
			}
		],
		Screenshots: [
			{
				label: 'Email Screenshot',
				text: '{{if .EmailScreenshot}}<img src="{{.EmailScreenshot}}" style="width:100%">{{end}}'
			},
			{ label: 'Email From', text: '{{.EmailFrom}}' },
			{ label: 'Email Subject', text: '{{.EmailSubject}}' },
			{
				label: 'Before Landing Page Screenshot',
				text: '{{if .BeforeLandingPageScreenshot}}<img src="{{.BeforeLandingPageScreenshot}}" style="width:100%">{{end}}'
			},
			{
				label: 'Landing Page Screenshot',
				text: '{{if .LandingPageScreenshot}}<img src="{{.LandingPageScreenshot}}" style="width:100%">{{end}}'
			},
			{
				label: 'After Landing Page Screenshot',
				text: '{{if .AfterLandingPageScreenshot}}<img src="{{.AfterLandingPageScreenshot}}" style="width:100%">{{end}}'
			},
			{ label: 'Template Count', text: '{{.TemplateCount}}' },
			{
				label: 'More than one template (if)',
				text: `{{if gt .TemplateCount 1}}

{{else}}

{{end}}`
			},
			{
				label: 'Screenshots per template (loop)',
				text: `{{range $t := .TemplateScreenshots}}
<h2>Template {{$t.Number}} of {{$.TemplateCount}}: {{$t.Name}}</h2>
{{if $t.Email}}<p>From: {{$t.EmailFrom}}<br>Subject: {{$t.EmailSubject}}</p>
<img src="{{$t.Email}}" style="width:100%">{{end}}
{{if $t.BeforeLandingPage}}<img src="{{$t.BeforeLandingPage}}" style="width:100%">{{end}}
{{if $t.LandingPage}}<img src="{{$t.LandingPage}}" style="width:100%">{{end}}
{{if $t.AfterLandingPage}}<img src="{{$t.AfterLandingPage}}" style="width:100%">{{end}}
{{end}}`
			}
		],
		Recipients: [
			{
				label: 'Recipient rows (loop)',
				text: `{{range .Recipients}}
<tr>
  <td>{{.FirstName}} {{.LastName}}</td>
  <td>{{.Email}}</td>
  <td>{{.Department}}</td>
  <td>{{.Position}}</td>
  <td>{{if .ClickedLink}}Yes{{else}}No{{end}}</td>
  <td>{{if .SubmittedData}}Yes{{else}}No{{end}}</td>
  <td>{{if .Reported}}Yes{{else}}No{{end}}</td>
</tr>
{{end}}`
			}
		]
	};

	// sample data for the live preview only. the real report is rendered by Go's
	// text/template on the server; the preview engine below imitates the subset it
	// uses (fields, $ and range variables, if/else, range, compare and printf) so
	// the preview shows values, not raw tags.
	const sampleDepartmentGroups = [
		{ Group: 'Engineering', Total: 70, Clicked: 21, ClickedPercent: '30', Submitted: 9, SubmittedPercent: '13', Reported: 11, ReportedPercent: '16', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: false },
		{ Group: 'Sales', Total: 49, Clicked: 20, ClickedPercent: '41', Submitted: 10, SubmittedPercent: '20', Reported: 4, ReportedPercent: '8', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: false },
		{ Group: 'Other (small groups)', Total: 1, Clicked: 0, ClickedPercent: '0', Submitted: 0, SubmittedPercent: '0', Reported: 0, ReportedPercent: '0', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: true }
	];
	const samplePositionGroups = [
		{ Group: 'Head of operations', Total: 40, Clicked: 12, ClickedPercent: '30', Submitted: 5, SubmittedPercent: '13', Reported: 6, ReportedPercent: '15', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: false },
		{ Group: 'Account Executive', Total: 35, Clicked: 14, ClickedPercent: '40', Submitted: 7, SubmittedPercent: '20', Reported: 3, ReportedPercent: '9', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: false },
		{ Group: 'Other (small groups)', Total: 1, Clicked: 0, ClickedPercent: '0', Submitted: 0, SubmittedPercent: '0', Reported: 0, ReportedPercent: '0', TrainingStarted: 0, TrainingStartedPercent: '0', TrainingCompleted: 0, TrainingCompletedPercent: '0', Suppressed: true }
	];
	// neutral placeholder image standing in for a screenshot in the preview.
	// the label is fixed text from this file, never user input
	const screenshotPlaceholder = (label) =>
		'data:image/svg+xml,' +
		encodeURIComponent(
			`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="500" viewBox="0 0 800 500">` +
				`<rect width="800" height="500" fill="#f3f4f6"/>` +
				`<rect x="12" y="12" width="776" height="476" fill="none" stroke="#d1d5db" stroke-width="2" stroke-dasharray="10 8"/>` +
				`<text x="400" y="240" text-anchor="middle" font-family="sans-serif" font-size="30" fill="#6b7280">${label}</text>` +
				`<text x="400" y="282" text-anchor="middle" font-family="sans-serif" font-size="18" fill="#9ca3af">Sample placeholder</text>` +
				`</svg>`
		);
	const sampleTemplateScreenshots = [1, 2].map((n) => ({
		Number: n,
		Name: `Sample template ${n}`,
		Email: screenshotPlaceholder(`Template ${n}: email screenshot`),
		EmailFrom: `it-support@sample${n}.test`,
		EmailSubject: `Sample subject for template ${n}`,
		BeforeLandingPage: screenshotPlaceholder(`Template ${n}: before landing page screenshot`),
		LandingPage: screenshotPlaceholder(`Template ${n}: landing page screenshot`),
		AfterLandingPage: screenshotPlaceholder(`Template ${n}: after landing page screenshot`)
	}));
	const reportSampleData = {
		CampaignName: 'Q1 Phishing Simulation',
		CompanyName: 'World Corp',
		ReportDate: new Date().toLocaleDateString(),
		CampaignStartDate: '2025-01-01',
		CampaignEndDate: '2025-01-31',
		CampaignClosedAt: '2025-02-01',
		TotalTargets: 120,
		EmailsSent: 118,
		EmailsOpened: 74,
		ResultClicked: 32,
		ResultClickedPercent: '27.1',
		ResultSubmitted: 14,
		ResultSubmittedPercent: '11.9',
		ResultReported: 8,
		ResultReportedPercent: '6.8',
		OpenedOfSent: '62.7',
		ClickedOfOpened: '43.2',
		SubmittedOfClicked: '43.8',
		SentRate: 98.3,
		OpenRate: 61.7,
		ClickRate: 26.7,
		SubmitRate: 11.7,
		ReportRate: 6.7,
		IsTraining: false,
		TrainingStarted: 0,
		TrainingCompleted: 0,
		TrainingStartedPercent: '0',
		TrainingCompletedPercent: '0',
		TrainingStartedRate: 0,
		TrainingCompletedRate: 0,
		StartedOfOpened: '0',
		CompletedOfStarted: '0',
		GroupsBy: 'Department',
		Groups: sampleDepartmentGroups,
		DepartmentGroups: sampleDepartmentGroups,
		PositionGroups: samplePositionGroups,
		// two templates, so the default report previews its per template section
		TemplateCount: sampleTemplateScreenshots.length,
		TemplateScreenshots: sampleTemplateScreenshots,
		EmailScreenshot: sampleTemplateScreenshots[0].Email,
		EmailFrom: sampleTemplateScreenshots[0].EmailFrom,
		EmailSubject: sampleTemplateScreenshots[0].EmailSubject,
		BeforeLandingPageScreenshot: sampleTemplateScreenshots[0].BeforeLandingPage,
		LandingPageScreenshot: sampleTemplateScreenshots[0].LandingPage,
		AfterLandingPageScreenshot: sampleTemplateScreenshots[0].AfterLandingPage,
		Recipients: [
			{ FirstName: 'Alice', LastName: 'Andersen', Email: 'alice@worldcorp.test', Department: 'Research and Development', Position: 'Head of operations', ClickedLink: true, SubmittedData: true, Reported: false },
			{ FirstName: 'Bob', LastName: 'Berg', Email: 'bob@worldcorp.test', Department: 'Sales', Position: 'Account Executive', ClickedLink: true, SubmittedData: false, Reported: false },
			{ FirstName: 'Carol', LastName: 'Chan', Email: 'carol@worldcorp.test', Department: 'Finance', Position: 'Analyst', ClickedLink: false, SubmittedData: false, Reported: true }
		]
	};

	// format a value the way Go's printf "%.Nf" would, for the preview only
	function goFormatFloat(fmt, val) {
		const m = /%\.(\d+)f/.exec(fmt);
		if (m) return (Number(val) || 0).toFixed(Number(m[1]));
		return String(val ?? '');
	}

	// Go template truthiness: false, 0, "", nil and empty lists are false
	function reportTruthy(v) {
		if (Array.isArray(v)) return v.length > 0;
		if (v !== null && typeof v === 'object') return Object.keys(v).length > 0;
		return !!v;
	}

	// the template functions the preview understands. pure value functions, no
	// code execution; anything else is left in the preview as written
	const reportFuncs = {
		not: (a) => !reportTruthy(a),
		and: (...a) => {
			const i = a.findIndex((x) => !reportTruthy(x));
			return i === -1 ? a[a.length - 1] : a[i];
		},
		or: (...a) => {
			const i = a.findIndex((x) => reportTruthy(x));
			return i === -1 ? a[a.length - 1] : a[i];
		},
		eq: (a, b) => a === b || String(a) === String(b),
		ne: (a, b) => !(a === b || String(a) === String(b)),
		lt: (a, b) => (Number(a) || 0) < (Number(b) || 0),
		le: (a, b) => (Number(a) || 0) <= (Number(b) || 0),
		gt: (a, b) => (Number(a) || 0) > (Number(b) || 0),
		ge: (a, b) => (Number(a) || 0) >= (Number(b) || 0),
		len: (a) => (Array.isArray(a) || typeof a === 'string' ? a.length : 0),
		mul: (a, b) => (Number(a) || 0) * (Number(b) || 0),
		printf: (fmt, v) => goFormatFloat(fmt, v)
	};

	const REPORT_UNSUPPORTED = Symbol('unsupported');
	// the deepest nesting of blocks the preview renders, and the most blocks
	// it renders in all, so nested loops can not grow without end
	const REPORT_MAX_DEPTH = 100;
	const REPORT_MAX_STEPS = 100000;

	// look up one argument: a literal, the dot, a field (.X, $.X, $t.X) or a
	// variable ($, $t). only own properties of the sample data and of the
	// variables are read
	function reportValue(tok, scope) {
		if (/^-?\d+(\.\d+)?$/.test(tok)) return Number(tok);
		if (/^"[^"]*"$/.test(tok)) return tok.slice(1, -1);
		if (tok === 'true' || tok === 'false') return tok === 'true';
		if (tok === '.') return scope.dot;
		const m = /^(\$\w*)?((?:\.\w+)*)$/.exec(tok);
		if (!m || (!m[1] && !m[2])) return REPORT_UNSUPPORTED;
		let v;
		if (m[1] === undefined) v = scope.dot;
		else if (m[1] === '$') v = scope.root;
		else {
			const name = m[1].slice(1);
			v = Object.prototype.hasOwnProperty.call(scope.vars, name) ? scope.vars[name] : undefined;
		}
		for (const key of m[2].split('.').slice(1)) {
			v =
				v !== null && typeof v === 'object' && Object.prototype.hasOwnProperty.call(v, key)
					? v[key]
					: undefined;
		}
		return v;
	}

	// evaluate one pipeline like `gt $.TemplateCount (len $.TemplateScreenshots)`.
	// returns REPORT_UNSUPPORTED for anything the preview does not know
	function evalReportExpr(expr, scope) {
		const tokens = expr.match(/"[^"]*"|\(|\)|[^\s()]+/g) || [];
		let i = 0;
		const command = () => {
			const items = [];
			while (i < tokens.length && tokens[i] !== ')') {
				if (tokens[i] === '(') {
					i += 1;
					const v = command();
					if (tokens[i] !== ')') return REPORT_UNSUPPORTED;
					i += 1;
					items.push({ value: v });
				} else {
					items.push({ tok: tokens[i] });
					i += 1;
				}
			}
			if (!items.length) return REPORT_UNSUPPORTED;
			const head = items[0];
			const isFunc =
				head.tok !== undefined && Object.prototype.hasOwnProperty.call(reportFuncs, head.tok);
			const args = (isFunc ? items.slice(1) : items).map((it) =>
				it.tok !== undefined ? reportValue(it.tok, scope) : it.value
			);
			if (args.includes(REPORT_UNSUPPORTED)) return REPORT_UNSUPPORTED;
			if (isFunc) return reportFuncs[head.tok](...args);
			return args.length === 1 ? args[0] : REPORT_UNSUPPORTED;
		};
		const value = command();
		return i === tokens.length ? value : REPORT_UNSUPPORTED;
	}

	// parse the template into text, actions and if/range/with blocks, matching
	// each {{end}} to its own opening tag so nested blocks stay balanced
	function parseReportTemplate(text) {
		// the list a still open block is appending to: its else part once
		// reached, otherwise its last branch or its body
		const openBody = (node) =>
			node.elseBody ?? (node.branches ? node.branches[node.branches.length - 1].body : node.body);
		const root = { body: [] };
		const stack = [];
		let current = root.body;
		let last = 0;
		// scanned with indexOf so the time stays linear in the template size
		for (;;) {
			const open = text.indexOf('{{', last);
			if (open === -1) break;
			const close = text.indexOf('}}', open + 2);
			if (close === -1) break;
			if (open > last) current.push({ type: 'text', value: text.slice(last, open) });
			last = close + 2;
			const raw = text.slice(open, last);
			// "{{- " and " -}}" are trim markers
			const inner = text
				.slice(open + 2, close)
				.replace(/^-\s/, '')
				.replace(/\s-$/, '')
				.trim();
			const block = stack[stack.length - 1];
			if (stack.length > REPORT_MAX_DEPTH) {
				// too deep to be a real report, the rest is shown as written
				current.push({ type: 'text', value: text.slice(open) });
				return root.body;
			}
			let k;
			if (inner.startsWith('/*')) {
				continue;
			} else if ((k = /^(if|with)\s+([\s\S]+)$/.exec(inner))) {
				const node = { type: k[1], branches: [{ expr: k[2], body: [] }], elseBody: null };
				current.push(node);
				stack.push(node);
				current = node.branches[0].body;
			} else if ((k = /^range\s+(?:(\$\w+)\s*(?:,\s*(\$\w+)\s*)?:=\s*)?([\s\S]+)$/.exec(inner))) {
				// with two variables Go binds the index first and the element second
				const node = {
					type: 'range',
					indexVar: k[2] ? k[1] : null,
					itemVar: k[2] || k[1] || null,
					expr: k[3],
					body: [],
					elseBody: null
				};
				current.push(node);
				stack.push(node);
				current = node.body;
			} else if (block && (k = /^else\s+if\s+([\s\S]+)$/.exec(inner)) && block.type === 'if') {
				const branch = { expr: k[1], body: [] };
				block.branches.push(branch);
				current = branch.body;
			} else if (block && inner === 'else') {
				block.elseBody = [];
				current = block.elseBody;
			} else if (block && inner === 'end') {
				stack.pop();
				current = stack.length ? openBody(stack[stack.length - 1]) : root.body;
			} else {
				current.push({ type: 'action', expr: inner, raw });
			}
		}
		if (last < text.length) current.push({ type: 'text', value: text.slice(last) });
		return root.body;
	}

	function renderReportNodes(nodes, scope) {
		let out = '';
		for (const node of nodes) {
			scope.steps.left -= 1;
			if (scope.steps.left < 0) {
				throw new Error('report preview too large');
			}
			if (node.type === 'text') {
				out += node.value;
			} else if (node.type === 'action') {
				const v = evalReportExpr(node.expr, scope);
				if (v === REPORT_UNSUPPORTED) out += node.raw;
				else if (v !== undefined && v !== null && typeof v !== 'object') out += String(v);
			} else if (node.type === 'if') {
				const hit = node.branches.find((b) => reportTruthy(evalReportExpr(b.expr, scope)));
				if (hit) out += renderReportNodes(hit.body, scope);
				else if (node.elseBody) out += renderReportNodes(node.elseBody, scope);
			} else if (node.type === 'with') {
				const v = evalReportExpr(node.branches[0].expr, scope);
				if (reportTruthy(v)) out += renderReportNodes(node.branches[0].body, { ...scope, dot: v });
				else if (node.elseBody) out += renderReportNodes(node.elseBody, scope);
			} else if (node.type === 'range') {
				const v = evalReportExpr(node.expr, scope);
				const items = Array.isArray(v) ? v : [];
				if (!items.length && node.elseBody) out += renderReportNodes(node.elseBody, scope);
				items.forEach((item, idx) => {
					const vars = Object.assign(Object.create(null), scope.vars);
					if (node.indexVar) vars[node.indexVar.slice(1)] = idx;
					if (node.itemVar) vars[node.itemVar.slice(1)] = item;
					out += renderReportNodes(node.body, { root: scope.root, dot: item, vars, steps: scope.steps });
				});
			}
		}
		return out;
	}

	// render the report template against the sample data. the dot and $ both
	// start at the root report data, like Go's Execute
	function renderReportPreview(text) {
		try {
			return renderReportNodes(parseReportTemplate(text), {
				root: reportSampleData,
				dot: reportSampleData,
				vars: Object.create(null),
				steps: { left: REPORT_MAX_STEPS }
			});
		} catch (e) {
			// a template the preview can not render is shown as written
			return text;
		}
	}

	switch (contentType) {
		case 'domain': {
			delete templates['Email'];
			delete templates['Recipient'];
			delete templates['URLs & Tracking'];
			break;
		}
		case 'email': {
			templates['URLs & Tracking'] = [...templates['URLs & Tracking'], ...emailTemplates];
			break;
		}
		case 'report': {
			delete templates['Email'];
			delete templates['Recipient'];
			delete templates['URLs & Tracking'];
			// report variables grouped into meaningful categories; keep Functions last
			const reportFunctions = templates['Functions'];
			delete templates['Functions'];
			Object.assign(templates, reportCategories);
			templates['Functions'] = reportFunctions;
			break;
		}
	}

	// replace the whole content from the parent while the editor is open, as the
	// editor only reads value when it mounts. keepUndo lets ctrl+z bring the old
	// content back
	export const replaceContent = (text, keepUndo = false) => {
		value = text;
		if (!editor) return;
		const model = editor.getModel();
		if (keepUndo) {
			editor.pushUndoStop();
			editor.executeEdits('replace-content', [{ range: model.getFullModelRange(), text }]);
			editor.pushUndoStop();
		} else {
			model.setValue(text);
		}
		// measure again once the new content is drawn, so lines wrap at the
		// editor width
		requestAnimationFrame(() => editor?.layout());
	};

	const insertTemplate = (text) => {
		if (editor) {
			const selection = editor.getSelection();
			editor.executeEdits('template-insert', [
				{
					range: selection,
					text: text
				}
			]);
			editor.focus();
			// updatePreview();
		}
	};

	onMount(() => {
		document.body.classList.add('overflow-hidden');
		/* @ts-ignore */
		self.MonacoEnvironment = {
			getWorker: function (_, label) {
				if (label === 'html') {
					return new htmlWorker();
				}
				return new editorWorker();
			}
		};
		// offer rb client completions in the phishing page editor
		if (contentType === 'page') {
			ensureRbLanguageFeatures();
		}
		const editorOptions = {
			value: value,
			language: 'html',
			theme: pcDarkTheme(monaco),
			automaticLayout: true,
			minimap: {
				enabled: false
			},
			fontSize: 13,
			lineNumbers: 'on',
			folding: true,
			wordWrap: 'on',
			contextmenu: true,
			overviewRulerBorder: false,
			overviewRulerLanes: 0,
			hideCursorInOverviewRuler: true,
			scrollbar: {
				horizontal: 'hidden',
				// at the end of the code the wheel scrolls the page or modal
				alwaysConsumeMouseWheel: false
			}
		};

		/* @ts-ignore - editorOptions is not complete */
		editor = monaco.editor.create(editorContainer, editorOptions);

		// Ctrl/Cmd+S saves by submitting the surrounding form, so the editor's usual
		// save shortcut maps to the page's save action.
		editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
			editorContainer?.closest('form')?.requestSubmit();
		});

		// vim mode will be initialized by reactive statement if needed

		editor.getModel().onDidChangeContent((e) => {
			// the bound value follows every edit, only the preview waits
			value = editor.getValue();
			if (previewRenderDelayID) {
				clearTimeout(previewRenderDelayID);
				previewRenderDelayID = null;
			}
			previewRenderDelayID = setTimeout(() => {
				updatePreview();
			}, previewRenderDelay);
		});
		updatePreview();

		return () => {
			isDestroyed = true;
			document.body.classList.remove('overflow-hidden');
			// properly cleanup vim mode first
			destroyVimMode();
			if (editor) {
				editor.dispose();
				monaco.editor.getModels().forEach((model) => model.dispose());
				editor = null;
			}
		};
	});

	// track vim mode state to prevent duplicate initialization
	let vimModeInstance = null;

	const initializeVimMode = () => {
		if (localVimMode && editor && !vimModeInstance && !isDestroyed) {
			try {
				const statusNode = vimStatusBar;
				vimModeInstance = vimModule.initVimMode(editor, statusNode);

				// integrate system clipboard with vim registers
				setupVimClipboardIntegration(editor, vimModeInstance, localVimMode, monaco);
			} catch (e) {
				console.error('vim mode not available', e);
			}
		}
	};

	const destroyVimMode = () => {
		if (vimModeInstance) {
			try {
				// cleanup clipboard integration first
				destroyVimClipboardIntegration(vimModeInstance);

				// use official monaco-vim dispose method
				vimModeInstance.dispose();
			} catch (e) {
				console.warn('Error disposing vim mode:', e);
			}

			vimModeInstance = null;
		}
	};

	// sync with external vim mode control
	$: if (externalVimMode !== null) {
		localVimMode = externalVimMode;
	} else {
		localVimMode = $vimModeEnabled;
	}

	// Watch for vim mode changes
	$: if (editor && !isDestroyed && typeof localVimMode === 'boolean') {
		if (localVimMode && !vimModeInstance) {
			// Wait for DOM updates to complete
			tick().then(() => {
				if (localVimMode && !vimModeInstance && !isDestroyed) {
					initializeVimMode();
				}
			});
		} else if (!localVimMode && vimModeInstance) {
			destroyVimMode();
		}
	}

	const selectPreviewDomain = () => {
		baseURL = selectedDomain ? selectedDomain : baseURL;
		updatePreview();
	};

	// create the named iframe the preview form posts into. the sandboxed server
	// response is what isolates the content; the sandbox attribute without
	// allow-same-origin adds a second layer. a plain iframe, not a shadow root,
	// so the form can find the frame by name.
	const createPreviewIframe = () => {
		if (!shadowContainer) return;

		shadowContainer.innerHTML = '';

		const iframe = document.createElement('iframe');
		iframe.name = previewFrameName;
		iframe.sandbox = 'allow-forms allow-modals allow-popups allow-scripts';
		iframe.title = 'preview';
		iframe.style.cssText = 'height: 100%; width: 100%; border: none; background: white;';

		shadowContainer.appendChild(iframe);

		previewFrame = iframe;
	};

	const updatePreview = async () => {
		if (isRenderingPreview) {
			return;
		}
		const v = editor.getValue() ?? value;
		value = v;
		const content = await replaceTemplateVariables(v);

		if (shadowContainer && !previewFrame) {
			createPreviewIframe();
		}

		// post the content to the backend, which returns it with a sandbox policy
		// so the preview runs isolated and still loads its own external resources
		if (previewFrame) {
			submitPreviewForm(content, previewFrameName);
		}
		isRenderingPreview = false;
	};

	const replaceTemplateVariables = async (text) => {
		// replace inline image cid: references with data URLs for preview in shadow DOM
		if (attachments && attachments.length > 0) {
			// find all cid: references in the text
			const cidRegex = /src=["']cid:([^"']+)["']/gi;
			const matches = [];
			let match;
			while ((match = cidRegex.exec(text)) !== null) {
				matches.push(match[1]);
			}

			// fetch and replace each cid: reference with data URL
			for (const filename of matches) {
				// find the attachment with this filename that is inline
				const attachment = attachments.find((att) => att.isInline && att.fileName === filename);
				if (attachment && attachment.id) {
					try {
						// fetch attachment content from API
						const response = await fetch(`/api/v1/attachment/${attachment.id}/content`);
						const json = await response.json();

						if (json.success && json.data && json.data.file) {
							// determine mime type from filename extension
							const ext = filename.split('.').pop().toLowerCase();
							const mimeTypes = {
								png: 'image/png',
								jpg: 'image/jpeg',
								jpeg: 'image/jpeg',
								gif: 'image/gif',
								webp: 'image/webp',
								svg: 'image/svg+xml',
								bmp: 'image/bmp'
							};
							const mimeType = mimeTypes[ext] || 'image/png';

							// create data URL from base64 data
							const dataUrl = `data:${mimeType};base64,${json.data.file}`;

							// replace cid: with data URL (escape filename for regex)
							const escapedFilename = filename.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
							text = text.replace(new RegExp(`cid:${escapedFilename}`, 'g'), dataUrl);
						}
					} catch (error) {
						console.error('Failed to fetch attachment for preview:', error);
					}
				}
			}
		}

		let param = '?id=905f286e-486b-434b-8ecc-d82456a07f7b';
		let _baseURL = `https://${baseURL}`;
		let _url = `https://${baseURL}${param}`;
		// mock direct stage links for preview; the real ones carry an encrypted state param
		let _beforeLandingPageURL = `${_url}&state=before`;
		let _landingPageURL = `${_url}&state=landing`;
		let _afterLandingPageURL = `${_url}&state=after`;
		let _qrURL = _url;

		if (text.includes('{{qr')) {
			const r = /{{qr\s+(.+?)\s+(\d+)}}/g;
			const rr = r.exec(text);
			let dotSize = 4;
			if (rr && rr[1] && rr[1] !== '.URL') {
				_qrURL = rr[1];
			}
			if (rr && rr[2]) {
				dotSize = Number(rr[2]);
			}
			const qrHash = `${_qrURL}${dotSize}`
				.split('')
				.reduce((a, b) => ((a << 5) - a + b.charCodeAt(0)) | 0, 0); // TODO add credits for hashing func
			if (previousQRHash === qrHash) {
				text = text.replace(r, (match, urlVar, size) => {
					return previousQRCode;
				});
			} else {
				const qr = await generateQR(_qrURL, dotSize);
				previousQRHash = qrHash;
				previousQRCode = qr;
				text = text.replace(r, (match, urlVar, size) => {
					return qr;
				});
			}
		}

		if (text.includes('{{urlEscape')) {
			const r = /{{urlEscape\s+(.+?)}}/g;
			text = text.replace(r, (match, v) => {
				return 'URL_ENCODED_TEXT';
			});
		}
		if (text.includes('{{randInt')) {
			const r = /{{randInt\s+(\d+)\s+(\d+)}}/g;
			text = text.replace(r, (match, min, max) => {
				min = parseInt(min, 10);
				max = parseInt(max, 10);
				return Math.floor(Math.random() * (max - min + 1) + min);
			});
		}
		if (text.includes('{{randAlpha')) {
			const r = /{{randAlpha\s+(\d+)}}/g;
			text = text.replace(r, (match, length) => {
				const alphaChars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ';
				length = parseInt(length, 10);
				if (length > 32) {
					return 'ERROR: length must be less than 32';
				}
				let result = '';
				for (let i = 0; i < length; i++) {
					result += alphaChars.charAt(Math.floor(Math.random() * alphaChars.length));
				}
				return result;
			});
		}

		// handle base64 function
		if (text.includes('{{base64')) {
			const r = /{{base64\s+"([^"]+)"}}/g;
			text = text.replace(r, (match, input) => {
				return btoa(input);
			});
		}

		// handle date function with format and optional offset
		if (text.includes('{{date')) {
			const r = /{{date\s+"([^"]+)"\s*(-?\d+)?}}/g;
			text = text.replace(r, (match, format, offset) => {
				const currentTime = new Date();
				const offsetSeconds = offset ? parseInt(offset, 10) : 0;
				const targetTime = new Date(currentTime.getTime() + offsetSeconds * 1000);

				return formatDate(targetTime, format);
			});
		}

		switch (contentType) {
			case 'report':
				return renderReportPreview(text);
			case 'domain':
				return text.replaceAll('{{.BaseURL}}', _baseURL);
			case 'page':
				return text
					.replaceAll('{{.FirstName}}', 'Alice')
					.replaceAll('{{.LastName}}', 'Andersen')
					.replaceAll('{{.Email}}', 'alice@worldcorp.test')
					.replaceAll('{{.To}}', 'Alice <alice@worldcorp.test>')
					.replaceAll('{{.Phone}}', '+45 13374242')
					.replaceAll('{{.ExtraIdentifier}}', 'Al1C5')
					.replaceAll('{{.Position}}', 'Head of operations')
					.replaceAll('{{.Department}}', 'Research and Development')
					.replaceAll('{{.City}}', 'Odense')
					.replaceAll('{{.Country}}', 'Denmark')
					.replaceAll('{{.Misc}}', 'Pasta')
					.replaceAll('{{.Tracker}}', '')
					.replaceAll('{{.TrackerURL}}', '')
					.replaceAll('{{.From}}', '')
					.replaceAll('{{.BaseURL}}', _baseURL)
					.replaceAll('{{.URL}}', _url)
					.replaceAll('{{.BeforeLandingPageURL}}', _beforeLandingPageURL)
					.replaceAll('{{.LandingPageURL}}', _landingPageURL)
					.replaceAll('{{.AfterLandingPageURL}}', _afterLandingPageURL)
					.replaceAll('{{MicrosoftDeviceCode}}', 'ABCD-1234')
					.replaceAll('{{MicrosoftDeviceCodeURL}}', 'https://microsoft.com/devicelogin')
					.replaceAll('{{DeviceCodeCaptured}}', 'false')
					.replace(
						/\{\{RemoteBrowserScript\s+"[^"]*"\}\}/g,
						'<!-- RemoteBrowserScript (injected at runtime) -->'
					);
			case 'email':
				return text
					.replaceAll('{{.FirstName}}', 'Alice')
					.replaceAll('{{.LastName}}', 'Andersen')
					.replaceAll('{{.Email}}', 'alice@worldcorp.test')
					.replaceAll('{{.To}}', 'Alice <alice@worldcorp.test>')
					.replaceAll('{{.Phone}}', '+45 13374242')
					.replaceAll('{{.ExtraIdentifier}}', 'Al1C5')
					.replaceAll('{{.Position}}', 'Head of operations')
					.replaceAll('{{.Department}}', 'Research and Development')
					.replaceAll('{{.City}}', 'Odense')
					.replaceAll('{{.Country}}', 'Denmark')
					.replaceAll('{{.Misc}}', 'Pasta')
					.replaceAll(
						'{{.Tracker}}',
						`<img src=\"${_baseURL}/wf/open?upn=905f286e-486b-434b-8ecc-d82456a07f7b\" alt=\"\" width=\"1\" height=\"1\" border=\"0\" style=\"height:1px !important;width:1px\" />`
					)
					.replaceAll(
						'{{.TrackerURL}}',
						`${_baseURL}/wf/open?upn=905f286e-486b-434b-8ecc-d82456a07f7b`
					)
					.replaceAll('{{.From}}', 'sender@new-order.test')
					.replaceAll('{{.BaseURL}}', _baseURL)
					.replaceAll('{{.URL}}', _url)
					.replaceAll('{{.BeforeLandingPageURL}}', _beforeLandingPageURL)
					.replaceAll('{{.LandingPageURL}}', _landingPageURL)
					.replaceAll('{{.AfterLandingPageURL}}', _afterLandingPageURL)
					.replaceAll('{{MicrosoftDeviceCode}}', 'ABCD-1234')
					.replaceAll('{{MicrosoftDeviceCodeURL}}', 'https://microsoft.com/devicelogin')
					.replaceAll('{{DeviceCodeCaptured}}', 'false');
		}
	};

	const onSetFile = (event) => {
		const file = event.target.files[0];
		const reader = new FileReader();
		reader.onload = (e) => {
			value = e.target.result.toString();
			editor.getModel().setValue(value);
			updatePreview();
		};
		reader.readAsText(file);
	};

	const openFullPagePreview = async (e) => {
		e.preventDefault();
		const v = editor.getValue();
		value = v;
		const content = await replaceTemplateVariables(v);
		// open the current content isolated in a new tab through the backend so it
		// gets its own sandbox policy and loads its own external resources
		submitPreviewForm(content, '_blank');
	};

	const triggerFileInput = () => {
		if (fileInputRef) {
			fileInputRef.click();
		}
	};

	// formatDate converts readable date format (YmdHis) to formatted date string
	const formatDate = (date, format) => {
		const pad = (num, size = 2) => num.toString().padStart(size, '0');

		const replacements = {
			Y: date.getFullYear(), // 4-digit year
			y: date.getFullYear().toString().slice(-2), // 2-digit year
			m: pad(date.getMonth() + 1), // 2-digit month
			n: date.getMonth() + 1, // month without leading zero
			d: pad(date.getDate()), // 2-digit day
			j: date.getDate(), // day without leading zero
			H: pad(date.getHours()), // 24-hour format
			G: date.getHours(), // 24-hour without leading zero
			h: pad(date.getHours() % 12 || 12), // 12-hour format
			g: date.getHours() % 12 || 12, // 12-hour without leading zero
			i: pad(date.getMinutes()), // minutes
			s: pad(date.getSeconds()), // seconds
			A: date.getHours() >= 12 ? 'PM' : 'AM', // uppercase AM/PM
			a: date.getHours() >= 12 ? 'pm' : 'am' // lowercase am/pm
		};

		let result = format;
		for (const [key, value] of Object.entries(replacements)) {
			result = result.replaceAll(key, value.toString());
		}
		return result;
	};
</script>

<div
	class="w-full min-w-0 {isExpanded ? 'z-[9000]' : 'mb-6'} col-start-1 col-end-4 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<div
		class="bg-white dark:bg-gray-800 transition-colors duration-200"
		class:fixed={isExpanded}
		class:inset-0={isExpanded}
		class:z-50={isExpanded}
		class:flex={isExpanded}
		class:flex-col={isExpanded}
		style={isExpanded ? 'width: 100vw; height: 100dvh;' : ''}
		role={isExpanded ? 'dialog' : undefined}
		aria-modal={isExpanded ? 'true' : undefined}
		aria-label={isExpanded ? 'Editor' : undefined}
		on:keydown={(e) => {
			if (isExpanded && e.key === 'Escape') {
				e.stopPropagation();
				isExpanded = false;
			}
		}}
	>
		<!-- details -->
		{#if !isExpanded}
			<div
				class="flex flex-col lg:flex-row lg:items-center h-auto w-full justify-between mb-4 bg-white dark:bg-gray-800 transition-colors duration-200"
			>
				<slot />
			</div>
		{/if}
		<!-- editor controls -->
		<div
			class="flex flex-wrap items-center gap-2 w-full p-2 sm:p-3 rounded-t-md border border-b-0 border-gray-200 dark:border-gray-700 bg-gray-100 dark:bg-slate-900 transition-colors duration-200"
		>
			<div class="flex flex-wrap items-center gap-2 min-w-0 w-full sm:w-auto">
				<!-- custom file upload button -->
				<button
					type="button"
					on:click={triggerFileInput}
					class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600"
				>
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-4 w-4"
						viewBox="0 0 20 20"
						fill="currentColor"
					>
						<path
							fill-rule="evenodd"
							d="M3 17a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM6.293 6.707a1 1 0 010-1.414l3-3a1 1 0 011.414 0l3 3a1 1 0 01-1.414 1.414L11 5.414V13a1 1 0 11-2 0V5.414L7.707 6.707a1 1 0 01-1.414 0z"
							clip-rule="evenodd"
						/>
					</svg>
					<span>Load File</span>
				</button>
				<input
					bind:this={fileInputRef}
					type="file"
					on:change={onSetFile}
					accept=".html,.htm,.txt"
					class="hidden"
				/>

				<!-- template selector -->
				<select
					aria-label="Insert template"
					class="pc-select h-8 flex-1 sm:flex-none min-w-0 max-w-full text-sm dark:bg-gray-700"
					on:change={(e) => {
						const t = /** @type {HTMLSelectElement} */ (e.target);
						if (t.value) {
							insertTemplate(t.value);
							t.value = '';
						}
					}}
				>
					<option class="" value="">Templates...</option>
					{#each Object.entries(computedTemplates) as [group, items]}
						<optgroup label={group}>
							{#each items as item}
								<option value={item.text}>{item.label}</option>
							{/each}
						</optgroup>
					{/each}
				</select>

				<!-- domain selector if available (not shown for report templates) -->
				{#if contentType !== 'report'}
					<select
						bind:value={selectedDomain}
						on:change={selectPreviewDomain}
						aria-label="Preview domain"
						class="pc-select h-8 min-w-0 max-w-full text-sm dark:bg-gray-700 w-full sm:w-64 text-ellipsis"
					>
						{#if domainMap.values().length}
							<option value="" class="italic">Select preview domain...</option>
							{#each domainMap.values() as domain}
								<option value={domain}>{domain}</option>
							{/each}
						{:else}
							<option value="" class="italic">No domains - Assets will not load</option>
						{/if}
					</select>
				{/if}
			</div>
			<div class="flex flex-wrap items-center gap-2 sm:ml-auto">
				<!-- vim mode toggle button -->
				<button
					type="button"
					title="Toggle vim mode (Ctrl+Alt+V)"
					aria-pressed={localVimMode}
					on:click={() => {
						vimModeEnabled.update((v) => !v);
					}}
					class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {localVimMode ? 'bg-cta-blue text-white border-cta-blue font-semibold hover:bg-active-blue' : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600'}"
				>
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-4 w-4"
						viewBox="0 0 20 20"
						fill="currentColor"
						aria-hidden="true"
					>
						<path d="M3 3h18v18H3V3zm2 2v14h14V5H5zm2 2h10v2H7V7zm0 4h10v2H7v-2zm0 4h6v2H7v-2z" />
					</svg>
					<span>Vim</span>
				</button>
				<!-- custom preview toggle button -->
				<button
					type="button"
					on:click={async () => {
						isPreviewVisible = !isPreviewVisible;
						if (isPreviewVisible) {
							previewFrame = null;
							await tick();
							updatePreview();
						} else {
							previewFrame = null;
							if (shadowContainer) shadowContainer.innerHTML = '';
						}
					}}
					aria-pressed={isPreviewVisible}
					class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue {isPreviewVisible ? 'bg-cta-blue text-white border-cta-blue font-semibold hover:bg-active-blue' : 'bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600'}"
				>
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-4 w-4"
						viewBox="0 0 20 20"
						fill="currentColor"
					>
						<path d="M10 12a2 2 0 100-4 2 2 0 000 4z" />
						<path
							fill-rule="evenodd"
							d="M.458 10C1.732 5.943 5.522 3 10 3s8.268 2.943 9.542 7c-1.274 4.057-5.064 7-9.542 7S1.732 14.057.458 10zM14 10a4 4 0 11-8 0 4 4 0 018 0z"
							clip-rule="evenodd"
						/>
					</svg>
					<span>Preview</span>
				</button>

				<!-- open in new window button -->
				<button
					type="button"
					on:click={openFullPagePreview}
					aria-label="Open preview in new window"
					title="Open preview in new window"
					class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600"
				>
					<svg
						xmlns="http://www.w3.org/2000/svg"
						class="h-4 w-4"
						viewBox="0 0 20 20"
						fill="currentColor"
					>
						<path
							fill-rule="evenodd"
							d="M4 4a2 2 0 00-2 2v8a2 2 0 002 2h12a2 2 0 002-2V8.414l-4-4H4zm.5 2a.5.5 0 00-.5.5v7a.5.5 0 00.5.5h11a.5.5 0 00.5-.5v-7a.5.5 0 00-.5-.5h-11z"
							clip-rule="evenodd"
						/>
						<path
							d="M8 6h2v2H8V6zM6 8h2v2H6V8zM8 10h2v2H8v-2zM6 12h2v2H6v-2zM10 8h2v2h-2V8zM12 6h2v2h-2V6zM10 12h2v2h-2v-2zM12 10h2v2h-2v-2z"
						/>
					</svg>
					<span class="hidden sm:inline">New Window</span>
				</button>
				<button
					type="button"
					on:click={() => (isExpanded = !isExpanded)}
					aria-expanded={isExpanded}
					aria-label={isExpanded ? 'Collapse editor' : 'Expand editor'}
					title={isExpanded ? 'Collapse editor' : 'Expand editor'}
					class="h-8 inline-flex items-center justify-center gap-2 px-3 rounded-md border text-sm whitespace-nowrap cursor-pointer transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-cta-blue dark:focus-visible:ring-highlight-blue bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600"
				>
					{#if !isExpanded}
						<!-- Expand icon -->
						<svg class="h-4 w-4" viewBox="0 0 20 20" fill="none">
							<path
								d="M4 8V4h4M16 8V4h-4M4 12v4h4M16 12v4h-4"
								stroke="currentColor"
								stroke-width="1.5"
								stroke-linecap="round"
							/>
							<path
								d="M4 4l5 5M16 4l-5 5M4 16l5-5M16 16l-5-5"
								stroke="currentColor"
								stroke-width="1.5"
								stroke-linecap="round"
							/>
						</svg>
						<span class="hidden sm:inline">Expand</span>
					{:else}
						<!-- Collapse icon -->
						<svg class="h-4 w-4" viewBox="0 0 20 20" fill="none">
							<path
								d="M9 4h-5v5M11 4h5v5M9 16h-5v-5M11 16h5v-5"
								stroke="currentColor"
								stroke-width="1.5"
								stroke-linecap="round"
							/>
							<path
								d="M9 9l-5-5M11 9l5-5M9 11l-5 5M11 11l5 5"
								stroke="currentColor"
								stroke-width="1.5"
								stroke-linecap="round"
							/>
						</svg>
						<span class="hidden sm:inline">Collapse</span>
					{/if}
				</button>
			</div>
		</div>
		<!-- editor and preview, side by side from md, stacked below -->
		<div
			class="flex flex-col md:flex-row w-full overflow-hidden {isExpanded
				? 'flex-1 min-h-0'
				: 'md:h-[var(--editor-h)]'}"
			style="--editor-h: {height}"
		>
			<div
				class="flex flex-col relative min-w-0 w-full transition-colors duration-200 {isExpanded
					? 'flex-1 min-h-0 md:h-full'
					: 'h-[var(--editor-h)] max-sm:h-60 md:h-full'} {isPreviewVisible ? 'md:w-1/2' : ''}"
			>
				<div bind:this={editorContainer} class="h-full"></div>
				{#if localVimMode}
					<div
						bind:this={vimStatusBar}
						class="absolute bottom-0 left-0 right-0 px-2 py-1 bg-gray-700 text-xs font-mono text-gray-300"
						style="height: 25px;"
					></div>
				{/if}
			</div>
			{#if isPreviewVisible}
				<div
					class="w-full md:w-1/2 min-w-0 border border-gray-300 dark:border-gray-600 bg-white transition-colors duration-200 {isExpanded
						? 'flex-1 min-h-0 md:h-full'
						: 'h-[var(--editor-h)] max-sm:h-60 md:h-full'}"
				>
					<div bind:this={shadowContainer} class="h-full w-full"></div>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	:global(.monaco-editor .current-line) {
		background-color: rgba(255, 255, 255, 0.05) !important;
	}
</style>
