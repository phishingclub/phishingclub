<script>
	import { onMount } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
	import { BiMap } from '$lib/utils/maps';
	import { previewQR as generateQR } from '$lib/utils/qrPreview';
	/** @type {'domain'|'page'|'email'} */

	export let contentType;
	export let value;
	export let baseURL = 'example.test';
	export let domainMap = new BiMap({});
	export let selectedDomain = '';
	let editor = null;
	let previewFrame = null;
	let previewRenderDelayID = null;
	let previewRenderDelay = 250;
	let isRenderingPreview = false;
	let previousQRCode = '';
	let previousQRHash = 0;

	let isPreviewVisible = false;
	let externalFrameRef = null;
	let fileInputRef;

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
			{ label: 'URL', text: '{{.URL}}' }
		],
		Functions: [
			{ label: 'URL as QR HTML', text: '{{qr .URL 4}}' },
			{ label: 'URL escape', text: '{{urlEscape "content" }}' },
			{ label: 'Random alphanumeric', text: '{{randAlpha 8}}' },
			{ label: 'Random number', text: '{{randInt 1 4}}' },
			{ label: 'Date', text: '{{date "Y-m-d H:i:s" 0}}' },
			{ label: 'Base64', text: '{{base64 "text"}}' }
		]
	};

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
	}

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

	/*
	$: {

		if (previewFrame && isPreviewVisible && !isRenderingPreview) {
			updatePreview();
		}
	}
	*/

	onMount(() => {
		document.body.classList.add('overflow-hidden');
		self.MonacoEnvironment = {
			getWorker: function (_, label) {
				if (label === 'html') {
					return new htmlWorker();
				}
				return new editorWorker();
			}
		};
		editor = monaco.editor.create(document.getElementById('monaco-editor'), {
			value: value,
			language: 'html',
			theme: 'vs-dark',
			automaticLayout: true,
			minimap: {
				enabled: false
			}
		});

		editor.getModel().onDidChangeContent((e) => {
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
			document.body.classList.remove('overflow-hidden');
			if (editor) {
				editor.dispose();
				monaco.editor.getModels().forEach((model) => model.dispose());
			}
		};
	});

	const selectPreviewDomain = () => {
		baseURL = selectedDomain ? selectedDomain : baseURL;
		updatePreview();
	};

	const updatePreview = async () => {
		if (isRenderingPreview) {
			return;
		}
		const v = editor.getValue() ?? value;
		value = v;
		const content = await replaceTemplateVariables(v);
		if (previewFrame) {
			const blob = new Blob([content], { type: 'text/html' });
			URL.revokeObjectURL(previewFrame.src);
			const url = URL.createObjectURL(blob);
			previewFrame.src = url;
		}
		if (externalFrameRef) {
			const blob = new Blob([createEmbed(content)], { type: 'text/html' });
			externalFrameRef.location.replace(URL.createObjectURL(blob));
		}
		isRenderingPreview = false;
	};

	const replaceTemplateVariables = async (text) => {
		let param = '?id=905f286e-486b-434b-8ecc-d82456a07f7b';
		let _baseURL = `https://${baseURL}`;
		let _url = `https://${baseURL}${param}`;
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
					.replaceAll('{{.URL}}', _url);
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
					.replaceAll('{{.URL}}', _url);
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

	const createEmbed = (content) => {
		return `
      <!DOCTYPE html>
      <html>
        <head>
          <title></title>
          <style>
            *, body, iframe {margin: 0; padding: 0; border: 0; height: 100%; width: 100%;}
          </style>
        </head>
        <body>
          <iframe
            sandbox="allow-forms allow-modals allow-popups allow-scripts allow-pointer-lock"
            src="data:text/html;base64,${btoa(content)}"></iframe>
        </body>
      </html>
    `;
	};

	const openFullPagePreview = async (e) => {
		e.preventDefault();
		const v = editor.getValue();
		value = v;
		const content = await replaceTemplateVariables(v);
		const blob = new Blob([createEmbed(content)], { type: 'text/html' });
		let url = URL.createObjectURL(blob);
		externalFrameRef = window.open(url, '_blank');
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

	let isDetailsVisible = $$slots.default;
</script>

<div
	class="w-80vw z-[9000] col-start-1 col-end-4 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<div class="bg-white dark:bg-gray-800 transition-colors duration-200">
		<div class="mt-4 flex items-center flex-wrap">
			<!-- mode tabs -->
			<div class="flex">
				{#if $$slots.default}
					<button
						on:click={() => {
							isDetailsVisible = true;
						}}
						type="button"
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md w-36 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 mb-2 text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 transition-colors duration-200"
						class:font-bold={isDetailsVisible}
						class:bg-cta-blue={isDetailsVisible}
						class:dark:bg-indigo-600={isDetailsVisible}
						class:text-white={isDetailsVisible}
					>
						<svg
							xmlns="http://www.w3.org/2000/svg"
							class="h-4 w-4"
							viewBox="0 0 20 20"
							fill="currentColor"
						>
							<path
								fill-rule="evenodd"
								d="M4 4a2 2 0 012-2h8a2 2 0 012 2v12a2 2 0 01-2 2H6a2 2 0 01-2-2V4zm2 0v12h8V4H6z"
								clip-rule="evenodd"
							/>
							<path fill-rule="evenodd" d="M7 7h6v2H7V7zm0 4h6v2H7v-2z" clip-rule="evenodd" />
						</svg>
						<span>Details</span>
					</button>
					<button
						on:click={() => {
							isDetailsVisible = false;
						}}
						type="button"
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md w-36 text-center cursor-pointer hover:opacity-80 ml-1 flex items-center justify-center gap-2 text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 transition-colors duration-200"
						class:font-bold={!isDetailsVisible}
						class:bg-cta-blue={!isDetailsVisible}
						class:dark:bg-indigo-600={!isDetailsVisible}
						class:text-white={!isDetailsVisible}
					>
						<svg
							xmlns="http://www.w3.org/2000/svg"
							class="h-4 w-4"
							viewBox="0 0 20 20"
							fill="currentColor"
						>
							<path
								fill-rule="evenodd"
								d="M12.316 3.051a1 1 0 01.633 1.265l-4 12a1 1 0 11-1.898-.632l4-12a1 1 0 011.265-.633zM5.707 6.293a1 1 0 010 1.414L3.414 10l2.293 2.293a1 1 0 11-1.414 1.414l-3-3a1 1 0 010-1.414l3-3a1 1 0 011.414 0zm8.586 0a1 1 0 011.414 0l3 3a1 1 0 010 1.414l-3 3a1 1 0 11-1.414-1.414L16.586 10l-2.293-2.293a1 1 0 010-1.414z"
								clip-rule="evenodd"
							/>
						</svg>
						<span>Editor</span>
					</button>
				{/if}
			</div>

			<!-- editor controls - custom markup that matches the design -->
			{#if !isDetailsVisible}
				<div
					class="flex items-center ml-0 xl:ml-4 flex-wrap gap-2 mb-2"
					class:ml-4={$$slots.default}
					class:mb-4={!$$slots.default}
				>
					<!-- custom file upload button -->
					<button
						type="button"
						on:click={triggerFileInput}
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
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

					<!-- custom preview toggle button -->
					<button
						type="button"
						on:click={() => {
							isPreviewVisible = !isPreviewVisible;
							if (isPreviewVisible) {
								updatePreview();
							}
						}}
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
						class:font-bold={isPreviewVisible}
						class:bg-cta-blue={isPreviewVisible}
						class:dark:bg-indigo-600={isPreviewVisible}
						class:text-white={isPreviewVisible}
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

					<!-- template selector -->
					<select
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 bg-white dark:bg-gray-700 text-black dark:text-gray-200 cursor-pointer transition-colors duration-200"
						on:change={(e) => {
							const t = /** @type {HTMLSelectElement} */ (e.target);
							if (t.value) {
								insertTemplate(t.value);
								t.value = ''; // reset selection
							}
						}}
					>
						<option class="" value="">Templates...</option>
						{#each Object.entries(templates) as [group, items]}
							<optgroup label={group}>
								{#each items as item}
									<option value={item.text}>{item.label}</option>
								{/each}
							</optgroup>
						{/each}
					</select>

					<!-- domain selector if available -->
					{#if domainMap.values().length}
						<select
							id="domain-select"
							bind:value={selectedDomain}
							on:change={selectPreviewDomain}
							class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 bg-white dark:bg-gray-700 text-black dark:text-gray-200 cursor-pointer transition-colors duration-200"
						>
							<option value="">Select preview domain...</option>
							{#each domainMap.values() as domain}
								<option value={domain}>{domain}</option>
							{/each}
						</select>
					{/if}

					<!-- open in new window button -->
					<button
						type="button"
						on:click={openFullPagePreview}
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
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
						<span>New Window</span>
					</button>
				</div>
			{/if}
		</div>

		<!-- details -->
		{#if $$slots.default}
			<div
				class="flex flex-col lg:flex-row lg:items-center h-auto w-full justify-between mb-4 bg-white dark:bg-gray-800 transition-colors duration-200"
			>
				{#if isDetailsVisible}
					<slot />
				{/if}
			</div>
		{/if}
	</div>

	<div class="flex h-full">
		<div
			class="flex flex-col border-2 border-black dark:border-gray-600 bg-white dark:bg-gray-900 {!isPreviewVisible
				? 'w-80vw'
				: 'w-1/2'} transition-colors duration-200"
			class:h-55vh={isDetailsVisible}
			class:h-67vh={!isDetailsVisible}
		>
			<div id="monaco-editor" class="h-full" />
		</div>
		<div
			class="bg-cta-blue dark:bg-indigo-600 cursor-move w-1 transition-colors duration-200"
			class:hidden={!isPreviewVisible}
		>
			&nbsp;
		</div>
		{#if isPreviewVisible}
			<div
				class="w-1/2 border-2 border-black dark:border-gray-600 bg-white transition-colors duration-200"
			>
				<iframe
					bind:this={previewFrame}
					sandbox="allow-forms allow-modals allow-popups allow-scripts allow-pointer-lock"
					title="preview"
					class="h-full w-full"
					style="color-scheme: light;"
				/>
			</div>
		{/if}
	</div>
</div>
