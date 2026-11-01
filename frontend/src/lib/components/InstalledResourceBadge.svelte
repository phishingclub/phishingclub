<script>
	import { onMount } from 'svelte';
	import {
		installedResources,
		loadInstalledResources,
		installedInfo
	} from '$lib/store/installedResources.js';

	// resourceType and resourceId identify the resource being viewed. variant
	// picks the layout: 'notice' is the quiet panel shown on edit forms, 'badge'
	// is the compact marker for a list row.
	export let resourceType;
	export let resourceId;
	export let variant = 'notice';

	onMount(() => {
		loadInstalledResources();
	});

	// info is the package entry for this resource, or null when it was not
	// installed from a package. It recomputes when the store fills.
	$: info = installedInfo($installedResources, resourceType, resourceId);
</script>

{#if info}
	{#if variant === 'badge'}
		<!-- a quiet marker in the app's own muted surface, not a loud colour -->
		<span
			class="ml-2 inline-flex shrink-0 items-center gap-1 rounded bg-grayblue-light px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:bg-gray-800/60 dark:text-gray-400"
			title={`Installed from ${info.packageId}`}
		>
			<svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
				<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
				<polyline points="3.27 6.96 12 12.01 20.73 6.96" />
				<line x1="12" y1="22.08" x2="12" y2="12" />
			</svg>
			Installed
		</span>
	{:else}
		<!-- the muted panel used for in form messages (matches the form field and
		FormError surfaces), with a small blue accent icon rather than a caution colour.
		mt-6 gives a gap below the modal header; mx-6 insets it to line up with the form
		fields, which sit inside a FormColumn with the same px-6 inset. -->
		<div
			class="mx-6 mt-6 flex items-start gap-2.5 rounded-md border border-gray-200 bg-grayblue-light px-3 py-2.5 dark:border-gray-700/60 dark:bg-gray-800/40"
		>
			<svg
				class="mt-0.5 h-4 w-4 shrink-0 text-cta-blue dark:text-highlight-blue"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
				aria-hidden="true"
			>
				<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
				<polyline points="3.27 6.96 12 12.01 20.73 6.96" />
				<line x1="12" y1="22.08" x2="12" y2="12" />
			</svg>
			<p class="text-sm leading-relaxed text-gray-600 dark:text-gray-300">
				Installed from <span class="font-semibold text-gray-800 dark:text-gray-100">{info.packageId}</span>. Edits here may be flagged as a conflict when the package is updated.
			</p>
		</div>
	{/if}
{/if}
