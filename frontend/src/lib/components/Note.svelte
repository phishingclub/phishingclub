<script>
	// a short note on something to look at: plain text, the words carry the
	// meaning. No icon and no color accent.
	// Unboxed when it sits inline under the field or control it is about, or in
	// the notes under a page header. boxed when it stands on its own in a
	// modal, a card or a page section.
	// No live role by default. Pass role="status", or "alert" for a failure,
	// only where the note appears after an action, so a screen reader reads it.
	/** @type {'warning'|'danger'|'info'} */
	export let tone = 'warning';
	export let boxed = false;
	export let id = undefined;
	/** @type {string|undefined} */
	export let role = undefined;
	let cssClass = '';
	export { cssClass as class };
</script>

<!-- the boxed fill is the darker inset the inputs use. bg-gray-50 is not used
     as app.css turns it into the card color in dark mode -->
<div
	{id}
	{role}
	data-tone={tone}
	class="text-sm text-slate-700 dark:text-gray-200 transition-colors duration-200 {boxed
		? 'rounded-md border border-gray-200 bg-pleasant-gray px-3 py-2 dark:border-gray-700 dark:bg-gray-900/40'
		: ''} {cssClass}"
>
	<slot />
</div>
