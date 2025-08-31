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

	const heightClasses = {
		small: 'h-64',
		medium: 'h-80',
		large: 'h-96'
	};

	onMount(() => {
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

		// Update value when editor content changes
		editor.getModel().onDidChangeContent(() => {
			value = editor.getValue();
		});

		return () => {
			if (editor) {
				editor.dispose();
			}
		};
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
		class="border border-gray-300 rounded-md {heightClasses[height]} w-full"
	></div>
	{#if placeholder}
		<div class="mt-2">
			<button
				type="button"
				on:click={() => (showExample = !showExample)}
				class="text-xs text-blue-600 hover:text-blue-800 underline focus:outline-none"
			>
				{showExample ? 'Hide' : 'Show'} example
			</button>
			{#if showExample}
				<div class="mt-2 p-3 bg-gray-50 border border-gray-200 rounded-md">
					<div class="flex justify-between items-start mb-2">
						<span class="text-xs font-medium text-gray-700">Example:</span>
						<button
							type="button"
							on:click={loadExample}
							class="text-xs text-blue-600 hover:text-blue-800 underline"
						>
							Load example
						</button>
					</div>
					<pre class="text-xs text-gray-600 whitespace-pre-wrap">{placeholder}</pre>
				</div>
			{/if}
		</div>
	{/if}
</div>
