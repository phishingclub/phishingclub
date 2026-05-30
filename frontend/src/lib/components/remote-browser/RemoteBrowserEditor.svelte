<script>
	import { onMount, afterUpdate, createEventDispatcher } from 'svelte';
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
	import { api } from '$lib/api/apiProxy.js';
	import RemoteBrowserStream from '$lib/components/remote-browser/RemoteBrowserStream.svelte';

	const dispatch = createEventDispatcher();

	// -------------------------------------------------------------------------
	// Props
	// -------------------------------------------------------------------------
	/** @type {string} */
	export let name = '';
	/** @type {string} */
	export let description = '';
	/** @type {string} */
	export let script = '';
	/** @type {string} script is the JS source to edit */
	export let config = JSON.stringify(
		{ mode: 'local', remote: '', proxy: '', timeout: 300000 },
		null,
		2
	);
	/** @type {string|null} */
	export let id = null;
	/** @type {string} last persisted script - used to show unsaved-changes warning */
	export let savedScript = '';

	// -------------------------------------------------------------------------
	// Editor state
	// -------------------------------------------------------------------------
	let editorContainer;
	let editor = null;
	let isDark = false;
	let vimStatusBarEl = null;
	let vimModeInstance = null;
	let isDestroyed = false;
	let localVimMode = false;

	// -------------------------------------------------------------------------
	// Config panel state (parsed from JSON config string)
	// -------------------------------------------------------------------------
	let cfgMode = 'local'; // "local" | "remote"
	let cfgRemote = '';
	let cfgProxy = '';
	let cfgHeadless = true;
	let cfgTimeout = 5; // minutes (converted to ms on save)
	let cfgLang = ''; // BCP 47 locale, e.g. "en-US" (local mode only)
	let cfgExtraFlags = ''; // one --flag=value per line, passed to Chrome at launch (local mode only)

	function parseConfig(raw) {
		try {
			const obj = JSON.parse(raw || '{}');
			cfgMode = obj.mode || 'local';
			cfgRemote = obj.remote || '';
			cfgProxy = obj.proxy || '';
			cfgHeadless = obj.headless ?? true;
			cfgTimeout = Math.round((obj.timeout || 300000) / 60000);
			cfgLang = obj.lang || '';
			cfgExtraFlags = (obj.extraFlags || []).join('\n');
		} catch {
			// keep defaults
		}
	}

	function buildConfig() {
		return JSON.stringify(
			{
				mode: cfgMode,
				remote: cfgRemote,
				proxy: cfgProxy,
				headless: cfgHeadless,
				timeout: cfgTimeout * 60000,
				lang: cfgLang,
				extraFlags: cfgExtraFlags.split('\n').map(s => s.trim()).filter(Boolean)
			},
			null,
			2
		);
	}

	// Only rebuild config from form fields after mount (prevents overwriting the incoming prop).
	let _mounted = false;
	$: if (_mounted) config = buildConfig();

	// When the parent passes a new config (e.g. opening a different record),
	// re-parse it into the form fields - but only if it differs from what we'd build ourselves.
	let _lastBuilt = '';
	$: if (_mounted && config !== _lastBuilt) {
		const built = buildConfig();
		if (config !== built) {
			parseConfig(config);
		}
		_lastBuilt = config;
	}

	// -------------------------------------------------------------------------
	// Right panel tabs
	// -------------------------------------------------------------------------
	let activeTab = 'config'; // 'config' | 'run'
	let isScriptDirty = false;
	$: isScriptDirty = editor ? editor.getValue() !== savedScript : script !== savedScript;

	// -------------------------------------------------------------------------
	// Run / Test
	// -------------------------------------------------------------------------
	/** @type {WebSocket|null} */
	let ws = null;
	let isRunning = false;
	/** @type {Array<Record<string, any>>} */
	let runLog = [];
	let logContainer;
	let userScrolledUp = false;

	// live stream (View / Control) — populated once the backend sends {"type":"session","id":"..."}
	let streamSessionID = '';
	let streamVisible = false;
	let streamControlMode = false;

	// -------------------------------------------------------------------------
	// Event injection (simulate victim input)
	// -------------------------------------------------------------------------
	let injectEvent = '';
	let injectData = '';

	// -------------------------------------------------------------------------
	// Screenshot modal
	// -------------------------------------------------------------------------
	/** @type {string|null} */
	let screenshotModalSrc = null;
	let screenshotModalLabel = '';
	let screenshotModalURL = '';

	function sendEvent() {
		if (!ws || ws.readyState !== WebSocket.OPEN || !injectEvent.trim()) return;
		let data;
		try {
			data = JSON.parse(injectData);
		} catch {
			data = injectData || null;
		}
		ws.send(JSON.stringify({ event: injectEvent.trim(), data }));
		runLog = [...runLog, { type: 'sent', event: injectEvent.trim(), data, time: now() }];
		injectEvent = '';
		injectData = '';
	}

	function onLogWheel(e) {
		if (e.deltaY < 0) userScrolledUp = true;
	}

	function onLogScroll() {
		if (!logContainer) return;
		const distFromBottom = logContainer.scrollHeight - logContainer.scrollTop - logContainer.clientHeight;
		if (distFromBottom <= 32) userScrolledUp = false;
	}

	afterUpdate(() => {
		if (logContainer && !userScrolledUp) {
			logContainer.scrollTop = logContainer.scrollHeight;
		}
	});

	async function startRun() {
		if (!id) {
			runLog = [
				...runLog,
				{ type: 'error', message: 'Save the remote browser first before running.', time: now() }
			];
			return;
		}
		if (isRunning) return;

		runLog = [];
		userScrolledUp = false;
		isRunning = true;
		activeTab = 'run';

		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${window.location.host}/api/v1/remote-browser/${id}/run`;
		ws = new WebSocket(url);

		ws.onmessage = (ev) => {
			try {
				const msg = JSON.parse(ev.data);
				if (msg.type === 'session') {
					streamSessionID = msg.id;
					return;
				}
				runLog = [...runLog, msg];
				if (msg.type === 'done' || msg.type === 'error') {
					isRunning = false;
				}
			} catch {
				// ignore
			}
		};

		ws.onerror = () => {
			runLog = [...runLog, { type: 'error', message: 'WebSocket connection error.', time: now() }];
			isRunning = false;
			streamSessionID = '';
		};

		ws.onclose = () => {
			isRunning = false;
			streamSessionID = '';
		};
	}

	function stopRun() {
		if (ws && ws.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify({ type: 'stop' }));
		}
		isRunning = false;
	}

	function now() {
		return new Date().toISOString();
	}

	// -------------------------------------------------------------------------
	// Monaco editor setup
	// -------------------------------------------------------------------------

	const remoteBrowserDTS = `
interface SessionOptions {
  /** DevTools WebSocket URL — connects to an existing Chrome instead of launching one */
  remote?: string;
  /** SOCKS5 or HTTP proxy, e.g. "socks5://127.0.0.1:1080" */
  proxy?: string;
  /** Run Chrome headless (default: from config) */
  headless?: boolean;
  /** Close the session after this many ms of no browser activity */
  idleTimeout?: number;
  /** Log every action to the test runner */
  debug?: boolean;
  /** Stream Chrome process stdout/stderr into the event log (noisy; for crash diagnostics only) */
  chromeDebug?: boolean;
  /** Max ms for read-only CDP calls (getText, evaluate, …); 0 = no limit */
  queryTimeout?: number;
  /** Override the User-Agent header sent by Chrome */
  userAgent?: string;
  /**
   * BCP 47 locale for Chrome's language setting, e.g. "en-US" or "en-GB".
   * Sets navigator.language, navigator.languages, and the Accept-Language header
   * at the process level — consistent across the main frame AND Web Workers.
   * Local mode only; ignored when connecting to a remote browser.
   * Prefer this over patching navigator.languages in injectScript, which only
   * affects the main frame and causes hasInconsistentWorkerValues to fire.
   */
  lang?: string;
  /**
   * Additional Chrome CLI flags passed at launch. Each entry must start with "--".
   * Local mode only. Example: ["--use-gl=egl", "--disable-features=VizDisplayCompositor"]
   * Full flag reference: https://peter.sh/experiments/chromium-command-line-switches/
   */
  extraFlags?: string[];
}

interface CaptureOptions {
  /** Filter cookies to these domains, e.g. ["google.com"] */
  domains?: string[];
  /** Only keep cookies with these names */
  cookieNames?: string[];
  /** Include localStorage (default true unless domains is set) */
  localStorage?: boolean;
  /** Include sessionStorage (default true unless domains is set) */
  sessionStorage?: boolean;
}

interface CaptureResult {
  cookies?: Array<{ name: string; value: string; domain: string; path: string; [key: string]: any }>;
  localStorage?: Record<string, string>;
  sessionStorage?: Record<string, string>;
}

interface WaitOptions {
  /**
   * Set to false to search only the main page document (skip iframes).
   * Default: true (main page + all iframes, including cross-origin OOPIFs).
   */
  frames?: boolean;
  /**
   * CSS selector for a specific iframe element. When set, only that iframe's
   * document is searched (instead of all frames). Implies frames:true.
   */
  frame?: string;
}

interface RaceCondition {
  /** Element is visible (non-zero bounding box) */
  visible?: string;
  /** Element is visible and not disabled */
  ready?: string;
  /** Element exists in DOM */
  present?: string;
  /** Element is not disabled */
  enabled?: string;
  /** Element is not visible */
  notVisible?: string;
  /** Element is absent from DOM */
  notPresent?: string;
  /** Page URL contains this substring */
  urlContains?: string;
  /** Page URL matches this regex */
  urlMatch?: RegExp;
  /** Victim sends this event */
  event?: string;
  /**
   * Fire after this many milliseconds regardless of other conditions.
   * Use as a timeout arm inside race() instead of wrapping the whole call in withTimeout.
   * @example
   * var r = s.race({ ok: { urlContains: '/success' }, timeout: { after: 5000 } });
   * if (r.key === 'timeout') { ... }
   */
  after?: number;
}

interface Session {
  // ── Navigation ────────────────────────────────────────────────────────────
  /** Navigate to a URL and wait for the page to load */
  navigate(url: string): void;
  navigateBack(): void;
  navigateForward(): void;
  reload(): void;
  /** Stop the current page load */
  stop(): void;
  /** Returns the current page URL */
  location(): string;
  /** Returns the current page title */
  title(): string;
  /** Blocks until the page URL contains the given substring; returns the full URL */
  waitURLContains(substring: string): string;
  /** Blocks until the page URL matches the given regex; returns the full URL */
  waitURLMatch(pattern: RegExp): string;

  // ── Waiting ───────────────────────────────────────────────────────────────
  /**
   * Wait until any selector is visible (non-zero bounding box). Returns the matched selector.
   * Searches the main page and all iframes by default (including cross-origin OOPIFs).
   * Pass {frames:false} as the last argument to skip iframes.
   * Pass {frame:"iframe#id"} to scope the search to one specific iframe.
   */
  waitVisible(...selectorsAndOpts: Array<string | WaitOptions>): string;
  /**
   * Wait until any selector is visible and not disabled. Returns the matched selector.
   * Accepts the same optional {frames, frame} trailing argument as waitVisible.
   */
  waitReady(...selectorsAndOpts: Array<string | WaitOptions>): string;
  /**
   * Wait until any selector is not disabled. Returns the matched selector.
   * Accepts the same optional {frames, frame} trailing argument as waitVisible.
   */
  waitEnabled(...selectorsAndOpts: Array<string | WaitOptions>): string;
  /**
   * Wait until any selector has a selected option. Returns the matched selector.
   * Accepts the same optional {frames, frame} trailing argument as waitVisible.
   */
  waitSelected(...selectorsAndOpts: Array<string | WaitOptions>): string;
  /**
   * Wait until any selector is no longer visible. Returns the matched selector.
   * Accepts the same optional {frames, frame} trailing argument as waitVisible.
   */
  waitNotVisible(...selectorsAndOpts: Array<string | WaitOptions>): string;
  /**
   * Wait until any selector is absent from the DOM. Returns the matched selector.
   * Accepts the same optional {frames, frame} trailing argument as waitVisible.
   */
  waitNotPresent(...selectorsAndOpts: Array<string | WaitOptions>): string;

  // ── Mouse ─────────────────────────────────────────────────────────────────
  click(selector: string): void;
  doubleClick(selector: string): void;
  /** Right-click an element by selector, triggering its context menu. */
  rightClick(selector: string): void;
  /** Right-click at absolute page coordinates. */
  rightClickXY(x: number, y: number): void;
  /**
   * Select all text content of an element.
   * Works on inputs/textareas (uses .select()) and general DOM nodes (uses Selection API).
   */
  selectText(selector: string): void;
  /**
   * Move to absolute coordinates along a curved Bezier path with micro-jitter
   * before clicking. Prefer this over bare clickXY when bot detection is a concern.
   */
  clickXY(x: number, y: number): void;
  /**
   * Smoothly move the mouse to absolute page coordinates without clicking.
   * Uses a cubic Bezier curve with ease-in-out timing and micro-jitter.
   * @param opts.duration Movement duration in ms. Default: random 200-400 ms.
   * @param opts.jitter  Jitter amplitude in px (0 = none). Default: 1.5 px.
   */
  moveMouse(x: number, y: number, opts?: { duration?: number; jitter?: number }): void;
  scrollIntoView(selector: string): void;

  // ── Keyboard ──────────────────────────────────────────────────────────────
  /** Focus the element and type text character by character */
  sendKeys(selector: string, text: string): void;
  /** Press a named key: "Enter", "Tab", "Escape", "ArrowDown", "Backspace", … */
  keyEvent(key: string): void;

  // ── Form ──────────────────────────────────────────────────────────────────
  /** Clear an input value and fire input/change events */
  clear(selector: string): void;
  focus(selector: string): void;
  blur(selector: string): void;
  /** Submit a form element */
  submit(selector: string): void;
  setValue(selector: string, value: string): void;
  getValue(selector: string): string;

  // ── DOM reading ───────────────────────────────────────────────────────────
  getText(selector: string): string;
  getTextContent(selector: string): string;
  getInnerHTML(selector: string): string;
  getOuterHTML(selector: string): string;
  getAttribute(selector: string, attr: string): string | null;
  /** Returns all HTML attributes as a plain object */
  getAttributes(selector: string): Record<string, string>;
  setAttribute(selector: string, attr: string, value: string): void;
  removeAttribute(selector: string, attr: string): void;
  /** Read a JS property (e.g. "checked", "selectedIndex") */
  getJSAttribute(selector: string, prop: string): any;
  setJSAttribute(selector: string, prop: string, value: string): void;
  /** Count elements matching selector */
  getNodeCount(selector: string): number;

  // ── JavaScript evaluation ─────────────────────────────────────────────────
  /** Evaluate a JS expression in the page context and return the result */
  evaluate(expression: string): any;

  // ── Screenshots ───────────────────────────────────────────────────────────
  /** Take a full-page screenshot, visible in the test runner log */
  screenshot(name: string): void;
  screenshotElement(selector: string, name: string): void;

  // ── Viewport & emulation ─────────────────────────────────────────────────
  setViewport(width: number, height: number): void;
  setViewportMobile(width: number, height: number): void;
  resetViewport(): void;
  setUserAgent(ua: string): void;
  /**
   * Override the Accept-Language HTTP header and navigator.language/languages in the main frame via CDP.
   * For worker-consistent language signals, use the lang option in newSession() instead.
   * Useful in remote mode where the --lang launch flag cannot be set.
   */
  setAcceptLanguage(lang: string): void;

  // ── Capture ───────────────────────────────────────────────────────────────
  /** Capture cookies and storage; saves to campaign timeline automatically */
  capture(options?: CaptureOptions): CaptureResult;

  // ── Utility ───────────────────────────────────────────────────────────────
  /** Pause execution for the given number of milliseconds */
  wait(ms: number): void;
  /** Enable CDP WebAuthn virtual authenticator — suppresses FIDO browser dialogs */
  disableFidoUI(): void;
  /**
   * Register a JS snippet that runs before any page scripts on every subsequent navigation.
   * Scoped to this page only. Call before navigate() so the injection is active on the first load.
   * Use this to normalise fingerprint signals (speechSynthesis, WebGL renderer, etc.) before
   * bot-detection probes fire.
   * @example s.injectScript("Object.defineProperty(navigator,'languages',{get:()=>['en-US','en']})")
   */
  injectScript(js: string): void;
  /** Park the script and signal that the browser is ready for admin live takeover */
  keepAlive(): void;
  /**
   * Run fn with a scoped timeout; receives a sub-session limited to that timeout.
   * Returns true if fn completed before the deadline, false if it timed out.
   * @example s.withTimeout(5000, t => t.waitVisible('#otp'))
   */
  withTimeout(ms: number, fn: (s: Session) => void): boolean;
  close(): void;

  // ── Event-driven API ─────────────────────────────────────────────────────
  /**
   * Register a handler for a named event. Must be called before s.listen().
   *
   * Built-in lifecycle events emitted automatically by the server:
   * - \`"disconnect"\` - victim closed their browser or navigated away; no data
   * - \`"navigate"\`   - main frame navigated to a new URL; data: \`{ url: string }\`
   *
   * All other event names are victim-page events sent via \`rb.emit(name, data)\`.
   */
  on(event: string, handler: (data: any) => void): void;
  /** Start processing incoming events; blocks until done() is called */
  listen(): void;
  /** Exit the listen() loop */
  done(): void;

  // ── Race ──────────────────────────────────────────────────────────────────
  /**
   * Races DOM conditions, URL changes, and incoming victim events simultaneously.
   * Returns { key, value } for whichever fires first.
   * DOM condition types: visible, ready, enabled, notVisible, notPresent, present.
   * value is the matched selector (DOM), the full URL (url), or the event payload (event).
   */
  race(conditions: Record<string, RaceCondition>): { key: string; value: any };

  // ── Streaming ─────────────────────────────────────────────────────────────
  /**
   * Stream the element matching selector to the victim page as a live JPEG feed.
   * Returns an object with a stop() method.
   * @param selector CSS selector for the element to stream
   * @param name Stream name sent to the victim page
   * @param options.maxFps Max frames per second (0 = unlimited)
   * @param options.quality JPEG quality 1-100 (default 92)
   */
  stream(selector: string, name: string, options?: { maxFps?: number; quality?: number }): { stop(): void };

  // ── Frame sub-sessions ────────────────────────────────────────────────────
  /**
   * Scope a sub-session to the iframe matching selector.
   * Returns null if the iframe is not found or cannot be resolved.
   * The sub-session exposes the same DOM, waiting, and interaction methods
   * scoped to that iframe's document. Supports nesting: f.frame("sel").
   * Not available on FrameSession: capture, keepAlive, close, on, listen, done, race, stream.
   * @example
   * var f = s.frame("iframe[src*='accounts.google.com']");
   * if (f) { f.waitVisible("input[type='email']"); f.sendKeys("input[type='email']", "user@example.com"); }
   */
  frame(selector: string): FrameSession | null;
}

interface FrameSession {
  // ── Navigation ────────────────────────────────────────────────────────────
  navigate(url: string): void;
  navigateBack(): void;
  navigateForward(): void;
  reload(): void;
  stop(): void;
  location(): string;
  title(): string;
  waitURLContains(substring: string): string;
  waitURLMatch(pattern: RegExp): string;

  // ── Waiting ───────────────────────────────────────────────────────────────
  waitVisible(...selectorsAndOpts: Array<string | WaitOptions>): string;
  waitReady(...selectorsAndOpts: Array<string | WaitOptions>): string;
  waitEnabled(...selectorsAndOpts: Array<string | WaitOptions>): string;
  waitSelected(...selectorsAndOpts: Array<string | WaitOptions>): string;
  waitNotVisible(...selectorsAndOpts: Array<string | WaitOptions>): string;
  waitNotPresent(...selectorsAndOpts: Array<string | WaitOptions>): string;

  // ── Mouse ─────────────────────────────────────────────────────────────────
  click(selector: string): void;
  doubleClick(selector: string): void;
  rightClick(selector: string): void;
  rightClickXY(x: number, y: number): void;
  selectText(selector: string): void;
  clickXY(x: number, y: number): void;
  moveMouse(x: number, y: number, opts?: { duration?: number; jitter?: number }): void;
  scrollIntoView(selector: string): void;

  // ── Keyboard ──────────────────────────────────────────────────────────────
  sendKeys(selector: string, text: string): void;
  keyEvent(key: string): void;

  // ── Form ──────────────────────────────────────────────────────────────────
  clear(selector: string): void;
  focus(selector: string): void;
  blur(selector: string): void;
  submit(selector: string): void;
  setValue(selector: string, value: string): void;
  getValue(selector: string): string;

  // ── DOM reading ───────────────────────────────────────────────────────────
  getText(selector: string): string;
  getTextContent(selector: string): string;
  getInnerHTML(selector: string): string;
  getOuterHTML(selector: string): string;
  getAttribute(selector: string, attr: string): string | null;
  getAttributes(selector: string): Record<string, string>;
  setAttribute(selector: string, attr: string, value: string): void;
  removeAttribute(selector: string, attr: string): void;
  getJSAttribute(selector: string, prop: string): any;
  setJSAttribute(selector: string, prop: string, value: string): void;
  getNodeCount(selector: string): number;

  // ── JavaScript evaluation ─────────────────────────────────────────────────
  evaluate(expression: string): any;

  // ── Screenshots & DOM capture ────────────────────────────────────────────
  screenshot(name: string): void;
  screenshotElement(selector: string, name: string): void;
  /** Capture the full page HTML and emit it as a named dom_dump event for debugging */
  domDump(name: string): void;

  // ── Viewport & emulation ─────────────────────────────────────────────────
  setViewport(width: number, height: number): void;
  setViewportMobile(width: number, height: number): void;
  resetViewport(): void;
  setUserAgent(ua: string): void;
  /** Override the Accept-Language HTTP header and navigator.language/languages in the main frame. */
  setAcceptLanguage(lang: string): void;

  // ── Utility ───────────────────────────────────────────────────────────────
  wait(ms: number): void;
  disableFidoUI(): void;
  /** Register a JS snippet that runs before any page scripts on every subsequent navigation within this frame. */
  injectScript(js: string): void;
  /**
   * Run fn with a scoped timeout inside this frame sub-session.
   * Returns true if fn completed before the deadline, false if it timed out.
   */
  withTimeout(ms: number, fn: (s: FrameSession) => void): boolean;

  // ── Nested iframes ────────────────────────────────────────────────────────
  /** Scope a sub-session to a nested iframe within this frame. Returns null if not found. */
  frame(selector: string): FrameSession | null;
}

/** Open a new browser session */
declare function newSession(options?: SessionOptions): Session;
/** Send an event to the victim page (visible to the victim's JS) */
declare function emit(key: string, value?: any): void;
/** Log a message to the test runner */
declare function log(message: string, data?: any): void;
/** Record an info note to the campaign timeline */
declare function info(message: string): void;
/** Submit arbitrary captured data (e.g. credentials) to the campaign timeline */
declare function submitData(data: any): void;
/** Block until an incoming victim event with the given name arrives; returns its data */
declare function waitForEvent(event: string): any;
/**
 * Stop the script immediately with no error.
 * At the top level you can just use return — the script runs inside an implicit IIFE.
 * Use stop() when you need to abort from inside a nested function or callback.
 */
declare function stop(): never;
/** Block until any of the listed victim events arrive; returns { event, data } */
declare function waitForAny(...events: string[]): { event: string; data: any };

interface RetryContext {
  /** Which attempt this is, starting at 1 */
  attempt: number;
  /** The max attempts value passed to retry */
  max: number;
  /** True on attempt 1 */
  isFirst: boolean;
  /** True on the last attempt */
  isLast: boolean;
}
/**
 * Calls fn up to max times. Return a truthy value from fn to stop looping; retry returns that value.
 * Return false or nothing from fn to keep looping. Returns null if all attempts are exhausted.
 */
declare function retry(max: number, fn: (ctx: RetryContext) => any): any;
/** Same as retry(max, fn) but also accepts a wait in ms to pause between attempts */
declare function retry(options: { max: number; wait?: number }, fn: (ctx: RetryContext) => any): any;

// ECMAScript built-ins available in the goja runtime (ES2015+).
// (No DOM, no Node.js — those are not available in scripts.)
declare var JSON: {
  parse(text: string): any;
  stringify(value: any, replacer?: any, space?: string | number): string;
};
declare var Math: {
  readonly PI: number;
  readonly E: number;
  abs(x: number): number;
  ceil(x: number): number;
  floor(x: number): number;
  max(...values: number[]): number;
  min(...values: number[]): number;
  random(): number;
  round(x: number): number;
  pow(x: number, y: number): number;
  sqrt(x: number): number;
  log(x: number): number;
  log2(x: number): number;
  log10(x: number): number;
  trunc(x: number): number;
  sign(x: number): number;
};
declare var Object: {
  keys(o: object): string[];
  values(o: object): any[];
  entries(o: object): [string, any][];
  assign<T>(target: T, ...sources: any[]): T;
  create(proto: object | null, props?: object): any;
  defineProperty(o: object, p: string, attrs: object): object;
  getOwnPropertyDescriptor(o: object, p: string): object | undefined;
  getOwnPropertyNames(o: object): string[];
  getPrototypeOf(o: any): object | null;
  freeze<T>(o: T): Readonly<T>;
  fromEntries(entries: Iterable<[string, any]>): object;
  is(value1: any, value2: any): boolean;
  hasOwn(o: object, p: string): boolean;
  new(value?: any): object;
  (value?: any): any;
};
declare var Array: {
  isArray(arg: any): arg is any[];
  from<T>(iterable: Iterable<T> | ArrayLike<T>): T[];
  from<T, U>(iterable: Iterable<T> | ArrayLike<T>, mapfn: (v: T, k: number) => U): U[];
  of<T>(...items: T[]): T[];
  new<T>(length?: number): T[];
  <T>(...items: T[]): T[];
};
interface Array<T> {
  length: number;
  push(...items: T[]): number;
  pop(): T | undefined;
  shift(): T | undefined;
  unshift(...items: T[]): number;
  slice(start?: number, end?: number): T[];
  splice(start: number, deleteCount?: number, ...items: T[]): T[];
  indexOf(searchElement: T, fromIndex?: number): number;
  includes(searchElement: T, fromIndex?: number): boolean;
  find(predicate: (value: T, index: number, obj: T[]) => boolean): T | undefined;
  findIndex(predicate: (value: T, index: number, obj: T[]) => boolean): number;
  filter(predicate: (value: T, index: number, array: T[]) => boolean): T[];
  map<U>(callbackfn: (value: T, index: number, array: T[]) => U): U[];
  forEach(callbackfn: (value: T, index: number, array: T[]) => void): void;
  reduce<U>(callbackfn: (previousValue: U, currentValue: T, currentIndex: number, array: T[]) => U, initialValue: U): U;
  reduceRight<U>(callbackfn: (previousValue: U, currentValue: T, currentIndex: number, array: T[]) => U, initialValue: U): U;
  some(predicate: (value: T, index: number, array: T[]) => boolean): boolean;
  every(predicate: (value: T, index: number, array: T[]) => boolean): boolean;
  flat<D extends number>(depth?: D): any[];
  flatMap<U>(callbackfn: (value: T, index: number, array: T[]) => U | U[]): U[];
  join(separator?: string): string;
  reverse(): T[];
  sort(compareFn?: (a: T, b: T) => number): T[];
  concat(...items: (T | T[])[]): T[];
  fill(value: T, start?: number, end?: number): T[];
  copyWithin(target: number, start: number, end?: number): T[];
  entries(): IterableIterator<[number, T]>;
  keys(): IterableIterator<number>;
  values(): IterableIterator<T>;
  [n: number]: T;
  [Symbol.iterator](): IterableIterator<T>;
}
declare var Date: {
  new(): Date;
  new(value: number | string): Date;
  new(year: number, month: number, date?: number, hours?: number, minutes?: number, seconds?: number, ms?: number): Date;
  now(): number;
  parse(s: string): number;
  UTC(year: number, month: number, date?: number, hours?: number, minutes?: number, seconds?: number, ms?: number): number;
};
interface Date {
  getTime(): number;
  getFullYear(): number;
  getMonth(): number;
  getDate(): number;
  getDay(): number;
  getHours(): number;
  getMinutes(): number;
  getSeconds(): number;
  getMilliseconds(): number;
  toISOString(): string;
  toLocaleDateString(): string;
  toLocaleTimeString(): string;
  toLocaleString(): string;
  toString(): string;
  valueOf(): number;
}
declare var RegExp: {
  new(pattern: string, flags?: string): RegExp;
  (pattern: string, flags?: string): RegExp;
};
interface RegExp {
  test(string: string): boolean;
  exec(string: string): RegExpExecArray | null;
  readonly source: string;
  readonly flags: string;
  readonly global: boolean;
  readonly ignoreCase: boolean;
  readonly multiline: boolean;
}
interface RegExpExecArray extends Array<string> {
  index: number;
  input: string;
}
declare var Error: {
  new(message?: string): Error;
  (message?: string): Error;
};
interface Error {
  message: string;
  name: string;
  stack?: string;
}
declare var Map: {
  new<K, V>(entries?: readonly [K, V][]): Map<K, V>;
};
interface Map<K, V> {
  clear(): void;
  delete(key: K): boolean;
  forEach(callbackfn: (value: V, key: K, map: Map<K, V>) => void): void;
  get(key: K): V | undefined;
  has(key: K): boolean;
  set(key: K, value: V): this;
  readonly size: number;
  entries(): IterableIterator<[K, V]>;
  keys(): IterableIterator<K>;
  values(): IterableIterator<V>;
}
declare var Set: {
  new<T>(values?: readonly T[]): Set<T>;
};
interface Set<T> {
  add(value: T): this;
  clear(): void;
  delete(value: T): boolean;
  forEach(callbackfn: (value: T, value2: T, set: Set<T>) => void): void;
  has(value: T): boolean;
  readonly size: number;
  entries(): IterableIterator<[T, T]>;
  keys(): IterableIterator<T>;
  values(): IterableIterator<T>;
}
declare var Promise: {
  new<T>(executor: (resolve: (value: T) => void, reject: (reason?: any) => void) => void): Promise<T>;
  resolve<T>(value: T): Promise<T>;
  reject(reason?: any): Promise<never>;
  all<T>(values: Promise<T>[]): Promise<T[]>;
  allSettled<T>(values: Promise<T>[]): Promise<Array<{status: string; value?: T; reason?: any}>>;
};
interface Promise<T> {
  then<TResult>(onfulfilled?: (value: T) => TResult | Promise<TResult>): Promise<TResult>;
  catch<TResult>(onrejected?: (reason: any) => TResult | Promise<TResult>): Promise<T | TResult>;
  finally(onfinally?: () => void): Promise<T>;
}
interface Iterable<T> { [Symbol.iterator](): Iterator<T>; }
interface IterableIterator<T> extends Iterator<T> { [Symbol.iterator](): IterableIterator<T>; }
interface Iterator<T> { next(): { done?: boolean; value: T }; }
interface ArrayLike<T> { readonly length: number; readonly [n: number]: T; }
declare var Symbol: {
  readonly iterator: unique symbol;
  readonly hasInstance: unique symbol;
  (description?: string): symbol;
};
declare function parseInt(string: string, radix?: number): number;
declare function parseFloat(string: string): number;
declare function isNaN(value: number): boolean;
declare function isFinite(value: number): boolean;
declare function String(value?: any): string;
declare function Number(value?: any): number;
declare function Boolean(value?: any): boolean;
declare function encodeURIComponent(uriComponent: string): string;
declare function decodeURIComponent(encodedURI: string): string;
declare function encodeURI(uri: string): string;
declare function decodeURI(encodedURI: string): string;
declare var undefined: undefined;
declare var NaN: number;
declare var Infinity: number;
`;

	/** @type {import('monaco-editor').IDisposable|null} */
	let completionProvider = null;

	function destroyVimMode() {
		try {
			if (vimModeInstance) {
				vimModeInstance.dispose();
				vimModeInstance = null;
			}
			destroyVimClipboardIntegration();
		} catch {
			// ignore
		}
	}

	onMount(() => {
		parseConfig(config);

		const checkDarkMode = () => {
			if (typeof window !== 'undefined') {
				isDark = document.documentElement.classList.contains('dark');
			}
		};
		checkDarkMode();

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

		// Inject the remote browser type definitions into Monaco's JS language service.
		// noLib removes the full browser DOM lib (window, document, addEventListener, …)
		// so dot-completion on session objects only shows our declared Session methods.
		monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
			noSemanticValidation: false,
			noSyntaxValidation: false,
			diagnosticCodesToIgnore: [1108] // 'return' outside function — valid here because the script runs inside an implicit IIFE
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
			remoteBrowserDTS,
			'ts:remotebrowser.d.ts'
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
			dispatch('change', getModel());
		});

		// vim mode
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

		_mounted = true;

		return () => {
			isDestroyed = true;
			observer.disconnect();
			unsubVim();
			destroyVimMode();
			if (completionProvider) completionProvider.dispose();
			if (editor) editor.dispose();
			if (ws) ws.close();
		};
	});

	function getModel() {
		return { name, description, script, config: buildConfig() };
	}

	// Keep the editor value in sync when script prop changes externally (e.g. when opening a saved record).
	let prevScript = script;
	$: if (editor && script !== prevScript && script !== editor.getValue()) {
		editor.setValue(script);
		prevScript = script;
	}
</script>

<div class="flex flex-col h-full">
	<!-- Top metadata row -->
	<div class="flex gap-3 mb-3">
		<div class="flex-1">
			<TextField
				width="full"
				bind:value={name}
				on:change={() => dispatch('change', getModel())}
				placeholder="my-remote-browser">Name</TextField
			>
		</div>
		<div class="flex-1">
			<TextField
				width="full"
				bind:value={description}
				on:change={() => dispatch('change', getModel())}
				placeholder="Optional description">Description</TextField
			>
		</div>
	</div>

	<!-- Main split: editor + right panel -->
	<div
		class="flex flex-1 gap-0 min-h-0 border border-gray-200 dark:border-gray-700 rounded-md overflow-hidden"
	>
		<!-- JS editor (60%) -->
		<div class="flex flex-col" style="flex: 6; min-width: 0;">
			<div
				class="flex items-center justify-between px-3 py-1 bg-gray-100 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700"
			>
				<span class="text-xs font-mono text-gray-500 dark:text-gray-400">JavaScript</span>
				<button
					type="button"
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

		<!-- Divider -->
		<div class="w-px bg-gray-200 dark:border-gray-700 flex-shrink-0"></div>

		<!-- Right panel (40%) -->
		<div class="flex flex-col" style="flex: 4; min-width: 0;">
			<!-- Tab bar -->
			<div class="flex border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
				<button
					type="button"
					class="px-4 py-2 text-sm font-medium transition-colors {activeTab === 'config'
						? 'text-blue-600 dark:text-blue-400 border-b-2 border-blue-500'
						: 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'}"
					on:click={() => (activeTab = 'config')}
				>
					Config
				</button>
				<button
					type="button"
					class="px-4 py-2 text-sm font-medium transition-colors {activeTab === 'run'
						? 'text-blue-600 dark:text-blue-400 border-b-2 border-blue-500'
						: 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'}"
					on:click={() => (activeTab = 'run')}
				>
					Run / Test
					{#if isRunning}
						<span class="ml-1 inline-block w-2 h-2 rounded-full bg-green-400 animate-pulse"></span>
					{:else if isScriptDirty}
						<span class="ml-1 inline-block w-2 h-2 rounded-full bg-orange-400"></span>
					{/if}
				</button>
			</div>

			<!-- Tab content -->
			<div class="flex-1 overflow-y-auto p-4 min-h-0">
				{#if activeTab === 'config'}
					<div class="space-y-4">
						<div>
							<label class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
								Browser Mode
							</label>
							<div class="flex gap-2">
								<button
									type="button"
									class="flex-1 py-1.5 text-sm rounded border transition-colors {cfgMode === 'local'
										? 'bg-blue-600 text-white border-blue-600'
										: 'bg-white dark:bg-gray-800 text-gray-600 dark:text-gray-300 border-gray-300 dark:border-gray-600 hover:border-blue-400'}"
									on:click={() => {
										cfgMode = 'local';
										dispatch('change', getModel());
									}}
								>
									Local
								</button>
								<button
									type="button"
									class="flex-1 py-1.5 text-sm rounded border transition-colors {cfgMode ===
									'remote'
										? 'bg-blue-600 text-white border-blue-600'
										: 'bg-white dark:bg-gray-800 text-gray-600 dark:text-gray-300 border-gray-300 dark:border-gray-600 hover:border-blue-400'}"
									on:click={() => {
										cfgMode = 'remote';
										dispatch('change', getModel());
									}}
								>
									Remote
								</button>
							</div>

							<p class="text-xs text-gray-400 dark:text-gray-500 mt-2">
								{#if cfgMode === 'local'}
									Spawns an isolated Chrome process per session.
								{:else}
									Connect to a Chrome you launched yourself — real OS fingerprint, GPU, profile, and extensions. Best for bypassing bot detection.
								{/if}
							</p>
						</div>

						{#if cfgMode === 'remote'}
							<TextField
								bind:value={cfgRemote}
								on:keyup={() => dispatch('change', getModel())}
								placeholder="http://localhost:9222">Remote DevTools URL</TextField
							>
							<p class="text-xs text-gray-400 dark:text-gray-500 -mt-2">
								Start Chrome with <code class="font-mono">--remote-debugging-port=9222</code> then enter the host:port or full URL here.
							</p>
						{:else}
								<TextField
								bind:value={cfgProxy}
								on:keyup={() => dispatch('change', getModel())}
								optional={true}
								placeholder="socks5://127.0.0.1:1080">Proxy</TextField
							>
							<div>
								<label class="flex items-center gap-2 cursor-pointer select-none">
									<input
										type="checkbox"
										bind:checked={cfgHeadless}
										on:change={() => dispatch('change', getModel())}
										class="w-4 h-4 rounded border-gray-300 dark:border-gray-600 text-blue-600 focus:ring-blue-500"
									/>
									<span class="text-sm text-gray-700 dark:text-gray-300">Headless</span>
								</label>
							</div>
							<TextField
								bind:value={cfgLang}
								on:keyup={() => dispatch('change', getModel())}
								optional={true}
								placeholder="en-US">Language</TextField
							>
							<div class="flex flex-col">
								<p class="font-semibold text-slate-600 dark:text-gray-400 py-2 transition-colors duration-200 text-sm">
									Flags <span class="font-normal text-xs">(one per line — <code class="font-mono">--flag</code> adds/overrides, <code class="font-mono">!--flag</code> removes)</span>
								</p>
								<textarea
									bind:value={cfgExtraFlags}
									on:keyup={() => dispatch('change', getModel())}
									rows="3"
									placeholder="--use-gl=egl"
									class="font-mono rounded-md py-2 pl-2 text-gray-600 dark:text-gray-300 border focus:outline-none focus:border-solid focus:bg-gray-100 dark:focus:bg-gray-700/60 bg-grayblue-light dark:bg-gray-900/60 transition-colors duration-200 border-transparent dark:border-gray-700/60 focus:border-slate-400 dark:focus:border-highlight-blue/80 resize-y w-full text-sm"
								></textarea>
							</div>
						{/if}

						<TextField
							type="number"
							bind:value={cfgTimeout}
							on:keyup={() => dispatch('change', getModel())}
							placeholder="5">Timeout (minutes)</TextField
						>

					</div>
				{:else}
					<div class="flex flex-col h-full gap-3">
						{#if isScriptDirty}
							<p class="text-xs text-orange-400">Unsaved changes - save before running.</p>
						{/if}
						<!-- Action buttons -->
						<div class="flex gap-2">
							{#if !isRunning}
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-green-600 hover:bg-green-700 text-white rounded transition-colors"
									on:click={startRun}
								>
									<svg
										xmlns="http://www.w3.org/2000/svg"
										viewBox="0 0 20 20"
										fill="currentColor"
										class="w-4 h-4"
									>
										<path
											d="M6.3 2.84A1.5 1.5 0 0 0 4 4.11v11.78a1.5 1.5 0 0 0 2.3 1.27l9.344-5.891a1.5 1.5 0 0 0 0-2.538L6.3 2.84Z"
										/>
									</svg>
									Run
								</button>
							{:else}
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-red-600 hover:bg-red-700 text-white rounded transition-colors"
									on:click={stopRun}
								>
									<svg
										xmlns="http://www.w3.org/2000/svg"
										viewBox="0 0 20 20"
										fill="currentColor"
										class="w-4 h-4"
									>
										<path
											d="M5.25 3A2.25 2.25 0 0 0 3 5.25v9.5A2.25 2.25 0 0 0 5.25 17h9.5A2.25 2.25 0 0 0 17 14.75v-9.5A2.25 2.25 0 0 0 14.75 3h-9.5Z"
										/>
									</svg>
									Stop
								</button>
							{/if}
							{#if runLog.length > 0}
								<button
									type="button"
									class="px-3 py-1.5 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 border border-gray-300 dark:border-gray-600 rounded transition-colors"
									on:click={() => {
										runLog = [];
									}}
								>
									Clear
								</button>
							{/if}
							{#if streamSessionID}
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded transition-colors"
									on:click={() => { streamControlMode = false; streamVisible = true; }}
								>
									<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4">
										<path d="M10 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z" />
										<path fill-rule="evenodd" d="M.664 10.59a1.651 1.651 0 0 1 0-1.186A10.004 10.004 0 0 1 10 3c4.257 0 7.893 2.66 9.336 6.41.147.381.146.804 0 1.186A10.004 10.004 0 0 1 10 17c-4.257 0-7.893-2.66-9.336-6.41ZM14 10a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z" clip-rule="evenodd" />
									</svg>
									View
								</button>
								<button
									type="button"
									class="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-purple-600 hover:bg-purple-700 text-white rounded transition-colors"
									on:click={() => { streamControlMode = true; streamVisible = true; }}
								>
									<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4">
										<path fill-rule="evenodd" d="M2 4.25A2.25 2.25 0 0 1 4.25 2h11.5A2.25 2.25 0 0 1 18 4.25v8.5A2.25 2.25 0 0 1 15.75 15h-3.105a3.501 3.501 0 0 0 1.1 1.677A.75.75 0 0 1 13.26 18H6.74a.75.75 0 0 1-.484-1.323A3.501 3.501 0 0 0 7.355 15H4.25A2.25 2.25 0 0 1 2 12.75v-8.5Zm1.5 0a.75.75 0 0 1 .75-.75h11.5a.75.75 0 0 1 .75.75v7.5a.75.75 0 0 1-.75.75H4.25a.75.75 0 0 1-.75-.75v-7.5Z" clip-rule="evenodd" />
									</svg>
									Control
								</button>
							{/if}
						</div>

						<!-- Event log -->
						<div
							bind:this={logContainer}
							on:wheel={onLogWheel}
							on:scroll={onLogScroll}
							class="flex-1 overflow-y-auto font-mono text-xs bg-gray-900 dark:bg-gray-950 text-gray-200 rounded p-2 space-y-0.5 min-h-0 select-text"
							style="max-height: calc(100vh - 18rem);"
						>
							{#if runLog.length === 0}
								<span class="text-gray-500">No events yet. Click Run to execute the script.</span>
							{:else}
								{#each runLog as entry}
									<div
										class="leading-5 {entry.type === 'event'
											? 'text-blue-300'
											: entry.type === 'sent'
												? 'text-orange-300'
												: entry.type === 'capture'
													? 'text-purple-300'
													: entry.type === 'submit'
														? 'text-amber-300'
														: entry.type === 'info'
															? 'text-sky-300'
															: entry.type === 'screenshot'
																? 'text-teal-300'
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
										{:else if entry.type === 'sent'}
											<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
											<span class="text-orange-400"> → {entry.event}</span>
											{#if entry.data !== null && entry.data !== undefined && entry.data !== ''}
												<span class="text-gray-300"> data=</span><span
													>{JSON.stringify(entry.data)}</span
												>
											{/if}
										{:else if entry.type === 'screenshot'}
											<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
											<span class="text-teal-400"> 📷 {entry.key || 'screenshot'}</span>
											{#if entry.url}
												<span class="text-gray-500 text-xs font-mono ml-1 truncate max-w-xs inline-block align-middle" title={entry.url}>{entry.url}</span>
											{/if}
											<div class="mt-1">
												<!-- svelte-ignore a11y-click-events-have-key-events -->
												<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
												<img
													src={entry.value}
													alt={entry.key || 'screenshot'}
													class="max-h-32 rounded border border-teal-700/40 cursor-pointer hover:opacity-90 transition-opacity"
													on:click={() => {
														screenshotModalSrc = entry.value;
														screenshotModalLabel = entry.key || 'screenshot';
														screenshotModalURL = entry.url || '';
													}}
												/>
											</div>
										{:else if entry.type === 'info'}
											<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
											<span class="text-sky-400"> ℹ info</span>
											<span class="text-sky-200 ml-1">{entry.message}</span>
										{:else if entry.type === 'submit'}
											<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
											<span class="text-amber-400"> ⬆ submitData</span>
											<pre class="mt-1 text-xs text-amber-200 bg-gray-800 rounded p-1.5 overflow-x-auto max-h-40 overflow-y-auto select-text">{JSON.stringify(entry.value, null, 2)}</pre>
										{:else if entry.type === 'capture'}
											<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
											<span class="text-purple-400"> ★ capture</span>
											{#if entry.value?.cookies}
												<span class="text-gray-400"> · {entry.value.cookies.length} cookies</span>
											{/if}
											{#if entry.value?.localStorage}
												<span class="text-gray-400"> · {Object.keys(entry.value.localStorage).length} localStorage</span>
											{/if}
											{#if entry.value?.sessionStorage}
												<span class="text-gray-400"> · {Object.keys(entry.value.sessionStorage).length} sessionStorage</span>
											{/if}
											<pre class="mt-1 text-xs text-purple-200 bg-gray-800 rounded p-1.5 overflow-x-auto max-h-40 overflow-y-auto select-text">{JSON.stringify(entry.value, null, 2)}</pre>
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

						<!-- Event injection panel (visible while running) -->
						{#if isRunning}
							<div class="border border-orange-500/30 rounded p-2 space-y-1.5 bg-gray-800/50">
								<p class="text-xs text-orange-400/70 font-medium">Inject event</p>
								<div class="flex gap-2">
									<input
										type="text"
										bind:value={injectEvent}
										placeholder="event name"
										class="w-32 px-2 py-1 text-xs rounded border border-gray-600 bg-gray-800 text-gray-200 font-mono focus:outline-none focus:ring-1 focus:ring-orange-500"
										on:keydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); sendEvent(); } }}
									/>
									<input
										type="text"
										bind:value={injectData}
										placeholder="data JSON, e.g. {`{"username":"foo"}`}"
										class="flex-1 px-2 py-1 text-xs rounded border border-gray-600 bg-gray-800 text-gray-200 font-mono focus:outline-none focus:ring-1 focus:ring-orange-500"
										on:keydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); sendEvent(); } }}
									/>
									<button
										type="button"
										class="px-3 py-1 text-xs bg-orange-600 hover:bg-orange-700 text-white rounded transition-colors whitespace-nowrap"
										on:click={sendEvent}
									>
										Send
									</button>
								</div>
							</div>
						{/if}

						{#if !id}
							<p class="text-xs text-yellow-600 dark:text-yellow-400">
								Save the remote browser first to enable live test runs.
							</p>
						{/if}
					</div>
				{/if}
			</div>
		</div>
	</div>
</div>

<!-- Screenshot fullscreen modal -->
{#if screenshotModalSrc}
	<!-- svelte-ignore a11y-click-events-have-key-events -->
	<!-- svelte-ignore a11y-no-static-element-interactions -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
		on:click={() => { screenshotModalSrc = null; screenshotModalURL = ''; }}
	>
		<div
			class="relative max-w-[90vw] max-h-[90vh] flex flex-col items-center"
			on:click|stopPropagation
		>
			<div class="flex items-center justify-between w-full mb-2 px-1">
				<div class="flex flex-col min-w-0">
					<span class="text-teal-300 text-sm font-mono">{screenshotModalLabel}</span>
					{#if screenshotModalURL}
						<span class="text-gray-400 text-xs font-mono truncate" title={screenshotModalURL}>{screenshotModalURL}</span>
					{/if}
				</div>
				<button
					type="button"
					class="text-gray-400 hover:text-white transition-colors ml-4"
					on:click={() => { screenshotModalSrc = null; screenshotModalURL = ''; }}
				>
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-5 h-5">
						<path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
					</svg>
				</button>
			</div>
			<img
				src={screenshotModalSrc}
				alt={screenshotModalLabel}
				class="max-w-full max-h-[80vh] rounded shadow-2xl border border-gray-700"
			/>
		</div>
	</div>
{/if}

<RemoteBrowserStream
	bind:visible={streamVisible}
	crID={streamSessionID}
	controlMode={streamControlMode}
	{runLog}
	{isRunning}
	on:inject={(e) => {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		const { event, data } = e.detail;
		ws.send(JSON.stringify({ event, data }));
		runLog = [...runLog, { type: 'sent', event, data, time: now() }];
	}}
/>
