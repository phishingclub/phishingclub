<script>
	// Monthly click, submit and report rates as a small line chart.
	// months: [{ key, name, sent, clicked, submitted, reported }]
	import { NONE_YET, PROGRAM_MIN_GROUP, ratePercent } from '$lib/utils/programText.js';

	export let months = [];

	// months with fewer emails than this get a note in the tooltip
	const FEW_EMAILS = PROGRAM_MIN_GROUP;
	const height = 220;
	const margin = { top: 12, right: 48, bottom: 30, left: 44 };

	const series = [
		{
			key: 'clicked',
			label: 'Click rate',
			short: 'click',
			stroke: 'stroke-page-visited',
			fill: 'fill-page-visited',
			dash: ''
		},
		{
			key: 'submitted',
			label: 'Submit rate',
			short: 'submit',
			stroke: 'stroke-submitted-data',
			fill: 'fill-submitted-data',
			dash: '5 3'
		},
		{
			key: 'reported',
			label: 'Report rate',
			short: 'report',
			stroke: 'stroke-reported dark:stroke-slate-400',
			fill: 'fill-reported dark:fill-slate-400',
			dash: ''
		}
	];

	let width = 600;
	let hoverIdx = -1;
	let hoverSeries = '';

	const rateOf = (m, key) => ratePercent(m[key], m.sent);

	const niceStep = (v) => {
		for (const s of [1, 2, 5, 10, 20, 25, 50]) {
			if (v / s <= 4) return s;
		}
		return 25;
	};

	$: innerW = Math.max(width - margin.left - margin.right, 10);
	$: innerH = height - margin.top - margin.bottom;
	$: maxRate = Math.max(0, ...months.flatMap((m) => series.map((s) => rateOf(m, s.key) ?? 0)));
	$: step = maxRate > 0 ? niceStep(maxRate) : 5;
	$: yMax = Math.max(step * 2, Math.ceil(maxRate / step) * step);
	$: ticks = Array.from({ length: Math.round(yMax / step) + 1 }, (_, i) => i * step);
	$: xAt = (i) =>
		margin.left + (months.length > 1 ? (i * innerW) / (months.length - 1) : innerW / 2);
	$: yAt = (v) => margin.top + innerH - (v / yMax) * innerH;
	$: labelEvery = Math.max(1, Math.ceil(months.length / Math.max(1, Math.floor(innerW / 72))));
	$: slotW = months.length > 1 ? innerW / (months.length - 1) : innerW;

	// builds the path, leaving gaps for months without sends
	const pathFor = (key, list, x, y) => {
		let d = '';
		let pen = false;
		list.forEach((m, i) => {
			const r = rateOf(m, key);
			if (r === null) {
				pen = false;
				return;
			}
			d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(r).toFixed(1)} `;
			pen = true;
		});
		return d.trim();
	};

	const fmt = (v) => (v === null ? NONE_YET : `${Math.round(v)}%`);
	// an anonymous program leaves out a count that could point at a few people
	const fmtOf = (m, key) =>
		m.suppressed || (m.sent != null && m[key] == null) ? 'Hidden' : fmt(rateOf(m, key));

	// the hovered series is drawn last so it sits on top
	$: drawOrder = hoverSeries
		? [
				...series.filter((s) => s.key !== hoverSeries),
				...series.filter((s) => s.key === hoverSeries)
			]
		: series;

	// last known value per series, labels pushed apart so they do not overlap
	$: endLabels = (() => {
		const list = [];
		for (const s of series) {
			for (let i = months.length - 1; i >= 0; i--) {
				const r = rateOf(months[i], s.key);
				if (r !== null) {
					list.push({ s, value: r, x: xAt(i), y: yAt(r) });
					break;
				}
			}
		}
		// series ending on the same value share one gray label
		const merged = [];
		for (const l of list) {
			const same = merged.find((m) => fmt(m.value) === fmt(l.value));
			if (same) {
				same.shared = true;
				same.key = `${same.key}-${l.s.key}`;
			} else {
				merged.push({ ...l, key: l.s.key, shared: false });
			}
		}
		// push labels apart upward, so they never drop onto the month labels
		merged.sort((a, b) => b.y - a.y);
		for (let i = 1; i < merged.length; i++) {
			if (merged[i - 1].y - merged[i].y < 12) merged[i].y = merged[i - 1].y - 12;
		}
		return merged;
	})();

	const monthLabel = (m) =>
		`${m.name}: ${series.map((s) => `${s.short} ${fmtOf(m, s.key)}`).join(', ')}, ${m.sent ?? 0} ${m.sent === 1 ? 'email' : 'emails'}`;

	$: hovered = hoverIdx >= 0 ? months[hoverIdx] : null;
	$: tipLeft = hoverIdx >= 0 ? Math.min(Math.max(xAt(hoverIdx), 80), width - 80) : 0;
	$: ariaLabel =
		months.length > 0
			? `Monthly rates from ${months[0].name} to ${months[months.length - 1].name}`
			: 'Monthly rates';
</script>

<div class="w-full">
	<div class="flex flex-wrap gap-x-5 gap-y-1 mb-2 text-xs text-gray-600 dark:text-gray-300">
		{#each series as s}
			<span
				class="inline-flex items-center gap-1.5"
				role="presentation"
				on:mouseenter={() => (hoverSeries = s.key)}
				on:mouseleave={() => (hoverSeries = '')}
			>
				<svg width="18" height="4" aria-hidden="true" class="overflow-visible">
					<line
						x1="1"
						x2="17"
						y1="2"
						y2="2"
						stroke-width="2.5"
						stroke-linecap="round"
						stroke-dasharray={s.dash || null}
						class={s.stroke}
					/>
				</svg>
				{s.label}
			</span>
		{/each}
	</div>
	<div class="relative" bind:clientWidth={width}>
		<svg {width} {height} role="group" aria-label={ariaLabel} class="block overflow-visible">
			{#each ticks as t}
				<line
					x1={margin.left}
					x2={margin.left + innerW}
					y1={yAt(t)}
					y2={yAt(t)}
					class="stroke-gray-200 dark:stroke-gray-700"
					stroke-width="1"
				/>
				<text
					x={margin.left - 8}
					y={yAt(t)}
					text-anchor="end"
					dominant-baseline="middle"
					class="fill-gray-500 dark:fill-gray-400 text-[11px] tabular-nums">{t}%</text
				>
			{/each}
			{#each months as m, i}
				{#if i % labelEvery === 0 || i === months.length - 1}
					<text
						x={xAt(i)}
						y={height - 8}
						text-anchor="middle"
						class="fill-gray-500 dark:fill-gray-400 text-[11px]">{m.name}</text
					>
				{/if}
			{/each}
			{#if hoverIdx >= 0}
				<line
					x1={xAt(hoverIdx)}
					x2={xAt(hoverIdx)}
					y1={margin.top}
					y2={margin.top + innerH}
					class="stroke-gray-300 dark:stroke-gray-600"
					stroke-dasharray="3,3"
				/>
			{/if}
			{#each drawOrder as s (s.key)}
				<g
					class="transition-opacity duration-150"
					opacity={hoverSeries && hoverSeries !== s.key ? 0.35 : 1}
				>
					<path
						d={pathFor(s.key, months, xAt, yAt)}
						fill="none"
						stroke-width="2.5"
						stroke-linecap="round"
						stroke-linejoin="round"
						stroke-dasharray={s.dash || null}
						class={s.stroke}
					/>
					{#each months as m, i}
						{#if rateOf(m, s.key) !== null}
							<circle
								cx={xAt(i)}
								cy={yAt(rateOf(m, s.key))}
								r={hoverIdx === i ? 4 : 2.5}
								stroke-width="2"
								class="fill-white dark:fill-gray-900 {s.stroke}"
							/>
						{/if}
					{/each}
				</g>
			{/each}
			{#each endLabels as l (l.key)}
				<text
					x={l.x + 8}
					y={l.y}
					dominant-baseline="middle"
					class="text-[11px] font-semibold tabular-nums {l.shared
						? 'fill-gray-600 dark:fill-gray-300'
						: l.s.fill}"
					opacity={hoverSeries && !l.shared && hoverSeries !== l.s.key ? 0.35 : 1}
					>{fmt(l.value)}</text
				>
			{/each}
			{#each months as m, i}
				<rect
					role="button"
					tabindex="0"
					aria-label={monthLabel(m)}
					x={xAt(i) - slotW / 2}
					y={margin.top}
					width={slotW}
					height={innerH}
					fill="transparent"
					class="outline-none focus-visible:stroke-cta-blue dark:focus-visible:stroke-highlight-blue focus-visible:stroke-2"
					on:mouseenter={() => (hoverIdx = i)}
					on:mouseleave={() => (hoverIdx = -1)}
					on:focus={() => (hoverIdx = i)}
					on:blur={() => (hoverIdx = -1)}
					on:click={() => (hoverIdx = i)}
					on:keydown={(e) => {
						if (e.key === 'Enter' || e.key === ' ') hoverIdx = i;
					}}
				/>
			{/each}
		</svg>
		{#if hovered}
			<div
				class="pointer-events-none absolute top-0 -translate-x-1/2 rounded-md bg-white dark:bg-gray-800 px-3 py-2 text-xs shadow-lg ring-1 ring-gray-200 dark:ring-gray-700 text-gray-700 dark:text-gray-200"
				style="left: {tipLeft}px"
			>
				<p class="font-semibold mb-1">{hovered.name}</p>
				<p class="text-gray-600 dark:text-gray-400 mb-1 tabular-nums">
					{hovered.sent ?? 0}
					{hovered.sent === 1 ? 'email' : 'emails'}{#if (hovered.sent ?? 0) < FEW_EMAILS},
						<span class="font-semibold text-yellow-800 dark:text-yellow-300">few emails</span>{/if}
				</p>
				{#each series as s}
					<p class="flex items-center justify-between gap-4 tabular-nums">
						<span class="inline-flex items-center gap-1.5">
							<svg width="14" height="4" aria-hidden="true" class="overflow-visible">
								<line
									x1="1"
									x2="13"
									y1="2"
									y2="2"
									stroke-width="2.5"
									stroke-linecap="round"
									stroke-dasharray={s.dash ? '3 2' : null}
									class={s.stroke}
								/>
							</svg>{s.label}
						</span>
						<span class="font-semibold">{fmtOf(hovered, s.key)}</span>
					</p>
				{/each}
			</div>
		{/if}
	</div>
</div>
