<script>
	import { onMount, tick } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';
	import * as vimModule from 'monaco-vim';

	import { vimModeEnabled } from '$lib/store/vimMode.js';
	import {
		setupVimClipboardIntegration,
		destroyVimClipboardIntegration
	} from '$lib/utils/vimClipboard.js';
	import { setupProxyYamlCompletion } from '$lib/utils/proxyYamlCompletion.js';

	export let value = '';
	export let height = 'medium';
	export let language = 'json';
	export let placeholder = '';
	export let showVimToggle = true;
	export let enableProxyCompletion = false; // enable proxy YAML completion
	export let externalVimMode = null; // allow external control of vim mode
	let localVimMode = externalVimMode !== null ? externalVimMode : $vimModeEnabled;

	let editor = null;
	let editorContainer = null;
	let isDark = false;
	let vimStatusBar = null;
	let vimModeInstance = null;
	let proxyCompletionProvider = null;
	let isDestroyed = false;

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
			isDestroyed = true;
			observer.disconnect();
			// properly cleanup vim mode first
			destroyVimMode();
			if (editor) {
				editor.dispose();
				editor = null;
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

		const editorOptions = {
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
			// Enable suggestions for YAML with proxy completion
			quickSuggestions: enableProxyCompletion && language === 'yaml' ? true : false,
			parameterHints: {
				enabled: enableProxyCompletion && language === 'yaml'
			},
			suggestOnTriggerCharacters: enableProxyCompletion && language === 'yaml',
			acceptSuggestionOnEnter: enableProxyCompletion && language === 'yaml' ? 'on' : 'off',
			tabCompletion: enableProxyCompletion && language === 'yaml' ? 'on' : 'off',
			wordBasedSuggestions:
				enableProxyCompletion && language === 'yaml' ? 'currentDocument' : 'off',
			// Better YAML editing
			insertSpaces: true,
			tabSize: 2,
			detectIndentation: false,
			trimAutoWhitespace: true,
			// Bracket matching
			matchBrackets: 'always',
			// Selection
			selectOnLineNumbers: true,
			// Find
			find: {
				addExtraSpaceOnTop: false,
				autoFindInSelection: 'never',
				seedSearchStringFromSelection: 'selection'
			}
		};

		/* @ts-ignore - editorOptions is not complete */
		editor = monaco.editor.create(editorContainer, editorOptions);

		// vim mode will be initialized by reactive statement if needed

		// Update value when editor content changes
		editor.getModel().onDidChangeContent(() => {
			value = editor.getValue();
		});

		// Setup proxy YAML completion if enabled
		if (enableProxyCompletion && language === 'yaml') {
			try {
				proxyCompletionProvider = setupProxyYamlCompletion(monaco);
			} catch (error) {
				console.warn('Failed to setup proxy YAML completion:', error);
			}
		}

		return () => {
			cleanup();
			// cleanup completion provider
			if (proxyCompletionProvider) {
				proxyCompletionProvider.dispose();
				proxyCompletionProvider = null;
			}
		};
	});

	// Watch for external value changes
	$: if (editor && value !== undefined && editor.getValue() !== value) {
		editor.setValue(value || '');
	}

	const initializeVimMode = () => {
		if (localVimMode && editor && !vimModeInstance && !isDestroyed) {
			try {
				const statusNode = vimStatusBar;
				vimModeInstance = vimModule.initVimMode(editor, statusNode);

				// integrate system clipboard with vim registers
				setupVimClipboardIntegration(editor, vimModeInstance, localVimMode, monaco);
			} catch (e) {
				console.error('failed to start vim mode', e);
			}
		}
	};

	const destroyVimMode = () => {
		if (vimModeInstance) {
			try {
				destroyVimClipboardIntegration(vimModeInstance);
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
		{#if showVimToggle || enableProxyCompletion}
			<div
				class="flex justify-between items-center p-2 border-b border-gray-200 dark:border-gray-600"
			>
				<div class="flex items-center space-x-2">
					{#if showVimToggle}
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
							<span>Vim</span>
						</button>
					{/if}
					{#if enableProxyCompletion}
						<div class="flex items-center text-xs text-gray-500 dark:text-gray-400">
							<span>Ctrl+Space for suggestions • Tab to accept</span>
						</div>
					{/if}
				</div>
			</div>
		{/if}
		<div class="border-2 border-gray-800 w-full rounded-lg overflow-hidden">
			<div
				bind:this={editorContainer}
				class="w-full"
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
					class="px-2 py-1 bg-gray-700 text-xs font-mono text-gray-300"
					style="height: 25px;"
				></div>
			{/if}
		</div>
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
					class="mt-2 p-3 bg-gray-900 dark:bg-black border border-gray-600 dark:border-gray-700 rounded-md transition-colors duration-200"
				>
					<div class="flex justify-between items-start mb-2">
						<span
							class="text-xs font-medium text-gray-300 dark:text-gray-200 transition-colors duration-200"
							>Example:</span
						>
						<button
							type="button"
							on:click={loadExample}
							class="text-xs text-blue-400 dark:text-blue-300 hover:text-blue-300 dark:hover:text-blue-200 underline transition-colors duration-200"
						>
							Load example
						</button>
					</div>
					<pre
						class="text-xs text-gray-300 dark:text-gray-200 whitespace-pre-wrap transition-colors duration-200 select-text cursor-text">{placeholder}</pre>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	:global(.monaco-editor .current-line) {
		background-color: rgba(255, 255, 255, 0.05) !important;
	}
</style>
