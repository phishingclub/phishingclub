<script>
	import { onMount } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';
	import { vimModeEnabled } from '$lib/store/vimMode.js';

	export let value = '';
	export let height = 'medium';
	export let language = 'json';
	export let placeholder = '';
	export let showVimToggle = true;
	export let externalVimMode = null; // allow external control of vim mode
	let localVimMode = externalVimMode !== null ? externalVimMode : $vimModeEnabled;

	let editor = null;
	let editorContainer = null;
	let isDark = false;
	let vimStatusBar = null;
	let vimModeInstance = null;

	const heightClasses = {
		small: 'h-64',
		medium: 'h-80',
		large: 'h-96'
	};

	// Check for dark mode
	const checkDarkMode = () => {
		if (typeof window !== 'undefined') {
			isDark = document.documentElement.classList.contains('dark');
		}
	};

	onMount(() => {
		checkDarkMode();

		// Watch for dark mode changes
		const observer = new MutationObserver(() => {
			const newIsDark = document.documentElement.classList.contains('dark');
			if (newIsDark !== isDark) {
				isDark = newIsDark;
				if (editor) {
					monaco.editor.setTheme(isDark ? 'vs-dark' : 'vs-light');
				}
			}
		});

		observer.observe(document.documentElement, {
			attributes: true,
			attributeFilter: ['class']
		});

		const cleanup = () => {
			observer.disconnect();
			if (editor) {
				editor.dispose();
			}
		};
		/* @ts-ignore */
		self.MonacoEnvironment = {
			getWorker: function (_, label) {
				if (label === 'json') {
					return new jsonWorker();
				}
				return new editorWorker();
			}
		};

		editor = monaco.editor.create(editorContainer, {
			value: value || '',
			language: language,
			theme: 'vs-dark',
			automaticLayout: true,
			minimap: {
				enabled: false
			},
			scrollBeyondLastLine: false,
			fontSize: 13,
			lineNumbers: 'on',
			folding: true,
			wordWrap: 'on',
			contextmenu: true,
			scrollbar: {
				horizontal: 'hidden'
			},
			quickSuggestions: false,
			parameterHints: {
				enabled: false
			},
			suggestOnTriggerCharacters: false,
			acceptSuggestionOnEnter: 'off',
			tabCompletion: 'off',
			wordBasedSuggestions: 'off'
		});

		// vim mode will be initialized by reactive statement if needed

		// Update value when editor content changes
		editor.getModel().onDidChangeContent(() => {
			value = editor.getValue();
		});

		return () => {
			cleanup();
			// properly cleanup vim mode first
			destroyVimMode();
		};
	});

	// Watch for external value changes
	$: if (editor && value !== undefined && editor.getValue() !== value) {
		editor.setValue(value || '');
	}

	const initializeVimMode = () => {
		if (localVimMode && editor && !vimModeInstance) {
			import('monaco-vim')
				.then((vimModule) => {
					const statusNode = vimStatusBar;
					vimModeInstance = vimModule.initVimMode(editor, statusNode);
				})
				.catch(() => {
					console.warn('vim mode not available - monaco-vim package not installed');
				});
		}
	};

	const destroyVimMode = () => {
		if (vimModeInstance) {
			try {
				// use official monaco-vim dispose method
				vimModeInstance.dispose();
			} catch (e) {
				console.warn('Error disposing vim mode:', e);
			}

			// clear vim status bar
			if (vimStatusBar) {
				vimStatusBar.textContent = '';
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

	// debounce vim mode changes to prevent race conditions
	let vimModeTimeout = null;

	// Watch for vim mode changes
	$: if (editor && typeof localVimMode === 'boolean') {
		if (vimModeTimeout) {
			clearTimeout(vimModeTimeout);
		}
		vimModeTimeout = setTimeout(() => {
			if (localVimMode && !vimModeInstance) {
				initializeVimMode();
			} else if (!localVimMode && vimModeInstance) {
				destroyVimMode();
			}
			vimModeTimeout = null;
		}, 100);
	}

	let showExample = false;

	const loadExample = () => {
		if (editor && placeholder) {
			editor.setValue(placeholder);
			value = placeholder;
		}
		showExample = false;
	};
</script>

<div class="w-full">
	<div class="bg-white dark:bg-gray-800 transition-colors duration-200 rounded-md">
		{#if showVimToggle}
			<div
				class="flex justify-between items-center p-2 border-b border-gray-200 dark:border-gray-600"
			>
				<div class="flex items-center space-x-2">
					<button
						type="button"
						on:click={() => {
							vimModeEnabled.update((v) => !v);
						}}
						class="h-8 border-2 border-gray-300 dark:border-gray-600 rounded-md px-3 text-center cursor-pointer hover:opacity-80 flex items-center justify-center gap-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
						class:font-bold={localVimMode}
						class:bg-cta-blue={localVimMode}
						class:dark:bg-indigo-600={localVimMode}
						class:text-white={localVimMode}
					>
						<span>Vim</span>
					</button>
				</div>
			</div>
		{/if}
		<div
			bind:this={editorContainer}
			class="border-2 border-black dark:border-gray-600 bg-white dark:bg-gray-900 w-full transition-colors duration-200"
			class:rounded-b-md={showVimToggle}
			class:rounded-md={!showVimToggle}
			class:h-64={height === 'small' && !localVimMode}
			class:h-80={height === 'medium' && !localVimMode}
			class:h-96={height === 'large' && !localVimMode}
			style={localVimMode
				? `height: ${height === 'small' ? '224px' : height === 'medium' ? '294px' : '359px'}`
				: ''}
		></div>
		{#if localVimMode}
			<div
				bind:this={vimStatusBar}
				class="px-2 py-1 bg-gray-100 dark:bg-gray-700 border-t border-gray-200 dark:border-gray-600 text-xs font-mono text-gray-700 dark:text-gray-300 rounded-b-md"
				style="height: 25px;"
			></div>
		{/if}
	</div>
	{#if placeholder}
		<div class="mt-2">
			<button
				type="button"
				on:click={() => (showExample = !showExample)}
				class="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline focus:outline-none transition-colors duration-200"
			>
				{showExample ? 'Hide' : 'Show'} example
			</button>
			{#if showExample}
				<div
					class="mt-2 p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-md transition-colors duration-200"
				>
					<div class="flex justify-between items-start mb-2">
						<span
							class="text-xs font-medium text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>Example:</span
						>
						<button
							type="button"
							on:click={loadExample}
							class="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline transition-colors duration-200"
						>
							Load example
						</button>
					</div>
					<pre
						class="text-xs text-gray-600 dark:text-gray-300 whitespace-pre-wrap transition-colors duration-200 select-text cursor-text">{placeholder}</pre>
				</div>
			{/if}
		</div>
	{/if}
</div>
