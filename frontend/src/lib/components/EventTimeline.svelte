<script>
	import { onMount, onDestroy } from 'svelte';
	import * as d3 from 'd3';
	import { toEvent } from '$lib/utils/events';

	export let events = [];
	export let isGhost = false;

	let svg;
	let tooltip;
	let container;
	let timelineGroup;
	let width;
	let height = 200;
	let margin = { top: 20, right: 40, bottom: 50, left: 40 };
	let currentCenterDate = '';
	let zoom;
	let currentTransform = null;
	let xScale;
	let use24Hour = true;
	let initialized = false;

	// Constants for zoom limits
	const MAX_ZOOM_IN = 1000 * 10; // Milliseconds minimum visible on screen
	const MIN_ZOOM_OUT_PADDING = 0.1; // 10% padding on each side when fully zoomed out

	$: if (container) {
		width = container.offsetWidth - margin.left - margin.right;
	}

	onDestroy(() => {
		// Clean up D3 event listeners
		if (svg) {
			d3.select(svg).on('.zoom', null);
		}

		// Clean up tooltip event listeners
		if (timelineGroup) {
			timelineGroup.selectAll('circle').on('mouseover', null).on('mouseout', null);
		}
	});

	function initializeTimeline() {
		if (isGhost) {
			handleGhost();
			return;
		}

		if (!svg) return;

		const g = d3
			.select(svg)
			.append('g')
			.attr('transform', `translate(${margin.left},${margin.top})`);

		g.append('defs')
			.append('clipPath')
			.attr('id', 'clip')
			.append('rect')
			.attr('width', width)
			.attr('height', height - margin.top - margin.bottom);

		timelineGroup = g.append('g').attr('clip-path', 'url(#clip)');

		// Base timeline
		timelineGroup
			.append('line')
			.attr('x1', 0)
			.attr('x2', width)
			.attr('y1', (height - margin.top - margin.bottom) / 2)
			.attr('y2', (height - margin.top - margin.bottom) / 2)
			.attr('stroke', '#E2E8F0')
			.attr('stroke-width', 2);

		// X Axis
		g.append('g')
			.attr('class', 'x-axis')
			.attr('transform', `translate(0,${height - margin.bottom})`);

		// Setup default time domain based on events
		if (events.length > 0) {
			const timestamps = events.map((d) => new Date(d.createdAt).getTime());
			const timeRange = d3.extent(timestamps);
			const timeSpan = timeRange[1] - timeRange[0];
			const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);

			const domainStart = new Date(timeRange[0] - padding);
			const domainEnd = new Date(timeRange[1] + padding);

			xScale = d3.scaleTime().domain([domainStart, domainEnd]).range([0, width]);
		} else {
			const now = new Date();
			const defaultStart = new Date(now);
			defaultStart.setHours(now.getHours() - 24);
			xScale = d3.scaleTime().domain([defaultStart, now]).range([0, width]);
		}

		// Initialize the zoom behavior with calculated scale limits
		setupZoom();

		updateAxes(xScale);
		initialized = true;
	}

	function setupZoom() {
		// Create a custom zoom behavior that handles the limits smoothly
		zoom = d3
			.zoom()
			.scaleExtent([0.1, 1000000]) // Use wide bounds
			.extent([
				[0, 0],
				[width, height]
			])
			.filter((event) => {
				// Check if this zoom operation would exceed our limit
				if (event.type === 'wheel' || event.type === 'touchmove' || event.type === 'mousemove') {
					// Get current visible timespan
					if (!currentTransform || !xScale) return true;

					const newX = currentTransform.rescaleX(xScale);
					const visibleDomain = newX.domain();
					const visibleTimeSpan = visibleDomain[1] - visibleDomain[0];

					// If we're already at 1 second and trying to zoom in further, block it
					if (
						visibleTimeSpan <= MAX_ZOOM_IN &&
						((event.type === 'wheel' && event.deltaY < 0) ||
							(event.type !== 'wheel' &&
								event.sourceEvent?.type === 'wheel' &&
								event.sourceEvent?.deltaY < 0))
					) {
						return false; // Block zoom in when already at max zoom
					}
				}
				return true; // Allow all other zoom operations
			})
			.on('zoom', handleZoom);

		d3.select(svg).call(zoom);
	}

	function updateTimeline() {
		if (!initialized) {
			initializeTimeline();
		}

		if (!svg || !timelineGroup) return;

		// Update scales based on events
		const timestamps = events.map((d) => new Date(d.createdAt));
		if (timestamps.length === 0) {
			const now = new Date();
			// Create a default domain of last 24 hours to now
			const defaultStart = new Date(now);
			defaultStart.setHours(now.getHours() - 24);
			xScale = d3.scaleTime().domain([defaultStart, now]).range([0, width]);
			updateAxes(xScale);
			return;
		}

		const timeRange = d3.extent(timestamps);
		const timeSpan = timeRange[1] - timeRange[0];
		// Smaller minimum padding for shorter time spans
		const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000); // minimum 1 minute padding

		const domainStart = new Date(timeRange[0].getTime() - padding);
		const domainEnd = new Date(timeRange[1].getTime() + padding);

		xScale = d3.scaleTime().domain([domainStart, domainEnd]).range([0, width]);

		// Update zoom constraints based on the new data
		setupZoom();

		// Update dots
		const dots = timelineGroup.selectAll('circle').data(events, (d) => d.id);

		dots.exit().remove();

		const dotsEnter = dots
			.enter()
			.append('circle')
			.attr('r', 6)
			.attr('stroke', '#fff')
			.attr('stroke-width', 2)
			.on('mouseover', showTooltip)
			.on('mouseout', hideTooltip);

		dots
			.merge(dotsEnter)
			.attr('cx', (d) => xScale(new Date(d.createdAt)))
			.attr('cy', (height - margin.top - margin.bottom) / 2)
			.attr('fill', (d) => getEventColor(d.eventName));

		if (currentTransform) {
			const newX = currentTransform.rescaleX(xScale);
			updateAxes(newX);
			timelineGroup.selectAll('circle').attr('cx', (d) => newX(new Date(d.createdAt)));
		} else {
			updateAxes(xScale);
			// Set initial zoom to fit all events nicely
			const optimalScale = calculateOptimalScale(domainStart, domainEnd);
			const initialTranslate = (width - width * optimalScale) / 2;
			currentTransform = d3.zoomIdentity.translate(initialTranslate, 0).scale(optimalScale);
			d3.select(svg).call(zoom.transform, currentTransform);
		}
	}

	function calculateOptimalScale(start, end) {
		if (!start || !end) return 0.9; // Default fallback

		const timeSpan = end - start;
		// Target visible timespan should be the actual timespan plus some padding
		const targetTimeSpan = timeSpan * (1 + MIN_ZOOM_OUT_PADDING * 2);
		const currentScale = width / targetTimeSpan;

		// Constrain to a reasonable range for initial view
		return Math.min(Math.max(currentScale, 0.7), 0.95);
	}

	function handleZoom(event) {
		if (!xScale) return;

		// Store the current transform
		currentTransform = event.transform;

		// Apply the transform to the x scale
		const newX = event.transform.rescaleX(xScale);

		// Update the visualization with the new scale
		updateAxes(newX);
		timelineGroup.selectAll('circle').attr('cx', (d) => newX(new Date(d.createdAt)));
		timelineGroup.select('line').attr('x1', 0).attr('x2', width);
	}

	function updateAxes(scale) {
		if (!scale || !scale.domain || typeof scale.domain !== 'function') {
			console.error('Invalid scale provided to updateAxes');
			return;
		}

		const domain = scale.domain();
		const timeSpan = domain[1] - domain[0];

		// Calculate available width per tick to avoid overlap
		const minTickSpacing = 80; // Minimum pixels between ticks
		const maxTicks = Math.floor(width / minTickSpacing);

		const xAxis = d3.select(svg).select('.x-axis');
		if (!xAxis.empty()) {
			let tickFunction;
			let tickFormat;

			if (timeSpan < 1000) {
				// Less than 1 second
				// For very zoomed in views, use fewer time ticks with millisecond precision
				tickFunction = d3.timeMillisecond.every(Math.ceil(timeSpan / maxTicks / 10) * 10);
				tickFormat = d3.timeFormat(use24Hour ? '%H:%M:%S.%L' : '%I:%M:%S.%L %p');
			} else if (timeSpan < 60000) {
				// Less than 1 minute
				tickFunction = d3.timeSecond.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 1000)));
				tickFormat = d3.timeFormat(use24Hour ? '%H:%M:%S' : '%I:%M:%S %p');
			} else if (timeSpan < 3600000) {
				// Less than 1 hour
				tickFunction = d3.timeMinute.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 60000)));
				tickFormat = d3.timeFormat(use24Hour ? '%H:%M' : '%I:%M %p');
			} else if (timeSpan < 86400000) {
				// Less than 1 day
				tickFunction = d3.timeHour.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 3600000)));
				tickFormat = d3.timeFormat(use24Hour ? '%H:%M' : '%I:%M %p');
			} else if (timeSpan < 2592000000) {
				// Less than 30 days
				tickFunction = d3.timeDay.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 86400000)));
				tickFormat = d3.timeFormat('%d %b');
			} else if (timeSpan < 31536000000) {
				// Less than 1 year
				tickFunction = d3.timeMonth.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 2592000000)));
				tickFormat = d3.timeFormat('%b %Y');
			} else {
				// More than 1 year
				tickFunction = d3.timeYear.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 31536000000)));
				tickFormat = d3.timeFormat('%Y');
			}

			// Apply the ticks with calculated frequency
			xAxis.call(d3.axisBottom(scale).ticks(tickFunction).tickFormat(tickFormat));

			xAxis
				.selectAll('text')
				.style('text-anchor', 'middle')
				.attr('dy', '1em')
				.attr('dx', '0')
				.attr('transform', 'rotate(0)');
		}

		updateCenterDate(scale);
	}

	function updateCenterDate(scale) {
		if (!scale || !scale.invert) return;

		const centerX = width / 2;
		const centerDate = scale.invert(centerX);
		const timeSpan = scale.domain()[1] - scale.domain()[0];

		const options = {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit',
			hour12: !use24Hour
		};

		if (timeSpan < 60000) {
			options.fractionalSecondDigits = 3;
		}

		try {
			currentCenterDate = centerDate.toLocaleDateString('en-US', options);
		} catch (e) {
			console.error('Error formatting center date:', e);
			currentCenterDate = '';
		}
	}

	function handleGhost() {
		if (!svg) return;

		d3.select(svg).selectAll('*').remove();

		const g = d3
			.select(svg)
			.append('g')
			.attr('transform', `translate(${margin.left},${margin.top})`);

		g.append('line')
			.attr('x1', 0)
			.attr('x2', width)
			.attr('y1', (height - margin.top - margin.bottom) / 2)
			.attr('y2', (height - margin.top - margin.bottom) / 2)
			.attr('stroke', '#E2E8F0')
			.attr('stroke-width', 2);

		const ghostDots = 8;
		const ghostDotsData = Array(ghostDots).fill(null);

		g.selectAll('.ghost-dot')
			.data(ghostDotsData)
			.enter()
			.append('circle')
			.attr('class', 'ghost-dot')
			.attr('cx', (_, i) => (width / (ghostDots - 1)) * i)
			.attr('cy', (height - margin.top - margin.bottom) / 2)
			.attr('r', 6)
			.attr('fill', '#E2E8F0')
			.attr('stroke', '#fff')
			.attr('stroke-width', 2);
	}

	function showTooltip(event, d) {
		if (!tooltip || !d || !d.eventName) return;

		const [x, y] = d3.pointer(event);
		tooltip.style.display = 'block';

		// Ensure tooltip stays within container bounds
		const tooltipWidth = 250; // Approximate width
		const leftPosition = x + margin.left + 10;
		const rightEdge = leftPosition + tooltipWidth;
		// If tooltip would go off the right side, position it to the left of the point
		const adjustedLeft =
			rightEdge > container.offsetWidth ? x + margin.left - tooltipWidth - 10 : leftPosition;

		tooltip.style.left = `${adjustedLeft}px`;
		tooltip.style.top = `${y + margin.top - 10}px`;

		try {
			tooltip.innerHTML = `
				<div class="p-2">
					<div class="font-semibold text-lg text-gray-700 border-b-2">${toEvent(d.eventName).name}</div>
					<div class="">${d.recipient?.email ?? ''}</div>
					<div class="text-xs text-gray-600">${new Date(d.createdAt).toLocaleString()}</div>
					${d.data ? `<div class="text-xs mt-1">${d.data}</div>` : ''}
				</div>
			`;
		} catch (e) {
			console.error('Error showing tooltip:', e);
			hideTooltip();
		}
	}

	function hideTooltip() {
		if (tooltip) {
			tooltip.style.display = 'none';
		}
	}

	function resetZoom() {
		if (!svg || !zoom || !xScale || events.length === 0) return;

		try {
			// If we have events, zoom to fit all events
			if (events.length > 0) {
				const timestamps = events.map((d) => new Date(d.createdAt).getTime());
				const timeRange = d3.extent(timestamps);
				const timeSpan = timeRange[1] - timeRange[0];

				// Add padding
				const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);
				const domainStart = new Date(timeRange[0] - padding);
				const domainEnd = new Date(timeRange[1] + padding);

				// Calculate optimal scale
				const optimalScale = calculateOptimalScale(domainStart, domainEnd);

				// Create transform to center the events
				const initialTranslate = (width - width * optimalScale) / 2;
				currentTransform = d3.zoomIdentity.translate(initialTranslate, 0).scale(optimalScale);

				// Apply transform with animation
				d3.select(svg).transition().duration(750).call(zoom.transform, currentTransform);
			} else {
				// Default view for no events
				const fullViewScale = 0.8;
				const initialX = (width - width * fullViewScale) / 2;
				currentTransform = d3.zoomIdentity.translate(initialX, 0).scale(fullViewScale);
				d3.select(svg).transition().duration(750).call(zoom.transform, currentTransform);
			}
		} catch (e) {
			console.error('Error resetting zoom:', e);
		}
	}

	function getEventColor(eventName) {
		if (!eventName) return '#6B7280'; // Default gray for undefined events

		const colorMap = {
			campaign_scheduled: '#4e68d8',
			campaign_active: '#53afe3',
			campaign_self_managed: '#303f9f',
			campaign_closed: '#9f9f9f',
			campaign_recipient_scheduled: '#4e68d8',
			campaign_recipient_message_sent: '#94cae6',
			campaign_recipient_message_failed: '#f2bb58',
			campaign_recipient_message_read: '#4cb5b5',
			campaign_recipient_before_page_visited: '#eea5fa',
			campaign_recipient_page_visited: '#f96dcf',
			campaign_recipient_after_page_visited: '#f6287b',
			campaign_recipient_submitted_data: '#f42e41',
			campaign_recipient_cancelled: '#161692',
			default: '#6B7280'
		};
		return colorMap[eventName] || colorMap.default;
	}

	$: if (svg && events) {
		updateTimeline();
	}
</script>

<div class="relative cursor-move" bind:this={container}>
	<div class="transition-opacity duration-300" class:animate-pulse={isGhost}>
		<svg
			bind:this={svg}
			width="100%"
			height={height + 20}
			class="bg-white rounded-lg shadow-sm p-2"
		/>
		<div
			bind:this={tooltip}
			class="absolute hidden bg-white shadow-lg rounded-lg border border-gray-200 z-10 pointer-events-none"
		/>
		{#if !isGhost}
			<div class="absolute top-2 right-2 flex gap-2">
				<button
					on:click={() => {
						use24Hour = !use24Hour;
						updateTimeline();
					}}
					class="px-2 py-1 text-xs bg-white border border-slate-200 rounded shadow-sm hover:bg-slate-50 text-slate-600"
				>
					{use24Hour ? '12h' : '24h'}
				</button>
				<button
					on:click={resetZoom}
					class="px-2 py-1 text-xs bg-white border border-slate-200 rounded shadow-sm hover:bg-slate-50 text-slate-600"
				>
					Reset View
				</button>
			</div>
			<div
				class="absolute top-0 left-1/2 transform -translate-x-1/2 bg-white px-3 py-1 rounded-b-lg shadow-sm border border-t-0 text-sm font-medium text-slate-700"
			>
				{currentCenterDate}
			</div>
			<div class="absolute bottom-0 right-0 p-2 text-xs text-slate-500">
				Drag to pan • Scroll to zoom
			</div>
		{/if}
	</div>
</div>

<style>
	:global(.x-axis text) {
		font-size: 11px;
		font-weight: 500;
		fill: #4a5568;
	}
	:global(.x-axis line) {
		stroke: #e2e8f0;
	}
	:global(.x-axis path) {
		stroke: #e2e8f0;
	}
	:global(.ghost-dot) {
		animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
	}

	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.5;
		}
	}
</style>
