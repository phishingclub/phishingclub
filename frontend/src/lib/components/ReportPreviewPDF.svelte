<script>
	import { api } from '$lib/api/apiProxy.js';
	import { addToast } from '$lib/store/toast';
	import ActionButton from './ActionButton.svelte';

	// the report template text as it is in the editor, saved or not
	export let content = '';
	export let isTraining = false;

	let isRendering = false;

	// renders the PDF and shows it in a new tab. The tab is opened on the click,
	// as browsers block a tab opened later, and gets the PDF once it is ready
	const render = async () => {
		if (isRendering || !content?.trim()) return;
		const tab = window.open('', '_blank');
		if (!tab) {
			addToast('Allow pop ups for this site to preview the PDF', 'Error');
			return;
		}
		// the tab only ever shows our PDF, it gets no handle back to this page
		tab.opener = null;
		tab.document.title = 'Rendering PDF';
		tab.document.body.textContent = 'Rendering PDF...';
		isRendering = true;
		try {
			const response = await api.reportTemplate.previewPDF(content, isTraining);
			if (!response?.success) {
				tab.close();
				addToast(response?.error || 'Failed to render the PDF', 'Error');
				return;
			}
			// typed as PDF so the browser only ever opens it in its PDF viewer
			const url = URL.createObjectURL(new Blob([response.data], { type: 'application/pdf' }));
			tab.location.href = url;
			// the tab reads the blob as it loads it
			setTimeout(() => URL.revokeObjectURL(url), 60000);
		} catch (e) {
			console.error('Failed to render the PDF preview:', e);
			tab.close();
			addToast('Failed to render the PDF', 'Error');
		} finally {
			isRendering = false;
		}
	};
</script>

<ActionButton
	variant="secondary"
	icon={isRendering ? 'spinner' : 'pdf'}
	disabled={isRendering || !content?.trim()}
	on:click={render}
>
	{isRendering ? 'Rendering PDF' : 'Preview PDF'}
</ActionButton>
