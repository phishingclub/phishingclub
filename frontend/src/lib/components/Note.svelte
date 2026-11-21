<script>
	// a short note on something to look at: plain text with a small round icon
	// in the StatusBadge tone. The text carries the meaning, the icon only
	// supports it.
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

	// the StatusBadge tones
	const TONES = {
		warning: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-300',
		danger: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
		info: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
	};
	// an exclamation mark for a warning or a problem, an i for information
	const MARKS = {
		warning:
			'M10 4.5a1 1 0 0 1 1 1v5a1 1 0 1 1-2 0v-5a1 1 0 0 1 1-1Zm0 8.75a1.25 1.25 0 1 1 0 2.5 1.25 1.25 0 0 1 0-2.5Z',
		info: 'M10 4.25a1.25 1.25 0 1 1 0 2.5 1.25 1.25 0 0 1 0-2.5ZM10 8.5a1 1 0 0 1 1 1v5a1 1 0 1 1-2 0v-5a1 1 0 0 1 1-1Z'
	};
</script>

<div
	{id}
	{role}
	class="flex items-start gap-2 text-sm text-slate-700 dark:text-gray-200 transition-colors duration-200 {boxed
		? 'rounded-md border border-gray-200 bg-gray-50 px-3 py-2 dark:border-gray-700 dark:bg-gray-800/60'
		: ''} {cssClass}"
>
	<span
		class="mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full {TONES[
			tone
		] ?? TONES.warning}"
		aria-hidden="true"
	>
		<svg class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
			<path d={MARKS[tone] ?? MARKS.warning} />
		</svg>
	</span>
	<div class="min-w-0 flex-1"><slot /></div>
</div>
