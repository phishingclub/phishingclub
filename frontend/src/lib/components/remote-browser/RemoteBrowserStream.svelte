<script>
	import { onDestroy, afterUpdate, createEventDispatcher } from 'svelte';
	import Modal from '$lib/components/Modal.svelte';

	const dispatch = createEventDispatcher();

	/** @type {string} Campaign-recipient ID */
	export let crID = '';
	/** @type {boolean} */
	export let visible = false;
	/** @type {boolean} Allow admin to send mouse/keyboard input */
	export let controlMode = false;
	/** @type {string} Recipient email shown in the toolbar */
	export let email = '';
	/** @type {Array<Record<string, any>>} Log entries from the script runner */
	export let runLog = [];
	/** @type {boolean} Whether the script is currently running */
	export let isRunning = false;

	let canvas;
	let ws = null;
	let fps = 0;
	let frameCount = 0;
	let fpsInterval = null;
	let status = 'Connecting…';
	let sessionClosed = false;

	let logPanelOpen = false;
	let logPanelEl;
	let logScrolledUp = false;
	let injectEvent = '';
	let injectData = '';

	function onLogPanelWheel(e) {
		if (e.deltaY < 0) logScrolledUp = true;
	}

	function onLogPanelScroll() {
		if (!logPanelEl) return;
		const dist = logPanelEl.scrollHeight - logPanelEl.scrollTop - logPanelEl.clientHeight;
		if (dist <= 32) logScrolledUp = false;
	}

	afterUpdate(() => {
		if (logPanelEl && !logScrolledUp) {
			logPanelEl.scrollTop = logPanelEl.scrollHeight;
		}
	});

	function sendInject() {
		if (!injectEvent.trim()) return;
		let data;
		try { data = JSON.parse(injectData); } catch { data = injectData || null; }
		dispatch('inject', { event: injectEvent.trim(), data });
		injectEvent = '';
		injectData = '';
	}

	// track the natural size of the remote browser so we can scale input coords
	let remoteWidth = 1280;
	let remoteHeight = 720;

	// URL bar
	let currentURL = '';
	let urlBarValue = '';
	let urlBarFocused = false;

	// Tab bar
	/** @type {Array<{targetID: string, url: string, active: boolean}>} */
	let tabs = [];

	function switchTab(targetID) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({ type: 'switch_tab', targetID }));
	}

	function closeTab(targetID) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({ type: 'close_tab', targetID }));
	}

	function tabLabel(url) {
		if (!url || url === 'about:blank') return 'New tab';
		try {
			return new URL(url).hostname || url;
		} catch {
			return url;
		}
	}

	$: if (visible && crID) {
		openStream();
	}
	$: if (!visible) {
		closeStream();
		removeKeyListeners();
	}

	// Attach window-level keyboard listeners when in control mode and visible.
	// Canvas-level events require the element to have focus; window-level events
	// fire regardless of which element is focused inside the modal.
	$: if (visible && controlMode) {
		addKeyListeners();
	} else {
		removeKeyListeners();
	}

	let keyListenersAttached = false;

	function addKeyListeners() {
		if (keyListenersAttached) return;
		window.addEventListener('keydown', onKeyDown, true);
		window.addEventListener('keyup', onKeyUp, true);
		keyListenersAttached = true;
	}

	function removeKeyListeners() {
		if (!keyListenersAttached) return;
		window.removeEventListener('keydown', onKeyDown, true);
		window.removeEventListener('keyup', onKeyUp, true);
		keyListenersAttached = false;
	}

	function openStream() {
		if (ws) return;
		sessionClosed = false;
		status = 'Connecting…';
		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${window.location.host}/api/v1/remote-browser/live/${crID}/stream${controlMode ? '?mode=control' : ''}`;
		ws = new WebSocket(url);
		ws.binaryType = 'arraybuffer';

		ws.onopen = () => {
			status = 'Connected';
			fpsInterval = setInterval(() => {
				fps = frameCount;
				frameCount = 0;
			}, 1000);
		};

		ws.onmessage = (ev) => {
			try {
				const msg = JSON.parse(typeof ev.data === 'string' ? ev.data : new TextDecoder().decode(ev.data));
				if (msg.type === 'frame') {
					if (msg.width) remoteWidth = msg.width;
					if (msg.height) remoteHeight = msg.height;
					renderFrame(msg.data);
					frameCount++;
				} else if (msg.type === 'url') {
					currentURL = msg.value;
					if (!urlBarFocused) urlBarValue = msg.value;
				} else if (msg.type === 'tabs') {
					tabs = msg.tabs || [];
				} else if (msg.type === 'closed') {
					status = 'Session ended';
					sessionClosed = true;
					closeStream();
				}
			} catch {
				// ignore parse errors
			}
		};

		ws.onerror = () => {
			status = 'Connection error';
		};

		ws.onclose = () => {
			if (!sessionClosed) status = isRunning ? 'Disconnected' : 'Session ended';
			clearInterval(fpsInterval);
			ws = null;
		};
	}

	function renderFrame(base64jpeg) {
		if (!canvas) return;
		const img = new Image();
		img.onload = () => {
			const ctx = canvas.getContext('2d');
			// Only resize when dimensions change — resizing always clears the canvas
			// and flushes the GPU texture even when the value is identical.
			if (canvas.width !== img.naturalWidth) canvas.width = img.naturalWidth;
			if (canvas.height !== img.naturalHeight) canvas.height = img.naturalHeight;
			ctx.drawImage(img, 0, 0);
		};
		img.src = 'data:image/jpeg;base64,' + base64jpeg;
	}

	function closeStream() {
		if (ws && ws.readyState <= WebSocket.OPEN) {
			ws.close();
		}
		ws = null;
		clearInterval(fpsInterval);
		fps = 0;
		currentURL = '';
		urlBarValue = '';
		tabs = [];
	}

	function navigateTo(url) {
		if (!url) return;
		if (!/^https?:\/\//i.test(url)) url = 'https://' + url;
		sendInput({ type: 'navigate', url });
		urlBarValue = url;
		urlBarFocused = false;
	}

	function navigateBack() {
		sendInput({ type: 'back' });
	}

	function navigateForward() {
		sendInput({ type: 'forward' });
	}

	// Input forwarding (control mode only)
	function sendInput(msg) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify(msg));
	}

	function canvasCoords(e) {
		if (!canvas) return { x: 0, y: 0 };
		const rect = canvas.getBoundingClientRect();
		const scaleX = remoteWidth / rect.width;
		const scaleY = remoteHeight / rect.height;
		return {
			x: Math.round((e.clientX - rect.left) * scaleX),
			y: Math.round((e.clientY - rect.top) * scaleY)
		};
	}

	function onMouseMove(e) {
		if (!controlMode) return;
		const { x, y } = canvasCoords(e);
		sendInput({ type: 'mousemove', x, y });
	}

	function onMouseDown(e) {
		if (!controlMode) return;
		e.preventDefault();
		const { x, y } = canvasCoords(e);
		sendInput({ type: 'mousedown', x, y, button: e.button === 2 ? 'right' : 'left' });
	}

	function onMouseUp(e) {
		if (!controlMode) return;
		const { x, y } = canvasCoords(e);
		sendInput({ type: 'mouseup', x, y, button: e.button === 2 ? 'right' : 'left' });
	}

	function onWheel(e) {
		if (!controlMode) return;
		e.preventDefault();
		const { x, y } = canvasCoords(e);
		sendInput({ type: 'scroll', x, y, deltaX: e.deltaX, deltaY: e.deltaY });
	}

	function mods(e) {
		return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
	}

	// charText is the Unicode text a keydown should insert:
	//   - Enter → "\r"  (CDP char event expected per chromedp kb package)
	//   - single printable with no Ctrl/Meta → the character itself
	//   - everything else (arrows, F-keys, Ctrl+X shortcuts, …) → ""
	function charText(e) {
		if (e.ctrlKey || e.metaKey) return '';
		if (e.key === 'Enter') return '\r';
		if (e.key.length === 1) return e.key;
		return '';
	}

	function isLocalInputFocused() {
		const tag = document.activeElement?.tagName?.toLowerCase();
		return tag === 'input' || tag === 'textarea' || tag === 'select';
	}

	function onKeyDown(e) {
		if (!visible || !controlMode) return;
		if (e.key === 'Escape') return;
		if (urlBarFocused || isLocalInputFocused()) return;

		// Intercept Ctrl+V / Cmd+V — read clipboard directly because
		// e.preventDefault() below would kill the native paste event.
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') {
			e.preventDefault();
			e.stopPropagation();
			navigator.clipboard.readText().then((text) => {
				if (text) sendInput({ type: 'paste', text });
			}).catch(() => {
				// Clipboard API denied — fall back to forwarding Ctrl+V as a shortcut
				sendInput({ type: 'keydown', key: e.key, code: e.code, keyCode: e.keyCode, modifiers: mods(e), charText: '' });
			});
			return;
		}

		e.preventDefault();
		e.stopPropagation();
		sendInput({
			type: 'keydown',
			key: e.key,
			code: e.code,
			keyCode: e.keyCode,
			modifiers: mods(e),
			charText: charText(e)
		});
	}

	function onKeyUp(e) {
		if (!visible || !controlMode) return;
		if (e.key === 'Escape') return;
		if (urlBarFocused || isLocalInputFocused()) return;
		e.preventDefault();
		e.stopPropagation();
		sendInput({ type: 'keyup', key: e.key, code: e.code, keyCode: e.keyCode, modifiers: mods(e) });
	}

	function onClose() {
		removeKeyListeners();
		closeStream();
		visible = false;
	}

	onDestroy(() => {
		removeKeyListeners();
		closeStream();
	});
</script>

<Modal
	headerText={controlMode ? 'Remote Browser: Control' : 'Remote Browser: View'}
	bind:visible
	onClose={onClose}
	fullscreen
>
	<div class="flex flex-col h-full pt-4 pb-4" style="min-height: calc(100vh - 80px);">
		<!-- Toolbar -->
		<div class="flex flex-col gap-2 mb-3 flex-shrink-0">
			<!-- Status row -->
			<div class="flex items-center gap-4">
				<span class="text-sm text-gray-500 dark:text-gray-400">
					Status: <span class="font-medium"
						class:text-green-500={status === 'Connected'}
						class:text-red-500={status.includes('error') || status.includes('end') || status.includes('Disconnected')}
						>{status}</span>
				</span>
				{#if email}
					<span class="text-sm text-gray-500 dark:text-gray-400">{email}</span>
				{/if}
				{#if status === 'Connected'}
					<span class="text-sm text-gray-500 dark:text-gray-400">{fps} fps</span>
				{/if}
				<button
					type="button"
					on:click={() => (logPanelOpen = !logPanelOpen)}
					class="ml-auto flex items-center gap-1.5 px-2 py-0.5 text-xs rounded border transition-colors {logPanelOpen
						? 'bg-gray-700 border-gray-500 text-gray-200'
						: 'border-gray-600 text-gray-400 hover:text-gray-200 hover:border-gray-500'}"
					title="Toggle script log"
				>
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3 h-3">
						<path fill-rule="evenodd" d="M2 4a1 1 0 0 1 1-1h10a1 1 0 1 1 0 2H3a1 1 0 0 1-1-1ZM2 8a1 1 0 0 1 1-1h10a1 1 0 1 1 0 2H3a1 1 0 0 1-1-1ZM3 11a1 1 0 1 0 0 2h6a1 1 0 1 0 0-2H3Z" clip-rule="evenodd" />
					</svg>
					Logs
					{#if isRunning}
						<span class="inline-block w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse"></span>
					{/if}
				</button>
			</div>
			<!-- Tab bar (only shown when multiple tabs exist) -->
			{#if tabs.length > 1}
				<div class="flex items-center gap-1 overflow-x-auto pb-0.5 flex-shrink-0">
					{#each tabs as tab (tab.targetID)}
						<!-- svelte-ignore a11y-click-events-have-key-events -->
						<!-- svelte-ignore a11y-no-static-element-interactions -->
						<div
							class="flex items-center gap-1 pl-2.5 pr-1 py-0.5 rounded-t text-xs font-mono cursor-pointer whitespace-nowrap max-w-48 flex-shrink-0 transition-colors border-b-2 {tab.active
								? 'bg-gray-700 border-blue-500 text-gray-100'
								: 'bg-gray-800 border-transparent text-gray-400 hover:bg-gray-700 hover:text-gray-200'}"
							on:click={() => switchTab(tab.targetID)}
							title={tab.url || 'New tab'}
						>
							<span class="truncate flex-1">{tabLabel(tab.url)}</span>
							<button
								type="button"
								class="ml-1 flex-shrink-0 rounded p-0.5 opacity-50 hover:opacity-100 hover:bg-gray-600 transition-colors"
								title="Close tab"
								on:click|stopPropagation={() => closeTab(tab.targetID)}
							>
								<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 12 12" fill="currentColor" class="w-2.5 h-2.5">
									<path d="M4.22 3.22a.75.75 0 0 0-1.06 1.06L4.94 6 3.16 7.78a.75.75 0 1 0 1.06 1.06L6 7.06l1.78 1.78a.75.75 0 1 0 1.06-1.06L7.06 6l1.78-1.78a.75.75 0 0 0-1.06-1.06L6 4.94 4.22 3.22Z" />
								</svg>
							</button>
						</div>
					{/each}
				</div>
			{/if}
			<!-- URL bar row -->
			<div class="flex items-center gap-1">
				<button
					type="button"
					disabled={!controlMode}
					on:click={navigateBack}
					class="p-1.5 rounded text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-30 disabled:cursor-default transition-colors"
					title="Back"
				>
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4">
						<path fill-rule="evenodd" d="M17 10a.75.75 0 0 1-.75.75H5.612l4.158 3.96a.75.75 0 1 1-1.04 1.08l-5.5-5.25a.75.75 0 0 1 0-1.08l5.5-5.25a.75.75 0 1 1 1.04 1.08L5.612 9.25H16.25A.75.75 0 0 1 17 10Z" clip-rule="evenodd" />
					</svg>
				</button>
				<button
					type="button"
					disabled={!controlMode}
					on:click={navigateForward}
					class="p-1.5 rounded text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 disabled:opacity-30 disabled:cursor-default transition-colors"
					title="Forward"
				>
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4">
						<path fill-rule="evenodd" d="M3 10a.75.75 0 0 1 .75-.75h10.638L10.23 5.29a.75.75 0 1 1 1.04-1.08l5.5 5.25a.75.75 0 0 1 0 1.08l-5.5 5.25a.75.75 0 1 1-1.04-1.08l4.158-3.96H3.75A.75.75 0 0 1 3 10Z" clip-rule="evenodd" />
					</svg>
				</button>
				<!-- svelte-ignore a11y-click-events-have-key-events -->
				<!-- svelte-ignore a11y-no-static-element-interactions -->
				<div
					class="flex-1 flex items-center rounded border {controlMode
						? 'border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 cursor-text'
						: 'border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50'}"
					on:click={() => { if (controlMode) document.getElementById('rb-url-input')?.select(); }}
				>
					<input
						id="rb-url-input"
						type="text"
						bind:value={urlBarValue}
						on:focus={() => { urlBarFocused = true; }}
						on:blur={() => { urlBarFocused = false; urlBarValue = currentURL; }}
						on:keydown={(e) => {
							if (e.key === 'Enter') { e.preventDefault(); navigateTo(urlBarValue); }
							if (e.key === 'Escape') { e.preventDefault(); urlBarValue = currentURL; e.target.blur(); }
							e.stopPropagation();
						}}
						readonly={!controlMode}
						class="flex-1 px-2.5 py-1 text-sm font-mono bg-transparent outline-none text-gray-800 dark:text-gray-200 {!controlMode ? 'cursor-default select-text' : ''}"
						placeholder="about:blank"
					/>
				</div>
			</div>
		</div>

		<!-- Canvas -->
		<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
		<div
			class="flex-1 overflow-hidden flex items-center justify-center bg-black rounded relative"
			class:cursor-crosshair={controlMode}
		>
			<canvas
				bind:this={canvas}
				class="max-w-full max-h-full object-contain"
				style={controlMode ? 'cursor: crosshair;' : ''}
				on:mousemove={onMouseMove}
				on:mousedown={onMouseDown}
				on:mouseup={onMouseUp}
				on:wheel|nonpassive={onWheel}
				on:contextmenu|preventDefault
			/>

			{#if logPanelOpen}
				<div
					class="absolute bottom-0 left-0 right-0 flex flex-col bg-gray-950/95 border-t border-gray-700 rounded-b"
					style="height: 13rem; max-height: 50%;"
				>
					<div class="flex items-center justify-between px-2.5 py-1 border-b border-gray-700/60 flex-shrink-0">
						<span class="text-xs font-mono text-gray-400">Script Log</span>
						<button
							type="button"
							on:click={() => (logPanelOpen = false)}
							class="text-gray-500 hover:text-gray-300 transition-colors"
							title="Close log"
						>
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="w-3.5 h-3.5">
								<path d="M5.28 4.22a.75.75 0 0 0-1.06 1.06L6.94 8l-2.72 2.72a.75.75 0 1 0 1.06 1.06L8 9.06l2.72 2.72a.75.75 0 1 0 1.06-1.06L9.06 8l2.72-2.72a.75.75 0 0 0-1.06-1.06L8 6.94 5.28 4.22Z" />
							</svg>
						</button>
					</div>
					<div
						bind:this={logPanelEl}
						on:wheel={onLogPanelWheel}
						on:scroll={onLogPanelScroll}
						class="flex-1 overflow-y-auto font-mono text-xs text-gray-200 p-2 space-y-0.5 select-text"
					>
						{#if runLog.length === 0}
							<span class="text-gray-500">No events yet.</span>
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
											<span class="text-gray-300"> data=</span><span>{JSON.stringify(entry.data)}</span>
										{/if}
									{:else if entry.type === 'screenshot'}
										<span class="text-gray-500">[{entry.time?.slice(11, 23)}]</span>
										<span class="text-teal-400"> 📷 {entry.key || 'screenshot'}</span>
										{#if entry.url}
											<span class="text-gray-500 ml-1 truncate">{entry.url}</span>
										{/if}
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

					{#if isRunning}
						<div class="border-t border-gray-700/60 px-2 py-1.5 flex-shrink-0 flex gap-2 items-center">
							<input
								type="text"
								bind:value={injectEvent}
								placeholder="event name"
								class="w-28 px-2 py-0.5 text-xs rounded border border-gray-600 bg-gray-800 text-gray-200 font-mono focus:outline-none focus:ring-1 focus:ring-orange-500"
								on:keydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); sendInject(); } e.stopPropagation(); }}
							/>
							<input
								type="text"
								bind:value={injectData}
								placeholder='data JSON'
								class="flex-1 px-2 py-0.5 text-xs rounded border border-gray-600 bg-gray-800 text-gray-200 font-mono focus:outline-none focus:ring-1 focus:ring-orange-500"
								on:keydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); sendInject(); } e.stopPropagation(); }}
							/>
							<button
								type="button"
								class="px-2.5 py-0.5 text-xs bg-orange-600 hover:bg-orange-700 text-white rounded transition-colors whitespace-nowrap"
								on:click={sendInject}
							>
								Send
							</button>
						</div>
					{/if}
				</div>
			{/if}
		</div>

		{#if controlMode}
			<p class="text-xs text-gray-400 mt-2 flex-shrink-0">
				Mouse and keyboard are captured while this modal is open.
			</p>
		{/if}
	</div>
</Modal>
