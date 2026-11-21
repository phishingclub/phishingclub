<script>
	// The timeline of a program: sends and the people who read, clicked,
	// submitted data or reported, or started or completed a training, per day
	// or week before now, and the sends planned from now on, with the start, the end, the close, skip dates and
	// the current pause marked. An anonymous program has no timeline, the
	// program page shows a note in its place.
	import { onMount } from 'svelte';
	import { api } from '$lib/api/apiProxy.js';
	import { formatDateTime, toZoneLocal } from '$lib/utils/programText.js';
	import Table from '$lib/components/table/Table.svelte';
	import TableRow from '$lib/components/table/TableRow.svelte';
	import TableCell from '$lib/components/table/TableCell.svelte';

	export let programID;
	export let companyID = null;
	export let timeZone = '';
	// shows the counts as a table instead of the chart
	export let showTable = false;
	// the program has or had a phishing template, a training template. The
	// phishing outcome rows show with phishing, the training rows with training.
	export let hasPhishing = true;
	export let hasTraining = false;

	// days back and ahead of today, a range over 62 days is counted per week
	const RANGES = [7, 30, 90];
	let range = 30;

	const DAY_MS = 86400000;
	// the top row holds sends, the rows below the outcomes on one shared scale
	const SEND_H = 56;
	const ROW_H = 34;
	const ROW_GAP = 6;
	const TOP = 32;
	const BOTTOM = 22;
	// below this many bars with data the count is written above each send bar
	const FEW_BARS = 12;
	// below this width the chart uses short row names and wider bars
	const PHONE_W = 480;
	// the least room per bar a count above it needs
	const LABEL_W = 16;

	// the row names are the names of the result tiles, a phone shows the short
	// ones. The Tailwind tokens of the campaign colors. In light mode the pale marks
	// get a darker outline of the same hue, so they keep a 3:1 contrast.
	const KINDS = {
		read: {
			label: 'Emails read',
			short: 'Read',
			bar: 'fill-message-read stroke-teal-600 dark:stroke-none',
			key: 'bg-message-read'
		},
		clicked: {
			label: 'Clicked link',
			short: 'Clicked',
			bar: 'fill-page-visited stroke-pink-600 dark:stroke-none',
			key: 'bg-page-visited'
		},
		submitted: {
			label: 'Data submitted',
			short: 'Submitted',
			bar: 'fill-submitted-data stroke-none',
			key: 'bg-submitted-data'
		},
		reported: {
			label: 'Reported',
			short: 'Reported',
			bar: 'fill-reported dark:fill-slate-300 stroke-none',
			key: 'bg-reported dark:bg-slate-300'
		},
		trainingStarted: {
			label: 'Training started',
			short: 'Started',
			bar: 'fill-training-started stroke-none',
			key: 'bg-training-started'
		},
		trainingCompleted: {
			label: 'Training completed',
			short: 'Completed',
			bar: 'fill-training-completed stroke-emerald-700 dark:stroke-none',
			key: 'bg-training-completed'
		}
	};
	const PHISHING_OUTCOMES = ['read', 'clicked', 'submitted', 'reported'];
	const TRAINING_OUTCOMES = ['trainingStarted', 'trainingCompleted'];
	// the outcome rows shown, on one shared scale
	$: outcomes = [
		...(hasPhishing ? PHISHING_OUTCOMES : []),
		...(hasTraining ? TRAINING_OUTCOMES : [])
	];
	// the training part of Sent is named when the program has both kinds
	$: splitSent = hasPhishing && hasTraining;
	const SENT_BAR = 'fill-message-sent stroke-sky-600 dark:stroke-none';
	const PLANNED_BAR = 'fill-recipient-scheduled/15 stroke-recipient-scheduled';
	const HELD_BAR = 'fill-gray-200/60 stroke-gray-400 dark:fill-gray-700/40 dark:stroke-gray-500';

	let timeline = null;
	let loadError = '';
	let isLoading = true;
	let seq = 0;
	let width = 0;
	let box;
	let hoverIdx = -1;
	let focusIdx = -1;
	let hasFocus = false;

	// a YYYY-MM-DD day moved by n days
	const addDays = (day, n) => {
		const [y, m, d] = day.split('-').map(Number);
		return new Date(Date.UTC(y, m - 1, d + n)).toISOString().slice(0, 10);
	};
	const dayMs = (day) => {
		const [y, m, d] = day.split('-').map(Number);
		return Date.UTC(y, m - 1, d);
	};
	const zone = () => timeZone || 'UTC';
	const todayKey = () => toZoneLocal(new Date(), zone()).slice(0, 10);

	/** loads the range, keeping what is shown while a refresh runs */
	export const refresh = async () => {
		const id = ++seq;
		try {
			const today = todayKey();
			const res = today
				? await api.program.timeline(
						programID,
						addDays(today, -range),
						addDays(today, range),
						companyID
					)
				: await api.program.timeline(programID, '', '', companyID);
			if (id !== seq) return;
			if (!res.success) {
				loadError = res.error || 'failed';
				return;
			}
			loadError = '';
			timeline = res.data;
		} catch (e) {
			if (id !== seq) return;
			loadError = 'failed';
			console.error('failed to load the program timeline', e);
		} finally {
			if (id === seq) isLoading = false;
		}
	};

	const retry = () => {
		isLoading = true;
		refresh();
	};

	const chooseRange = (value) => {
		range = Number(value);
		isLoading = true;
		hoverIdx = -1;
		focusIdx = -1;
		refresh();
	};

	// the width is read from a box that clips its content, so the chart can
	// never widen the box it is measured from, and a change below a whole
	// pixel is ignored
	const measure = () => {
		if (!box) return;
		const w = Math.floor(box.getBoundingClientRect().width);
		if (w > 0 && w !== width) width = w;
	};

	onMount(() => {
		measure();
		const resizeObserver = new ResizeObserver(measure);
		if (box) resizeObserver.observe(box);
		refresh();
		return () => resizeObserver.disconnect();
	});

	// layout
	$: narrow = width > 0 && width < PHONE_W;
	// the label column fits the longest row name, "Training completed" when training is shown
	$: left = narrow ? 64 : hasTraining ? 116 : 92;
	// room on the right for the top of the scales, as "max 25"
	$: right = narrow ? 38 : 46;
	$: rows = [
		{ key: 'sends', label: 'Sent / Planned', y: TOP, h: SEND_H },
		...outcomes.map((k, i) => ({
			key: k,
			label: narrow ? KINDS[k].short : KINDS[k].label,
			y: TOP + SEND_H + ROW_GAP * 2 + i * (ROW_H + ROW_GAP),
			h: ROW_H
		}))
	];
	$: plotBottom = rows[rows.length - 1].y + rows[rows.length - 1].h;
	$: height = plotBottom + BOTTOM;
	$: innerW = Math.max((width || 600) - left - right, 10);
	$: buckets = timeline?.buckets ?? [];
	$: totalDays = timeline ? (dayMs(timeline.to) - dayMs(timeline.from)) / DAY_MS + 1 : 1;
	$: dayW = innerW / Math.max(totalDays || 1, 1);
	$: isWeek = timeline?.bucket === 'week';
	$: today = timeline?.today ?? '';
	$: xOfDay = (day) => left + ((dayMs(day) - dayMs(timeline.from)) / DAY_MS) * dayW;
	// x of a moment, read on the wall clock of the program zone
	$: xOfTime = (value) => {
		const local = toZoneLocal(value, zone());
		if (!local) return null;
		const minutes = Number(local.slice(11, 13)) * 60 + Number(local.slice(14, 16));
		return xOfDay(local.slice(0, 10)) + (minutes / 1440) * dayW;
	};
	$: inRange = (x) => x !== null && Number.isFinite(x) && x >= left && x <= left + innerW;

	// each bucket with its place, its counts and what can apply to it
	$: cells = buckets.map((b) => {
		const x0 = xOfDay(b.day);
		const w = b.days * dayW;
		const last = addDays(b.day, b.days - 1);
		return {
			b,
			x0,
			w,
			cx: x0 + w / 2,
			last,
			hasPast: b.day <= today,
			hasFuture: last >= today,
			partial: isWeek && b.days < 7,
			sent: b.sent ?? 0,
			trainingSent: b.trainingSent ?? 0,
			planned: b.planned ?? 0,
			read: b.read ?? 0,
			clicked: b.clicked ?? 0,
			submitted: b.submitted ?? 0,
			reported: b.reported ?? 0,
			trainingStarted: b.trainingStarted ?? 0,
			trainingCompleted: b.trainingCompleted ?? 0
		};
	});
	$: cellW = cells.length ? innerW / cells.length : innerW;
	// a narrow chart gives the bars more of their day, so a month of days stays readable
	$: barW = Math.max(2, Math.min(18, cellW * (narrow ? 0.8 : 0.66)));
	// the send row and the outcome rows each have their own scale
	$: sendMax = Math.max(0, ...cells.map((c) => c.sent + c.planned));
	$: outcomeMax = Math.max(0, ...cells.flatMap((c) => outcomes.map((k) => c[k])));
	// an outcome row with no count at all, drawn with a stronger base line and a 0
	$: zeroRow = (key) => outcomes.includes(key) && cells.every((c) => !c[key]);
	// the top of a scale, written once per scale: on the send row and on the
	// first outcome row for the shared one, left out when the bars carry their counts
	$: scaleTop = (i) => (labelCounts ? 0 : i === 0 ? sendMax : i === 1 ? outcomeMax : 0);
	// a count of one is at least 2 px tall, zero draws nothing
	const barH = (count, max, room) => (count > 0 && max > 0 ? Math.max(2, (count / max) * room) : 0);
	$: sendRoom = SEND_H - (labelCounts ? 14 : 4);
	// an outcome row keeps room above its bars for the count
	$: outcomeRoom = ROW_H - (labelCounts ? 13 : 2);
	$: labelCounts =
		cellW >= LABEL_W && cells.filter((c) => c.sent + c.planned > 0).length <= FEW_BARS;
	$: isEmpty =
		cells.length > 0 &&
		cells.every((c) => c.sent + c.planned + outcomes.reduce((t, k) => t + c[k], 0) === 0);
	$: held = !!timeline?.pausedAt;

	// marks along the top, staggered onto a second line when they would touch.
	// Now is a line without a name, as on the campaign timeline.
	$: nowX = timeline ? xOfTime(new Date()) : null;
	$: marks = (() => {
		if (!timeline) return [];
		const list = [
			{ key: 'start', label: 'Start', x: timeline.startAt ? xOfTime(timeline.startAt) : null },
			{ key: 'end', label: 'End', x: timeline.endAt ? xOfTime(timeline.endAt) : null },
			{ key: 'closed', label: 'Closed', x: timeline.closedAt ? xOfTime(timeline.closedAt) : null }
		]
			.filter((m) => inRange(m.x))
			.map((m) => {
				const half = (m.label.length * 5.6 + 4) / 2;
				const tx = Math.min(Math.max(m.x, left + half), left + innerW + right - half);
				return { ...m, half, tx, level: 0 };
			})
			.sort((a, b) => a.tx - b.tx);
		const ends = [-Infinity, -Infinity];
		for (const m of list) {
			m.level = m.tx - m.half >= ends[0] + 4 ? 0 : 1;
			ends[m.level] = Math.max(ends[m.level], m.tx + m.half);
		}
		return list;
	})();
	$: skipRects = (timeline?.skipDays ?? []).map((s) => ({
		key: s.from,
		x: xOfDay(s.from),
		w: ((dayMs(s.to) - dayMs(s.from)) / DAY_MS + 1) * dayW
	}));
	// earlier pauses are not stored, so the current pause is shaded from now on
	$: pauseX =
		held && inRange(nowX) ? nowX : held && timeline && today < timeline.from ? left : null;

	// axis labels, spaced so they do not overlap
	$: labelEvery = Math.max(1, Math.ceil(cells.length / Math.max(1, Math.floor(innerW / 64))));
	const dayLabel = (day) =>
		new Intl.DateTimeFormat(undefined, { timeZone: 'UTC', day: 'numeric', month: 'short' }).format(
			new Date(dayMs(day))
		);
	const longDayLabel = (day) =>
		new Intl.DateTimeFormat(undefined, {
			timeZone: 'UTC',
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			year: 'numeric'
		}).format(new Date(dayMs(day)));
	$: titleOf = (c) =>
		isWeek ? `${dayLabel(c.b.day)} to ${dayLabel(c.last)}` : longDayLabel(c.b.day);
	$: isSkip = (c) => (timeline?.skipDays ?? []).some((s) => s.from <= c.last && s.to >= c.b.day);

	// the rows of the tooltip and the table, leaving out what cannot apply
	$: detailsOf = (c) => {
		const list = [];
		if (c.hasPast) list.push({ key: 'sent', label: 'Sent', value: c.sent });
		if (c.hasPast && splitSent)
			list.push({ key: 'trainingSent', label: 'Training sent', value: c.trainingSent });
		if (c.hasFuture) list.push({ key: 'planned', label: 'Planned', value: c.planned });
		if (c.hasPast) {
			for (const k of outcomes) list.push({ key: k, label: KINDS[k].label, value: c[k] });
		}
		return list;
	};
	$: notesOf = (c) => [
		...(c.partial ? ['Partial week, only the days in the range'] : []),
		...(isSkip(c) ? ['Skip date, nothing is sent'] : []),
		...(held && c.hasFuture && c.planned > 0 ? ['Moves when the program resumes'] : [])
	];
	$: announce = (c) =>
		c
			? `${titleOf(c)}: ${detailsOf(c)
					.map((r) => `${r.label} ${r.value}`)
					.join(', ')}${notesOf(c).length ? `. ${notesOf(c).join('. ')}` : ''}`
			: '';

	$: shownIdx = hoverIdx >= 0 ? hoverIdx : hasFocus ? focusIdx : -1;
	$: shown = shownIdx >= 0 ? cells[shownIdx] : null;
	// the tooltip sits beside the column, to the right when it fits there
	const TIP_W = 224;
	$: tipSide = shown && shown.x0 + shown.w + 6 + TIP_W > (width || 600) ? 'left' : 'right';
	// the tooltip is kept inside the chart, so on a phone it never runs past an
	// edge and may then cover the column
	$: tipLeft = shown ? (tipSide === 'right' ? shown.x0 + shown.w + 6 : shown.x0 - 6 - TIP_W) : 0;
	$: tipStyle = shown
		? `left: ${Math.round(Math.max(0, Math.min(tipLeft, (width || 600) - TIP_W)))}px`
		: '';

	$: totals = cells.reduce(
		(t, c) => ({
			sent: t.sent + c.sent,
			trainingSent: t.trainingSent + c.trainingSent,
			planned: t.planned + c.planned
		}),
		{ sent: 0, trainingSent: 0, planned: 0 }
	);
	$: zoneName = timeline?.timeZone || zone();
	$: ariaLabel = timeline
		? `Timeline from ${longDayLabel(timeline.from)} to ${longDayLabel(timeline.to)}, per ${isWeek ? 'week' : 'day'}. Use the arrow keys to move between ${isWeek ? 'weeks' : 'days'}.`
		: 'Timeline';

	// one index from the pointer x, so a column needs no target of its own
	// the columns are placed in the coordinates of the whole chart, so the
	// pointer is measured from the chart's left edge, not from the plot area
	const pointAt = (e) => {
		const svg = e.currentTarget.ownerSVGElement || e.currentTarget;
		const rect = svg.getBoundingClientRect();
		const scale = rect.width ? (width || rect.width) / rect.width : 1;
		const x = (e.clientX - rect.left) * scale;
		const i = cells.findIndex((c) => x >= c.x0 && x < c.x0 + c.w);
		hoverIdx = i;
	};
	const todayIdx = () => {
		const i = cells.findIndex((c) => c.b.day <= today && c.last >= today);
		return i >= 0 ? i : 0;
	};
	const onFocus = () => {
		hasFocus = true;
		if (focusIdx < 0 || focusIdx >= cells.length) focusIdx = todayIdx();
	};
	const onKey = (e) => {
		if (!cells.length) return;
		const moves = {
			ArrowLeft: focusIdx - 1,
			ArrowRight: focusIdx + 1,
			Home: 0,
			End: cells.length - 1
		};
		if (!(e.key in moves)) return;
		e.preventDefault();
		hoverIdx = -1;
		focusIdx = Math.min(Math.max(moves[e.key], 0), cells.length - 1);
	};
	// where a column of the table hides on a narrow screen
	const HIDE_BELOW = {
		trainingSent: 'lg',
		read: 'md',
		submitted: 'md',
		trainingStarted: 'lg',
		trainingCompleted: 'lg'
	};
	$: hideBelowOf = (k) =>
		k === 'read' || k === 'submitted' || splitSent ? (HIDE_BELOW[k] ?? '') : '';
	$: tableColumns = [
		{ column: isWeek ? 'Week' : 'Day', size: 'large' },
		{ column: 'Sent', size: 'small', compact: true },
		...(splitSent
			? [
					{
						column: 'Training sent',
						size: 'small',
						hideBelow: hideBelowOf('trainingSent'),
						compact: true
					}
				]
			: []),
		{ column: 'Planned', size: 'small', compact: true },
		...outcomes.map((k) => ({
			column: KINDS[k].label,
			size: 'small',
			compact: true,
			...(hideBelowOf(k) ? { hideBelow: hideBelowOf(k) } : {})
		}))
	];
	const noteCls = 'text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200';
</script>

<div
	class="w-full min-w-0 bg-white dark:bg-gray-900/80 rounded-lg shadow-md dark:shadow-none dark:ring-1 dark:ring-gray-600/30 p-4 sm:p-6 transition-colors duration-200"
>
	<!-- the keys of what the rows cannot name, and the range -->
	<div class="mb-1 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
		<div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-gray-300">
			<span class="inline-flex items-center gap-1.5">
				<span
					class="inline-block w-2.5 h-3 rounded-sm bg-message-sent ring-1 ring-sky-600 dark:ring-0"
					aria-hidden="true"
				></span>
				Sent
			</span>
			<span class="inline-flex items-center gap-1.5">
				<span
					class="inline-block w-2.5 h-3 rounded-sm bg-recipient-scheduled/15 border-[1.5px] border-recipient-scheduled"
					aria-hidden="true"
				></span>
				Planned
			</span>
			{#if timeline?.skipDays?.length}
				<span class="inline-flex items-center gap-1.5">
					<span
						class="inline-block w-3 h-3 rounded-sm bg-gray-200 dark:bg-gray-700/70"
						aria-hidden="true"
					></span>
					Skip dates
				</span>
			{/if}
			{#if held}
				<span class="inline-flex items-center gap-1.5">
					<span class="inline-block w-3 h-3 rounded-sm bg-closed/20" aria-hidden="true"></span>
					Paused
				</span>
			{/if}
		</div>
		<label
			class="flex flex-wrap items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
			title="Days back and ahead of today"
		>
			Time range:
			<select
				id="program-timeline-range"
				value={range}
				on:change={(e) => chooseRange(e.currentTarget.value)}
				class="pc-select pc-focus h-6 py-0"
			>
				{#each RANGES as r}
					<option value={r}>{`${r} days`}</option>
				{/each}
			</select>
			{#if timeline}
				<span id="program-timeline-dates" class="tabular-nums"
					>{dayLabel(timeline.from)} to {dayLabel(timeline.to)}</span
				>
			{/if}
		</label>
	</div>

	<div class="relative">
		<!-- the measured box clips its content, so the chart never sizes it -->
		<div bind:this={box} class="w-full min-w-0 overflow-hidden">
			{#if loadError && !timeline}
				<p
					class="flex flex-wrap items-center justify-center gap-x-2 {noteCls}"
					style="height: {height}px"
					role="status"
				>
					The timeline could not be loaded.
					<button
						type="button"
						class="pc-focus rounded-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue"
						on:click={retry}>Retry</button
					>
				</p>
			{:else if !timeline || !width}
				<!-- the placeholder shows on the first load only, a new range keeps the chart in place -->
				<svg width={width || 600} {height} aria-hidden="true" class="block animate-pulse">
					{#each rows as row}
						<rect
							x={left}
							y={row.y}
							width={innerW}
							height={row.h}
							rx="2"
							class="fill-gray-100 dark:fill-gray-800/70"
						/>
					{/each}
				</svg>
				<span class="sr-only" role="status">Loading the timeline</span>
			{:else}
				<!-- svelte-ignore a11y-no-noninteractive-tabindex a11y-no-noninteractive-element-interactions -->
				<div
					id="program-timeline-chart"
					role="group"
					tabindex="0"
					aria-label={ariaLabel}
					aria-busy={isLoading}
					class="pc-focus rounded-sm outline-none"
					on:focus={onFocus}
					on:blur={() => (hasFocus = false)}
					on:keydown={onKey}
				>
					<svg
						{width}
						{height}
						aria-hidden="true"
						class="block transition-opacity duration-200 {isLoading || loadError
							? 'opacity-50'
							: ''}"
					>
						<!-- skip dates and the pause behind the marks -->
						{#each skipRects as s (s.key)}
							<rect
								x={s.x}
								y={TOP}
								width={s.w}
								height={plotBottom - TOP}
								class="fill-gray-200/70 dark:fill-gray-700/50"
							/>
						{/each}
						{#if pauseX !== null}
							<rect
								x={pauseX}
								y={TOP}
								width={Math.max(left + innerW - pauseX, 0)}
								height={plotBottom - TOP}
								class="fill-closed/15 dark:fill-closed/20"
							/>
						{/if}
						{#if shown}
							<rect
								x={shown.x0}
								y={TOP}
								width={Math.max(shown.w, 1)}
								height={plotBottom - TOP}
								class="fill-gray-100 dark:fill-gray-800/70"
							/>
						{/if}

						<!-- row names on their base lines, the base lines and the top of each
						     scale. The line runs on under the name, so each row reads as one
						     ruled line like the grid of the trend chart. -->
						{#each rows as row, i}
							<text
								x={left - 8}
								y={row.y + row.h - 7}
								text-anchor="end"
								class="fill-gray-600 dark:fill-gray-300 text-[11px]"
								>{#if narrow && row.label.includes(' / ')}<tspan
										x={left - 8}
										y={row.y + row.h - 19}>{row.label.split(' / ')[0]} /</tspan
									><tspan x={left - 8} y={row.y + row.h - 7}>{row.label.split(' / ')[1]}</tspan
									>{:else}{row.label}{/if}</text
							>
							<line
								x1="0"
								x2={left}
								y1={row.y + row.h + 0.5}
								y2={row.y + row.h + 0.5}
								class="stroke-gray-200 dark:stroke-gray-700"
							/>
							<line
								x1={left}
								x2={left + innerW}
								y1={row.y + row.h + 0.5}
								y2={row.y + row.h + 0.5}
								class={zeroRow(row.key)
									? 'stroke-gray-400 dark:stroke-gray-500'
									: 'stroke-gray-300 dark:stroke-gray-600'}
							/>
							{#if zeroRow(row.key)}
								<text
									x={left + innerW + 4}
									y={row.y + row.h}
									dominant-baseline="middle"
									class="fill-gray-500 dark:fill-gray-400 text-[10px] tabular-nums">0</text
								>
							{:else if scaleTop(i) > 0}
								<text
									x={left + innerW + 4}
									y={i === 0 ? row.y + row.h - sendRoom : row.y}
									dominant-baseline="hanging"
									class="fill-gray-500 dark:fill-gray-400 text-[10px] tabular-nums"
									>max {scaleTop(i).toLocaleString()}</text
								>
							{/if}
						{/each}

						<!-- bars on the base line of each row -->
						{#each cells as c}
							{@const base = rows[0].y + rows[0].h}
							{@const sentH = barH(c.sent, sendMax, sendRoom)}
							{@const planH = barH(c.planned, sendMax, sendRoom)}
							{@const bx = c.cx - barW / 2}
							<g opacity={c.partial ? 0.55 : 1}>
								{#if sentH > 0}
									<rect
										x={bx + 0.5}
										y={base - sentH + 0.5}
										width={Math.max(barW - 1, 1)}
										height={Math.max(sentH - 1, 1)}
										stroke-width="1"
										stroke-dasharray={c.partial ? '2 1.5' : null}
										class={SENT_BAR}
									/>
								{/if}
								{#if planH > 0}
									<rect
										x={bx + 0.75}
										y={base - sentH - planH + 0.75}
										width={Math.max(barW - 1.5, 1)}
										height={Math.max(planH - 1.5, 0.5)}
										stroke-width="1.5"
										stroke-dasharray={c.partial ? '2 1.5' : null}
										class={held ? HELD_BAR : PLANNED_BAR}
									/>
								{/if}
								{#if labelCounts && sentH + planH > 0}
									<text
										x={c.cx}
										y={base - sentH - planH - 3}
										text-anchor="middle"
										class="fill-gray-700 dark:fill-gray-300 text-[10px] tabular-nums"
										>{(c.sent + c.planned).toLocaleString()}</text
									>
								{/if}
								{#each outcomes as k, li}
									{@const row = rows[li + 1]}
									{@const h = barH(c[k], outcomeMax, outcomeRoom)}
									{#if h > 0}
										<rect
											x={bx + 0.5}
											y={row.y + row.h - h + 0.5}
											width={Math.max(barW - 1, 1)}
											height={Math.max(h - 1, 1)}
											stroke-width="1"
											class={KINDS[k].bar}
										/>
										{#if labelCounts}
											<text
												x={c.cx}
												y={row.y + row.h - h - 3}
												text-anchor="middle"
												class="fill-gray-700 dark:fill-gray-300 text-[10px] tabular-nums"
												>{c[k].toLocaleString()}</text
											>
										{/if}
									{/if}
								{/each}
							</g>
						{/each}

						<!-- start, end and close, each named at the top, and now as a line -->
						{#each marks as m (m.key)}
							<line
								x1={m.x}
								x2={m.x}
								y1={TOP - (m.level ? 15 : 4)}
								y2={plotBottom}
								stroke-width="1.5"
								class="stroke-gray-500 dark:stroke-gray-400"
							/>
							<text
								x={m.tx}
								y={TOP - (m.level ? 18 : 7)}
								text-anchor="middle"
								class="text-[10px] font-semibold fill-gray-600 dark:fill-gray-300">{m.label}</text
							>
						{/each}
						{#if inRange(nowX)}
							<line
								x1={nowX}
								x2={nowX}
								y1={TOP}
								y2={plotBottom}
								stroke-width="2"
								stroke-dasharray="4,4"
								opacity="0.8"
								class="stroke-cta-blue dark:stroke-highlight-blue"
							/>
						{/if}

						<!-- axis -->
						{#each cells as c, i}
							{#if i % labelEvery === 0}
								<text
									x={Math.min(Math.max(c.cx, left + 16), left + innerW - 16)}
									y={height - 6}
									text-anchor="middle"
									class="fill-gray-500 dark:fill-gray-400 text-[11px]">{dayLabel(c.b.day)}</text
								>
							{/if}
						{/each}

						{#if shown && hasFocus && hoverIdx < 0}
							<rect
								x={shown.x0 + 0.5}
								y={TOP + 0.5}
								width={Math.max(shown.w - 1, 1)}
								height={plotBottom - TOP - 1}
								fill="none"
								stroke-width="1.5"
								class="stroke-cta-blue dark:stroke-highlight-blue"
							/>
						{/if}
						<!-- one pointer target over the plot -->
						<rect
							role="presentation"
							x={left}
							y={TOP}
							width={innerW}
							height={plotBottom - TOP}
							fill="transparent"
							on:mousemove={pointAt}
							on:mouseleave={() => (hoverIdx = -1)}
						/>
					</svg>
				</div>
				<p class="sr-only" aria-live="polite">{hasFocus && shown ? announce(shown) : ''}</p>

				{#if isEmpty}
					<p
						class="pointer-events-none absolute inset-x-0 text-center text-sm text-gray-600 dark:text-gray-400"
						style="top: {TOP + SEND_H / 2 - 10}px"
					>
						<span class="rounded bg-white/90 dark:bg-gray-900/90 px-2 py-0.5"
							>Nothing sent or planned in this range</span
						>
					</p>
				{/if}
			{/if}
		</div>

		{#if shown}
			<div
				id="program-timeline-tip"
				class="pointer-events-none absolute z-20 w-56 max-w-full bg-white dark:bg-gray-900 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 px-3 py-2 text-xs text-gray-700 dark:text-gray-300"
				style="top: 0; {tipStyle}"
				aria-hidden="true"
			>
				<p class="mb-1 text-sm font-bold text-gray-800 dark:text-gray-200">{titleOf(shown)}</p>
				<div class="space-y-1">
					{#each detailsOf(shown) as row}
						<p class="flex items-center justify-between gap-3 tabular-nums">
							<span class="inline-flex items-center gap-1.5">
								{#if row.key === 'sent'}
									<span
										class="inline-block w-2.5 h-2.5 rounded-sm bg-message-sent ring-1 ring-sky-600 dark:ring-0"
									></span>
								{:else if row.key === 'trainingSent'}
									<span class="inline-block w-2.5 h-2.5" aria-hidden="true"></span>
								{:else if row.key === 'planned'}
									<span
										class="inline-block w-2.5 h-2.5 rounded-sm border-[1.5px] {held
											? 'bg-gray-200/60 border-gray-400 dark:bg-gray-700/40 dark:border-gray-500'
											: 'bg-recipient-scheduled/15 border-recipient-scheduled'}"
									></span>
								{:else}
									<span class="inline-block w-2.5 h-2.5 rounded-sm {KINDS[row.key].key}"></span>
								{/if}
								{row.label}
							</span>
							<span class="font-semibold">{row.value.toLocaleString()}</span>
						</p>
					{/each}
					{#each notesOf(shown) as n}
						<p class="text-gray-500 dark:text-gray-400">{n}</p>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	{#if timeline && loadError}
		<p class="mt-2 flex flex-wrap items-center gap-x-2 {noteCls}" role="status">
			The timeline could not be updated.
			<button
				type="button"
				class="pc-focus rounded-sm font-medium text-cta-blue hover:underline dark:text-highlight-blue"
				on:click={retry}>Retry</button
			>
		</p>
	{/if}
	{#if timeline}
		<p id="program-timeline-totals" class="mt-3 {noteCls} {isLoading ? 'opacity-50' : ''}">
			{totals.sent.toLocaleString()} sent{#if splitSent}{' '}({totals.trainingSent.toLocaleString()}
				training){/if}, {totals.planned.toLocaleString()} planned in this range. People per {isWeek
				? 'week'
				: 'day'}, by the day it happened. Days in {zoneName}.
		</p>
		{#if held}
			<p id="program-timeline-paused" class="mt-1 {noteCls}">
				Paused since {formatDateTime(timeline.pausedAt)}. Planned sends move when it resumes.
			</p>
		{/if}
	{/if}
</div>

{#if showTable && timeline}
	<div id="program-timeline-table" class="mt-3">
		<Table
			columns={tableColumns}
			noSearch
			hasActions={false}
			plural={isWeek ? 'weeks' : 'days'}
			hasData={cells.length > 0}
		>
			{#each cells as c (c.b.day)}
				<TableRow>
					<TableCell>
						{titleOf(c)}{#if c.partial}{' '}<span class="text-gray-500 dark:text-gray-400"
								>(partial week)</span
							>{/if}
					</TableCell>
					<TableCell><span class="tabular-nums">{c.hasPast ? c.sent : ''}</span></TableCell>
					{#if splitSent}
						<TableCell hideBelow={hideBelowOf('trainingSent')}>
							<span class="tabular-nums">{c.hasPast ? c.trainingSent : ''}</span>
						</TableCell>
					{/if}
					<TableCell><span class="tabular-nums">{c.hasFuture ? c.planned : ''}</span></TableCell>
					{#each outcomes as k}
						<TableCell hideBelow={hideBelowOf(k)}>
							<span class="tabular-nums">{c.hasPast ? c[k] : ''}</span>
						</TableCell>
					{/each}
				</TableRow>
			{/each}
		</Table>
	</div>
{/if}
