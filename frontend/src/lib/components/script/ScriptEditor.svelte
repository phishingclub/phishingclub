<script>
	import { onMount } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker';
	import { vimModeEnabled } from '$lib/store/vimMode.js';
	import {
		setupVimClipboardIntegration,
		destroyVimClipboardIntegration
	} from '$lib/utils/vimClipboard.js';
	import * as vimModule from 'monaco-vim';
	import TextField from '$lib/components/TextField.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import SimpleCodeEditor from '$lib/components/editor/SimpleCodeEditor.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { eventDisplayLabel } from '$lib/consts/events.js';


	// --- test runner state ---------------------------------------------------
	const triggerEvents = [
		'campaign_recipient_message_sent',
		'campaign_recipient_message_read',
		'campaign_recipient_before_page_visited',
		'campaign_recipient_page_visited',
		'campaign_recipient_after_page_visited',
		'campaign_recipient_submitted_data',
		'campaign_recipient_reported',
		'campaign_recipient_evasion_page_visited',
		'campaign_recipient_deny_page_visited',
		'campaign_recipient_training_started',
		'campaign_recipient_training_completed'
	];
	const triggerEventOptions = triggerEvents.map((e) => ({ value: e, label: eventDisplayLabel(e) }));
	// only submitted_data carries a data payload (the parsed form body, same as the
	// webhook data); every other event has no data attached
	const dataBearingEvents = new Set(['campaign_recipient_submitted_data']);
	let testEventName = 'campaign_recipient_submitted_data';
	let testCampaignName = 'Test Campaign';
	let testEmail = 'target@example.test';
	let testDataText = '{\n  "username": "victim",\n  "password": "hunter2"\n}';
	let testResult = null;
	let testError = '';
	let testing = false;
	// 'script' shows the editor full-size, 'test' shows the test runner full-size
	let view = 'script';
	$: eventHasData = dataBearingEvents.has(testEventName);

	async function runTest() {
		testError = '';
		testResult = null;
		let data = {};
		if (eventHasData && testDataText.trim()) {
			try {
				data = JSON.parse(testDataText);
			} catch (e) {
				testError = 'Event data is not valid JSON: ' + e.message;
				return;
			}
		}
		testing = true;
		try {
			const res = await api.script.test({
				script,
				event: { name: testEventName, campaignName: testCampaignName, email: testEmail, data }
			});
			if (!res.success) {
				testError = res.error || 'Test failed';
				return;
			}
			testResult = res.data;
		} catch (e) {
			testError = e?.message || String(e);
		} finally {
			testing = false;
		}
	}

	// Props
	/** @type {string} */
	export let name = '';
	/** @type {string} script is the JS source to edit */
	export let script = '';

	let editor;
	let editorContainer;
	let completionProvider;
	let isDark = false;
	let vimStatusBarEl = null;
	let vimModeInstance = null;
	let localVimMode = false;
	let isDestroyed = false;

	// scriptDTS is the single source of truth the editor consumes for
	// autocomplete and inline type checking. It must match the bindings the
	// backend script engine exposes (backend/script/script.go).
	const scriptDTS = `
/** The event that triggered this script. Already filtered for the
 * campaign's anonymity and the configured data level, so fields may be empty. */
declare const event: {
	/** event name, e.g. "campaign_recipient_submitted_data" */
	name: string;
	/** the campaign id */
	campaignId: string;
	/** the recipient id, empty for campaign level events */
	recipientId: string;
	/** the campaign name, empty at the "none" data level */
	campaignName: string;
	/** the recipient email, only at "full" level on a non anonymous campaign */
	email: string;
	/** captured data, only at "full" level */
	data: Record<string, any>;
};

interface FetchOptions {
	/** HTTP method: GET (default), POST, PUT, PATCH, DELETE, ... */
	method?: string;
	/** request headers, e.g. { 'Content-Type': 'application/json' } */
	headers?: Record<string, string>;
	/** request body (a string; use encode.json(obj) for JSON) */
	body?: string;
	/** route this request through a proxy: http://, https:// or socks5:// */
	proxy?: string;
	/** per request timeout in milliseconds (max 30000) */
	timeoutMs?: number;
}

interface FetchResponse {
	/** HTTP status code, e.g. 200 */
	status: number;
	/** response headers */
	headers: Record<string, string>;
	/** response body as a string (use decode.json(res.body) to parse JSON) */
	body: string;
}

declare const http: {
	/**
	 * Send a synchronous HTTP request and return the response. Any method is
	 * supported. Blocks until the response arrives or the timeout fires; throws
	 * on a network error, so wrap it in try/catch if you want to handle failures.
	 *
	 * @example
	 * // GET and parse JSON
	 * const res = http.fetch('https://api.example.test/users/42');
	 * if (res.status === 200) {
	 *   const user = decode.json(res.body);
	 *   log('got user', { name: user.name });
	 * }
	 *
	 * @example
	 * // capture from a response body with a regex — the whole JS RegExp API
	 * // works (match/matchAll/exec/replace/test); use numbered groups (m[1])
	 * const res = http.fetch('https://api.example.test/login');
	 * const m = res.body.match(/"csrf_token":"([A-Za-z0-9._-]+)"/);
	 * const token = m && m[1];
	 *
	 * @example
	 * // POST JSON with headers
	 * const res = http.fetch('https://api.example.test/hook', {
	 *   method: 'POST',
	 *   headers: { 'Content-Type': 'application/json' },
	 *   body: encode.json({ event: event.name, email: event.email })
	 * });
	 *
	 * @example
	 * // PUT with a signed header and a timeout
	 * http.fetch('https://api.example.test/items/1', {
	 *   method: 'PUT',
	 *   headers: { 'X-Signature': hmac.sha256(secret, body) },
	 *   body: body,
	 *   timeoutMs: 5000
	 * });
	 */
	fetch(url: string, options?: FetchOptions): FetchResponse;
};

/** Output encoding for hash / hmac / random. Defaults to "hex". */
type OutputEncoding = 'hex' | 'base64' | 'base64url' | 'base32';

declare const encode: {
	base64(s: string): string;
	/** url-safe base64, no padding (JWT / OAuth) */
	base64url(s: string): string;
	base32(s: string): string;
	hex(s: string): string;
	/** query escape (space as +) */
	url(s: string): string;
	/** path segment escape */
	urlPath(s: string): string;
	html(s: string): string;
	/** JSON stringify any value */
	json(value: any): string;
	/** object -> application/x-www-form-urlencoded */
	form(obj: Record<string, any>): string;
	/** gzip then base64 */
	gzip(s: string): string;
	/** raw deflate then base64 */
	deflate(s: string): string;
};

declare const decode: {
	base64(s: string): string;
	base64url(s: string): string;
	base32(s: string): string;
	hex(s: string): string;
	url(s: string): string;
	urlPath(s: string): string;
	html(s: string): string;
	/** JSON parse a string into a value */
	json(s: string): any;
	/** form-encoded string -> object (repeated key -> array) */
	form(s: string): Record<string, any>;
	/** base64 of gzip -> string */
	gzip(s: string): string;
	/** base64 of deflate -> string */
	deflate(s: string): string;
};

/** Hashing. hash.sha256(input) -> hex; pass an encoding for base64 etc. */
declare const hash: {
	md5(input: string, encoding?: OutputEncoding): string;
	sha1(input: string, encoding?: OutputEncoding): string;
	sha256(input: string, encoding?: OutputEncoding): string;
	sha384(input: string, encoding?: OutputEncoding): string;
	sha512(input: string, encoding?: OutputEncoding): string;
};

/** Keyed HMAC signing, e.g. for request or webhook signatures. */
declare const hmac: {
	sha1(key: string, message: string, encoding?: OutputEncoding): string;
	sha256(key: string, message: string, encoding?: OutputEncoding): string;
	sha384(key: string, message: string, encoding?: OutputEncoding): string;
	sha512(key: string, message: string, encoding?: OutputEncoding): string;
};

declare const jwt: {
	/** Split and JSON-parse a JWT. Does NOT verify the signature. */
	decode(token: string): { header: any; payload: any; signature: string };
};

/** Secure random for nonces, PKCE verifiers, OAuth state. */
declare const random: {
	/** n random bytes (1..4096) in the given encoding, default hex */
	bytes(n: number, encoding?: OutputEncoding): string;
	uuid(): string;
};

/** Write a line to the SERVER logs (for debugging). Not visible in the app. */
declare function log(message: string, data?: any): void;

/** Record a campaign_recipient_info event, visible in the campaign timeline.
 * The detail follows the campaign's data-retention and anonymity rules.
 * Uncaught exceptions and timeouts are recorded automatically the same way. */
declare function info(message: string, data?: Record<string, any>): void;

/** The events a script may create with emitEvent. Server-detected outcomes
 * (opens, clicks, reports, delivery, training) cannot be forged. */
type EmittableEvent = 'campaign_recipient_submitted_data' | 'campaign_recipient_info';

/** Create a new campaign event in this same campaign and recipient context, for
 * data the script itself authored (e.g. a token obtained via http.fetch). Goes
 * through the same storage and anonymization rules as native capture: data is
 * stored only when the campaign keeps submitted data, and stripped on anonymous
 * campaigns.
 *
 * @example
 * // record a token the script redeemed as submitted data
 * emitEvent('campaign_recipient_submitted_data', { accessToken: token }); */
declare function emitEvent(name: EmittableEvent, data?: Record<string, any>): void;

/** End the script early. */
declare function stop(): void;
`;

	const defaultScript = `// Runs when a subscribed campaign event fires.
// Available: event, http.fetch, encode, decode, hash, hmac, jwt, random, log, info, emitEvent, stop
// (encode/decode/hash/hmac/jwt/random are the data transform toolkit)
log('event received', { name: event.name });

// event.data holds the submitted form fields on a submitted_data event.
// Read whatever fields your landing page posts:
var username = event.data && event.data.username;
if (username) {
	info('captured a submission', { username: username });
}

// Example: forward the submitted data to an external service (set a real URL)
// const res = http.fetch('https://your-endpoint.test/hook', {
//   method: 'POST',
//   headers: { 'Content-Type': 'application/json' },
//   body: encode.json({ event: event.name, email: event.email, data: event.data })
// });
// const parsed = decode.json(res.body);
// log('forwarded', { status: res.status, id: parsed.id });
`;

	function destroyVimMode() {
		if (vimModeInstance) {
			destroyVimClipboardIntegration();
			vimModeInstance.dispose();
			vimModeInstance = null;
		}
	}

	onMount(() => {
		if (!script) {
			script = defaultScript;
		}
		isDark = document.documentElement.classList.contains('dark');

		const observer = new MutationObserver(() => {
			const newIsDark = document.documentElement.classList.contains('dark');
			if (newIsDark !== isDark) {
				isDark = newIsDark;
				if (editor) monaco.editor.setTheme(isDark ? 'vs-dark' : 'vs-light');
			}
		});
		observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });

		/* @ts-ignore */
		self.MonacoEnvironment = {
			getWorker: function (_, label) {
				if (label === 'typescript' || label === 'javascript') {
					return new tsWorker();
				}
				return new editorWorker();
			}
		};

		// noLib removes the browser DOM lib so dot completion only shows the
		// declared script API. 1108 is 'return' outside function, valid here
		// because the script runs inside an implicit IIFE on the backend.
		monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
			noSemanticValidation: false,
			noSyntaxValidation: false,
			diagnosticCodesToIgnore: [1108]
		});
		monaco.languages.typescript.javascriptDefaults.setCompilerOptions({
			noLib: true,
			allowJs: true,
			checkJs: true,
			allowNonTsExtensions: true,
			target: monaco.languages.typescript.ScriptTarget.ES2020,
			strict: false
		});
		completionProvider = monaco.languages.typescript.javascriptDefaults.addExtraLib(
			scriptDTS,
			'ts:script.d.ts'
		);

		editor = monaco.editor.create(editorContainer, {
			value: script,
			language: 'javascript',
			theme: isDark ? 'vs-dark' : 'vs-light',
			minimap: { enabled: false },
			wordWrap: 'off',
			folding: false,
			scrollBeyondLastLine: false,
			fontSize: 13,
			automaticLayout: true
		});

		editor.onDidChangeModelContent(() => {
			script = editor.getValue();
		});

		const unsubVim = vimModeEnabled.subscribe((enabled) => {
			if (isDestroyed) return;
			localVimMode = enabled;
			if (enabled) {
				vimModeInstance = vimModule.initVimMode(editor, vimStatusBarEl);
				setupVimClipboardIntegration(editor, vimModeInstance, localVimMode, monaco);
			} else {
				destroyVimMode();
			}
		});

		return () => {
			isDestroyed = true;
			observer.disconnect();
			unsubVim();
			destroyVimMode();
			if (completionProvider) completionProvider.dispose();
			if (editor) editor.dispose();
		};
	});
</script>

<div class="flex h-full min-h-0 flex-col gap-3">
	<!-- top bar: name + view toggle -->
	<div class="flex items-end gap-3">
		<div class="min-w-0 flex-1">
			<TextField bind:value={name} placeholder="my-script">Name</TextField>
		</div>
		<div
			class="mb-2 inline-flex overflow-hidden rounded-md border border-gray-300 text-sm dark:border-gray-600"
		>
			<button
				type="button"
				on:click={() => (view = 'script')}
				class="px-3 py-1.5 font-medium transition-colors {view === 'script'
					? 'bg-blue-500 text-white'
					: 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'}"
			>
				Script
			</button>
			<button
				type="button"
				on:click={() => (view = 'test')}
				class="px-3 py-1.5 font-medium transition-colors {view === 'test'
					? 'bg-blue-500 text-white'
					: 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'}"
			>
				Test run
			</button>
		</div>
	</div>

	<!-- body: editor stays mounted; the test runner overlays it full-size -->
	<div class="relative min-h-0 flex-1">
		<!-- editor (kept sized with invisible, not display:none, so Monaco stays laid out) -->
		<div
			class="flex h-full flex-col overflow-hidden rounded border border-gray-300 dark:border-gray-700"
			class:invisible={view !== 'script'}
		>
			<div
				class="flex items-center justify-between px-3 py-1 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700"
			>
				<span class="text-xs font-mono text-gray-500 dark:text-gray-400">JavaScript</span>
				<button
					type="button"
					title="Toggle vim mode"
					on:click={() => vimModeEnabled.update((v) => !v)}
					class="h-8 border-2 rounded-md w-20 px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 transition-colors duration-200"
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
					<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
						<path d="M3 3h18v18H3V3zm2 2v14h14V5H5zm2 2h10v2H7V7zm0 4h10v2H7v-2zm0 4h6v2H7v-2z" />
					</svg>
					<span class="text-xs">Vim</span>
				</button>
			</div>
			<div bind:this={editorContainer} class="flex-1" style="min-height: 0;"></div>
			<div
				bind:this={vimStatusBarEl}
				class="h-5 bg-gray-100 dark:bg-gray-800 text-xs text-gray-500 dark:text-gray-400 px-2"
			></div>
		</div>

		{#if view === 'test'}
			<!-- fills the body; below lg it stacks and scrolls -->
			<div
				class="absolute inset-0 flex min-h-0 flex-col gap-4 overflow-y-auto lg:flex-row lg:overflow-hidden"
			>
				<!-- LEFT: input controls (scroll) with the Run button pinned below -->
				<div class="flex min-h-0 flex-none flex-col lg:w-80 xl:w-96">
					<div class="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto pr-1">
						<TextFieldSelect
							id="script-test-event"
							bind:value={testEventName}
							options={triggerEventOptions}
							size="normal"
						>
							Event
						</TextFieldSelect>
						<TextField bind:value={testCampaignName}>Campaign name</TextField>
						<TextField bind:value={testEmail}>Recipient email</TextField>

						<!-- only campaign_recipient_submitted_data carries data (the parsed form body);
						     other events have no data attached -->
						{#if eventHasData}
							<div class="flex w-full flex-col py-2">
								<p class="py-1 font-semibold text-slate-600 dark:text-gray-400">Event data (JSON)</p>
								<SimpleCodeEditor
									bind:value={testDataText}
									language="json"
									height="medium"
									showVimToggle={false}
									showExpandButton={false}
								/>
							</div>
						{/if}

						{#if testError}
							<div
								class="rounded border border-red-300 bg-red-50 p-2 text-xs text-red-700 dark:border-red-700 dark:bg-red-900/30 dark:text-red-300"
							>
								{testError}
							</div>
						{/if}
					</div>

					<button
						type="button"
						on:click={runTest}
						disabled={testing}
						class="mt-3 flex w-full flex-none items-center justify-center gap-2 rounded-md bg-gradient-to-b from-blue-500 to-indigo-400 px-4 py-2 text-sm font-semibold text-white transition-all duration-200 hover:from-blue-400 hover:to-indigo-400 disabled:cursor-not-allowed disabled:opacity-60 dark:from-blue-600 dark:to-indigo-500"
					>
						{#if testing}
							<span class="inline-block h-2 w-2 animate-pulse rounded-full bg-white"></span>
							Running…
						{:else}
							Run test
						{/if}
					</button>
				</div>

				<!-- RIGHT: run log -->
				<div class="flex min-h-0 min-w-0 flex-1 flex-col">
					<div class="mb-2 flex items-center justify-between">
						<div class="flex items-center gap-2">
							<span
								class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400"
								>Run log</span
							>
							{#if testing}
								<span class="inline-block h-2 w-2 animate-pulse rounded-full bg-green-400"></span>
							{/if}
						</div>
						{#if testResult || testError}
							<button
								type="button"
								on:click={() => {
									testResult = null;
									testError = '';
								}}
								class="text-xs font-medium text-gray-500 transition-colors hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
							>
								Clear
							</button>
						{/if}
					</div>

					<!-- log body (same rendering as the remote browser editor) -->
					<div
						class="min-h-[16rem] flex-1 select-text space-y-0.5 overflow-y-auto rounded bg-gray-900 p-2 font-mono text-xs text-gray-200 lg:min-h-0 dark:bg-gray-950"
					>
						{#if !testResult}
							<span class="text-gray-500">No events yet. Click Run test to execute the script.</span>
						{:else}
							{#each testResult.entries ?? [] as entry}
							<div
								class="leading-5 {entry.type === 'event'
									? 'text-blue-300'
									: entry.type === 'info'
										? 'text-sky-300'
										: entry.type === 'error'
											? 'text-red-400'
											: entry.type === 'done'
												? 'text-green-400'
												: 'text-gray-400'}"
							>
								{#if entry.type === 'event'}
									<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
									<span class="text-blue-400"> emit </span>
									<span class="text-yellow-400">{entry.key}</span>
									<span class="text-gray-300"> = </span>
									<span>{JSON.stringify(entry.value)}</span>
								{:else if entry.type === 'info'}
									<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
									<span class="text-sky-400"> ℹ info</span>
									<span class="ml-1 text-sky-200">{entry.message}</span>
									{#if entry.data !== undefined && entry.data !== null}
										<span class="text-cyan-300"> {JSON.stringify(entry.data)}</span>
									{/if}
								{:else if entry.type === 'done'}
									<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
									<span class="text-green-400"> ✓ done</span>
								{:else}
									<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
									<span> {entry.message}</span>
									{#if entry.data !== undefined && entry.data !== null}
										<span class="text-cyan-300"> {JSON.stringify(entry.data)}</span>
									{/if}
								{/if}
							</div>
						{/each}
					{/if}
					</div>
				</div>
			</div>
		{/if}
	</div>
</div>
