<script>
	// asks before a draft or scheduled program starts, with the number of
	// recipients that will be enrolled when it is known. With nobody in the
	// groups there is nothing to start. When the stored end cuts off templates
	// from a start now, or leaves the last one less time, it says so and offers
	// to move the end, which is saved through the normal update before the
	// start.
	import Alert from '$lib/components/Alert.svelte';
	import FormError from '$lib/components/FormError.svelte';
	import Note from '$lib/components/Note.svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { formatDate, formatDateTime, plural } from '$lib/utils/programText.js';
	import { startEndCheck } from '$lib/utils/programSchedule.js';

	export let visible = false;
	export let name = '';
	/** @type {number|null} */
	export let people = null;
	// the stored program, read for its end and schedule. Without it no end check is made.
	/** @type {any} */
	export let program = null;
	/** @type {() => Promise<any>} */
	export let onConfirm;
	// called after the end was moved, so the page can show the new end
	export let onEndMoved = () => {};

	let check = null;
	let isOpen = false;
	let movedTo = null;
	let isMoving = false;
	let moveError = '';
	// checked each time the alert opens, from the moment it opens, and again
	// when the program is read again while it is open
	$: if (visible && !isOpen) {
		isOpen = true;
		movedTo = null;
		moveError = '';
	}
	$: if (!visible) isOpen = false;
	$: check = visible && program ? startEndCheck(program, new Date()) : null;
	$: cut = check && (check.count < check.total || check.gap != null) ? check : null;
	$: noPeople = people === 0;

	// a time span in hours under a day, else in days
	const spanText = (ms) =>
		ms < 86400000
			? plural(Math.max(1, Math.round(ms / 3600000)), 'hour', 'hours')
			: plural(Math.round(ms / 86400000), 'day', 'days');
	const cutText = (c) => {
		const on = formatDate(program.endAt);
		if (c.count === 0) return `No template goes out before the end on ${on}.`;
		if (c.count < c.total)
			return `Only ${c.count} of ${c.total} templates go out before the end on ${on}.`;
		if (c.overlap)
			return `With Start over and this spread, the last template can go out as late as the next one could start, so some recipients may not get it before the end on ${on}.`;
		return c.gap > 0
			? `The last template goes out about ${spanText(c.gap)} before the end on ${on}. Its links stop working when the program closes.`
			: `The last template may still be going out when the program ends on ${on}.`;
	};

	const moveEnd = async () => {
		if (!cut?.fitEnd || isMoving) return;
		isMoving = true;
		moveError = '';
		try {
			const res = await api.program.update(program.id, {
				name: program.name,
				endAt: cut.fitEnd.toISOString()
			});
			if (!res.success) {
				moveError = res.error || 'The end could not be moved';
				return;
			}
			movedTo = cut.fitEnd;
			check = null;
			onEndMoved();
		} catch (e) {
			moveError = 'The end could not be moved';
			console.error('failed to move the program end', e);
		} finally {
			isMoving = false;
		}
	};

	// the outline button of the wizard
	const moveButtonClass =
		'pc-focus inline-flex items-center px-4 py-2 rounded-md border border-cta-blue text-cta-blue bg-white hover:bg-blue-50 dark:bg-gray-800 dark:border-highlight-blue dark:text-blue-200 dark:hover:bg-highlight-blue/20 text-sm font-semibold transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed';
</script>

<Alert
	headline="start program"
	ok="Start"
	tone="affirmative"
	okDisabled={noPeople}
	bind:visible
	{onConfirm}
>
	<p class="mt-4 mb-2 text-gray-700 dark:text-gray-200 transition-colors duration-200">
		Start <strong>{name}</strong>?
		{#if noPeople}
			<!-- the note below says why -->
		{:else if people != null}
			{plural(people, 'recipient', 'recipients')} will be enrolled and the first templates go out from
			the next sending time.
		{:else}
			The first templates go out from the next sending time.
		{/if}
	</p>
	{#if noPeople}
		<Note boxed id="program-start-no-people" class="mt-3"
			>The groups have no recipients, so nothing will be sent.</Note
		>
	{/if}
	{#if movedTo}
		<p
			id="program-start-end-moved"
			class="mt-3 text-sm text-gray-600 dark:text-gray-400"
			role="status"
		>
			The end is now {formatDateTime(movedTo)}.
		</p>
	{:else if cut && !noPeople}
		<div id="program-start-end-warning" class="mt-3">
			<Note boxed>{cutText(cut)}</Note>
			{#if cut.fitEnd}
				<button
					id="program-start-move-end"
					type="button"
					class="mt-2 {moveButtonClass}"
					title={formatDateTime(cut.fitEnd)}
					disabled={isMoving}
					on:click={moveEnd}>Move end to {formatDate(cut.fitEnd)}</button
				>
			{/if}
			{#if moveError}
				<div class="mt-2"><FormError message={moveError} /></div>
			{/if}
		</div>
	{/if}
</Alert>
