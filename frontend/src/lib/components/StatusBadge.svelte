<script>
	// small tinted pill with a dot and a text label for the state of a thing.
	// the label carries the meaning, the color only supports it.
	/** @type {'success'|'warning'|'danger'|'info'|'neutral'} */
	export let tone = 'neutral';
	export let title = '';
	let cssClass = '';
	export { cssClass as class };

	const TONES = {
		success: 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300',
		warning: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/40 dark:text-yellow-300',
		danger: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
		info: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300',
		neutral: 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
	};
</script>

<span
	class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-semibold leading-5 whitespace-nowrap {TONES[
		tone
	] ?? TONES.neutral} {cssClass}"
	title={title || undefined}
>
	<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-current" aria-hidden="true"></span>
	<slot />
</span>
