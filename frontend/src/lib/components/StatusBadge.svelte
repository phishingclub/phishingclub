<script context="module">
	// the dot color of each tone, in the app's own state colors: the calendar
	// legend, the chart legends and the stat tiles use the same ones.
	// draft is a gray dot for a thing not started yet; neutral is a hollow dot
	// for a state that is off or over.
	export const STATUS_DOTS = {
		active: 'bg-campaign-active dark:bg-highlight-blue',
		managed: 'bg-pc-purple',
		scheduled: 'bg-campaign-scheduled',
		success: 'bg-training-completed dark:bg-campaign-completed',
		info: 'bg-cta-blue dark:bg-highlight-blue',
		warning: 'bg-failed-sending',
		danger: 'bg-pc-red',
		draft: 'bg-gray-400 dark:bg-gray-500',
		neutral: 'ring-1 ring-inset ring-gray-400 dark:ring-gray-500'
	};
</script>

<script>
	// the state of a thing as a colored dot and its name in body text, as in
	// the calendar and chart legends. The label carries the meaning, the dot
	// only supports it.
	/** @type {'active'|'managed'|'scheduled'|'success'|'info'|'warning'|'danger'|'draft'|'neutral'} */
	export let tone = 'neutral';
	export let title = '';
	// a state that links to where it is handled
	export let href = '';
	let cssClass = '';
	export { cssClass as class };

	$: dot = STATUS_DOTS[tone] ?? STATUS_DOTS.neutral;
	// an off state recedes, a state that needs a look stands out
	$: text =
		tone === 'neutral'
			? 'text-slate-500 dark:text-gray-400'
			: tone === 'danger' || tone === 'warning'
				? 'font-semibold text-slate-800 dark:text-gray-100'
				: 'text-slate-700 dark:text-gray-200';
	// no font size of its own: the state reads at the size of the text around it
	$: classes = `inline-flex items-baseline gap-1.5 whitespace-nowrap align-baseline ${text} ${cssClass}`;
</script>

{#if href}
	<a
		{href}
		class="{classes} pc-focus rounded-sm underline-offset-2 transition-colors duration-200 hover:text-cta-blue hover:underline dark:hover:text-highlight-blue"
		title={title || undefined}
	>
		<span class="h-2 w-2 shrink-0 self-center rounded-full {dot}" aria-hidden="true"></span>
		<slot />
	</a>
{:else}
	<span class={classes} title={title || undefined}>
		<span class="h-2 w-2 shrink-0 self-center rounded-full {dot}" aria-hidden="true"></span>
		<slot />
	</span>
{/if}
