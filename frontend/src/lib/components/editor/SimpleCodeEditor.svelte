<script>
	import { onMount } from 'svelte';
	import * as monaco from 'monaco-editor';
	import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';
	import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker';

	export let value = '';
	export let height = 'medium';
	export let language = 'json';
	export let placeholder = '';

	let editor = null;
	let editorContainer = null;
	let isDark = false;

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
			theme: isDark ? 'vs-dark' : 'vs-light',
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

		// Update value when editor content changes
		editor.getModel().onDidChangeContent(() => {
			value = editor.getValue();
		});

		return cleanup;
	});

	// Watch for external value changes
	$: if (editor && value !== undefined && editor.getValue() !== value) {
		editor.setValue(value || '');
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
	<div
		bind:this={editorContainer}
		class="border border-gray-300 dark:border-gray-600 rounded-md {heightClasses[
			height
		]} w-full transition-colors duration-200"
	></div>
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
