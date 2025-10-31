<script>
	import { onMount, tick } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker';
	import * as vimModule from 'monaco-vim';
	import { BiMap } from '$lib/utils/maps';
	import { previewQR as generateQR } from '$lib/utils/qrPreview';
	import { vimModeEnabled } from '$lib/store/vimMode.js';
	import {
		setupVimClipboardIntegration,
		destroyVimClipboardIntegration
	} from '$lib/utils/vimClipboard.js';
	/** @type {'domain'|'page'|'email'} */

	export let contentType;
	export let value;
	export let baseURL = 'example.test';
	export let domainMap = new BiMap({});
	export let selectedDomain = '';
	export let externalVimMode = null; // allow external control of vim mode
	let localVimMode = externalVimMode !== null ? externalVimMode : $vimModeEnabled;
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
		const editorOptions = {
			value: value,
			language: 'html',
			theme: 'vs-dark',
			automaticLayout: true,
			minimap: {
				enabled: false
			},
			fontSize: 13,
			lineNumbers: 'on',
			folding: true,
			wordWrap: 'on',
			contextmenu: true,
			scrollbar: {
				horizontal: 'hidden'
			}
		};

		/* @ts-ignore - editorOptions is not complete */
		editor = monaco.editor.create(editorContainer, editorOptions);

		// vim mode will be initialized by reactive statement if needed

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

	// create shadow dom for iframe isolation
	const createShadowIframe = () => {
		if (!shadowContainer) return;

		// clear existing content
		shadowContainer.innerHTML = '';

		// create shadow root
		const shadowRoot = shadowContainer.attachShadow({ mode: 'closed' });

		// create iframe inside shadow dom
		const iframe = document.createElement('iframe');
		iframe.sandbox = 'allow-forms allow-modals allow-popups allow-scripts allow-pointer-lock';
		iframe.title = 'preview';
		iframe.style.cssText = 'height: 100%; width: 100%; border: none;';

		// add styles to shadow root to isolate it
		const style = document.createElement('style');
		style.textContent = `
			:host {
				display: block;
				height: 100%;
				width: 100%;
				background: white;
			}
			iframe {
				height: 100%;
				width: 100%;
				border: none;
			}
		`;

		shadowRoot.appendChild(style);
		shadowRoot.appendChild(iframe);

		// set as preview frame
		previewFrame = iframe;
	};

	const updatePreview = async () => {
		if (isRenderingPreview) {
			return;
		}
		const v = editor.getValue() ?? value;
		value = v;
		const content = await replaceTemplateVariables(v);

		// create shadow iframe if not exists
		if (shadowContainer && !previewFrame) {
			createShadowIframe();
		}

		if (previewFrame) {
			// use data url for null origin isolation
			previewFrame.src = 'data:text/html;charset=utf-8,' + encodeURIComponent(content);
		}
		if (externalFrameRef) {
			const embedContent = createEmbed(content);
			const dataUrl = 'data:text/html;charset=utf-8,' + encodeURIComponent(embedContent);
			externalFrameRef.location.replace(dataUrl);
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
            src="data:text/html;charset=utf-8,${encodeURIComponent(content)}"></iframe>
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
</script>

<div
	class="w-80vw z-[9000] col-start-1 col-end-4 flex flex-col bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 transition-colors duration-200"
>
	<div
		class="bg-white dark:bg-gray-800 transition-colors duration-200"
		class:fixed={isExpanded}
		class:inset-0={isExpanded}
		class:z-50={isExpanded}
		style={isExpanded ? 'width: 100vw; height: 100vh;' : ''}
		role={isExpanded ? 'dialog' : undefined}
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
			class="flex items-center flex-wrap gap-2 bg-slate-900 w-full justify-start p-4 rounded-t-md"
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

			<!-- vim mode toggle button -->
			<button
				type="button"
				on:click={() => {
					vimModeEnabled.update((v) => !v);
				}}
				class="h-8 border-2 rounded-md w-36 px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 transition-colors duration-200"
				class:font-bold={localVimMode}
				class:bg-blue-600={localVimMode}
				class:dark:bg-blue-500={localVimMode}
				class:text-white={localVimMode}
				class:border-blue-600={localVimMode}
				class:dark:border-blue-500={localVimMode}
				class:text-gray-700={!localVimMode}
				class:dark:text-gray-200={!localVimMode}
				class:bg-white={!localVimMode}
				class:dark:bg-gray-700={!localVimMode}
				class:border-gray-300={!localVimMode}
				class:dark:border-gray-600={!localVimMode}
			>
				<svg
					xmlns="http://www.w3.org/2000/svg"
					class="h-4 w-4"
					viewBox="0 0 20 20"
					fill="currentColor"
				>
					<path d="M3 3h18v18H3V3zm2 2v14h14V5H5zm2 2h10v2H7V7zm0 4h10v2H7v-2zm0 4h6v2H7v-2z" />
				</svg>
				<span>Vim</span>
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
			<select
				id="domain-select"
				bind:value={selectedDomain}
				on:change={selectPreviewDomain}
				class="h-8 w-64 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 bg-white dark:bg-gray-700 text-black dark:text-gray-200 cursor-pointer transition-colors duration-200 text-ellipsis"
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
				class="h-8 border-2 rounded-md w-36 px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 transition-colors duration-200"
				class:font-bold={isPreviewVisible}
				class:bg-blue-600={isPreviewVisible}
				class:dark:bg-blue-500={isPreviewVisible}
				class:text-white={isPreviewVisible}
				class:border-blue-600={isPreviewVisible}
				class:dark:border-blue-500={isPreviewVisible}
				class:text-gray-700={!isPreviewVisible}
				class:dark:text-gray-200={!isPreviewVisible}
				class:bg-white={!isPreviewVisible}
				class:dark:bg-gray-700={!isPreviewVisible}
				class:border-gray-300={!isPreviewVisible}
				class:dark:border-gray-600={!isPreviewVisible}
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
			<button
				type="button"
				on:click={() => (isExpanded = !isExpanded)}
				class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
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
					<span>Expand</span>
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
					<span>Collapse</span>
				{/if}
			</button>
		</div>
		<!-- editor below controls -->
		<!-- editor and preview side-by-side -->
		<div
			class={isExpanded
				? 'flex flex-row w-full h-[calc(100vh-64px)] overflow-hidden'
				: 'flex flex-row w-full h-[55vh] overflow-hidden'}
		>
			<div
				class="flex flex-col relative h-full transition-colors duration-200 {isPreviewVisible
					? 'w-1/2'
					: 'w-full'}"
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
					class="w-1/2 border-2 border-black dark:border-gray-600 bg-white transition-colors duration-200 h-full"
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
