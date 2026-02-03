<script>
	import { onMount, onDestroy } from 'svelte';
	import * as d3 from 'd3';
	import { toEvent } from '$lib/utils/events';

	// props
	export let events = [];
	export let isGhost = false;
	export let refreshInterval = 60000;

	// hoisted constant lookup tables (created once, not per-call)
	const EVENT_COLORS = Object.freeze({
		campaign_scheduled: '#62aded',
		campaign_active: '#5557f6',
		campaign_self_managed: '#9622fc',
		campaign_closed: '#9f9f9f',
		campaign_recipient_scheduled: '#4e68d8',
		campaign_recipient_cancelled: '#161692',
		campaign_recipient_message_sent: '#94cae6',
		campaign_recipient_message_failed: '#f2bb58',
		campaign_recipient_message_read: '#4cb5b5',
		campaign_recipient_evasion_page_visited: '#c8a2f0',
		campaign_recipient_before_page_visited: '#eea5fa',
		campaign_recipient_page_visited: '#f96dcf',
		campaign_recipient_after_page_visited: '#f6287b',
		campaign_recipient_deny_page_visited: '#ff6b35',
		campaign_recipient_submitted_data: '#f42e41',
		campaign_recipient_reported: '#2c3e50'
	});

	const EVENT_ICONS = Object.freeze({
		campaign_scheduled:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>',
		campaign_active:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.828 14.828a4 4 0 01-5.656 0M9 10h1m4 0h1m-6 4h1m4 0h1m-6 4h1m4 0h1M4 20h16a2 2 0 002-2V6a2 2 0 00-2-2H4a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>',
		campaign_self_managed:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"/></svg>',
		campaign_closed:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
		campaign_recipient_scheduled:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"/></svg>',
		campaign_recipient_cancelled:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>',
		campaign_recipient_message_sent:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 01-2.25 2.25h-15a2.25 2.25 0 01-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25m19.5 0v.243a2.25 2.25 0 01-1.07 1.916l-7.5 4.615a2.25 2.25 0 01-2.36 0L3.32 8.91a2.25 2.25 0 01-1.07-1.916V6.75"/></svg>',
		campaign_recipient_message_failed:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.072 16.5c-.77.833.192 2.5 1.732 2.5z"/></svg>',
		campaign_recipient_message_read:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/></svg>',
		campaign_recipient_evasion_page_visited:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>',
		campaign_recipient_before_page_visited:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/></svg>',
		campaign_recipient_page_visited:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"/></svg>',
		campaign_recipient_after_page_visited:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/></svg>',
		campaign_recipient_deny_page_visited:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636"/></svg>',
		campaign_recipient_submitted_data:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
		campaign_recipient_reported:
			'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.072 16.5c-.77.833.192 2.5 1.732 2.5z"/></svg>'
	});

	const DEFAULT_ICON =
		'<svg fill="currentColor" viewBox="0 0 20 20"><circle cx="10" cy="10" r="3"></circle></svg>';
	const DEFAULT_COLOR = '#6b7280';

	// performance constants
	const MAX_ZOOM_IN = 1000;
	const MIN_ZOOM_OUT_PADDING = 0.1;
	const MAX_VISIBLE_EVENTS = 1000;
	const AXIS_DEBOUNCE_MS = 150;
	const TOOLTIP_HIDE_DELAY = 100;
	const RESIZE_DEBOUNCE_MS = 100;
	const CURRENT_TIME_UPDATE_MS = 1000; // update every second so NOW line moves visibly

	// layout constants
	const HEIGHT = 200;
	const MARGIN = Object.freeze({ top: 20, right: 20, bottom: 30, left: 20 });

	// theme state
	let isDarkMode = false;

	// dom references
	let svg;
	let container;
	let timelineGroup;
	let currentTimeIndicator;
	let currentTimeWindow;
	let circlesGroup;
	let circleSelection = null;

	// layout state
	let width = 800;

	// timeline state
	let currentCenterDate = new Date();
	let zoom;
	let currentTransform = null;
	let xScale;
	let use24Hour = true;
	let initialized = false;
	let showFilterDropdown = false;

	// tooltip state (reactive for svelte template)
	let tooltipVisible = false;
	let tooltipX = 0;
	let tooltipY = 0;
	let tooltipEvent = null;

	// filter state
	let eventFilters = {
		campaign_recipient_scheduled: true,
		campaign_recipient_cancelled: true,
		campaign_recipient_message_sent: true,
		campaign_recipient_message_failed: true,
		campaign_recipient_message_read: true,
		campaign_recipient_evasion_page_visited: true,
		campaign_recipient_before_page_visited: true,
		campaign_recipient_page_visited: true,
		campaign_recipient_after_page_visited: true,
		campaign_recipient_deny_page_visited: true,
		campaign_recipient_submitted_data: true,
		campaign_recipient_reported: true
	};

	// caching - pre-processed events with timestamps
	let processedEvents = [];
	let filteredEventsCache = null;
	let lastFilterHash = '';
	let lastEventsHash = '';

	// animation and timing handles
	let rafId = null;
	let pendingZoomUpdate = false;
	let axisUpdateId = null;
	let tooltipHideId = null;
	let resizeTimeoutId = null;
	let currentTimeIntervalId = null;
	let themeObserver = null;
	let resizeObserver = null;

	// cached scale for RAF updates
	let pendingScale = null;

	// pre-cached circle nodes for fast iteration
	let circleNodes = [];
	let circleData = [];

	// inline helper functions
	const getEventColor = (eventName) => EVENT_COLORS[eventName] || DEFAULT_COLOR;
	const getEventIcon = (eventName) => EVENT_ICONS[eventName] || DEFAULT_ICON;

	function checkTheme() {
		isDarkMode = document.documentElement.classList.contains('dark');
	}

	function updateTimelineColors() {
		if (!timelineGroup) return;
		const strokeColor = isDarkMode ? '#374151' : '#fff';
		const centerLineColor = isDarkMode ? '#6b7280' : '#d1d5db';
		const indicatorColor = isDarkMode ? '#6887ea' : '#445ecc';
		const windowFill = isDarkMode ? 'rgba(104, 135, 234, 0.15)' : 'rgba(68, 94, 204, 0.15)';

		if (circlesGroup) {
			circlesGroup.selectAll('circle').attr('stroke', strokeColor);
		}
		timelineGroup.select('.center-line').attr('stroke', centerLineColor);
		if (currentTimeIndicator) {
			currentTimeIndicator.attr('stroke', indicatorColor);
		}
		if (currentTimeWindow) {
			currentTimeWindow.attr('fill', windowFill);
		}
	}

	// pre-process events with numeric timestamps for fast access
	function processEvents(eventsList) {
		if (!eventsList || eventsList.length === 0) return [];

		return eventsList
			.filter((e) => e?.eventName && e?.createdAt)
			.map((e) => ({
				...e,
				_ts: new Date(e.createdAt).getTime() // pre-computed timestamp
			}));
	}

	function getFilteredEvents() {
		// fast path: no events
		if (processedEvents.length === 0) {
			filteredEventsCache = [];
			return filteredEventsCache;
		}

		// check cache validity
		const filterHash = JSON.stringify(eventFilters);
		if (filterHash === lastFilterHash && filteredEventsCache) {
			return filteredEventsCache;
		}

		const hasAnyRecipientFiltersEnabled = Object.values(eventFilters).some(Boolean);

		filteredEventsCache = processedEvents.filter((event) => {
			// always show non-recipient campaign events
			if (
				event.eventName.startsWith('campaign_') &&
				!event.eventName.startsWith('campaign_recipient_')
			) {
				return true;
			}

			// filter recipient events
			if (!hasAnyRecipientFiltersEnabled) return false;
			return eventFilters[event.eventName] === true;
		});

		lastFilterHash = filterHash;
		return filteredEventsCache;
	}

	function getVisibleEvents(scale) {
		const filteredEvents = getFilteredEvents();
		if (!scale || filteredEvents.length <= MAX_VISIBLE_EVENTS) {
			return filteredEvents;
		}

		const domain = scale.domain();
		const rangeStart = domain[0].getTime();
		const rangeEnd = domain[1].getTime();
		const padding = (rangeEnd - rangeStart) * 0.1;
		const extendedStart = rangeStart - padding;
		const extendedEnd = rangeEnd + padding;

		// use pre-computed _ts for fast filtering
		const eventsInRange = filteredEvents.filter(
			(e) => e._ts >= extendedStart && e._ts <= extendedEnd
		);

		if (eventsInRange.length > MAX_VISIBLE_EVENTS) {
			const step = Math.ceil(eventsInRange.length / MAX_VISIBLE_EVENTS);
			return eventsInRange.filter((_, i) => i % step === 0);
		}

		return eventsInRange;
	}

	function updateCurrentTimeIndicatorImmediate() {
		if (!currentTimeIndicator || !xScale) return;

		const scale = currentTransform ? currentTransform.rescaleX(xScale) : xScale;
		const now = Date.now();
		const nowDate = new Date(now);
		const xPos = scale(nowDate);
		const domain = scale.domain();

		if (nowDate >= domain[0] && nowDate <= domain[1]) {
			const intervalMs = Math.max(refreshInterval, 60000);
			const windowStartTime = new Date(now - intervalMs);
			const xWindowStart = scale(windowStartTime);
			const windowWidth = Math.max(xPos - xWindowStart, 2);

			currentTimeIndicator.attr('x1', xPos).attr('x2', xPos).attr('opacity', 0.8);
			if (currentTimeWindow) {
				currentTimeWindow.attr('x', xWindowStart).attr('width', windowWidth).attr('opacity', 0.5);
			}
		} else {
			currentTimeIndicator.attr('opacity', 0);
			if (currentTimeWindow) {
				currentTimeWindow.attr('opacity', 0);
			}
		}
	}

	function updateAxesImmediate(scale) {
		if (!scale?.domain) return;

		const domain = scale.domain();
		const timeSpan = domain[1] - domain[0];
		const minTickSpacing = 80;
		const maxTicks = Math.floor(width / minTickSpacing);

		const xAxis = d3.select(svg).select('.x-axis');
		if (xAxis.empty()) return;

		let tickFunction;
		let tickFormat;

		if (timeSpan < 1000) {
			tickFunction = d3.timeMillisecond.every(Math.ceil(timeSpan / maxTicks / 10) * 10);
			tickFormat = d3.timeFormat(use24Hour ? '%H:%M:%S.%L' : '%I:%M:%S.%L %p');
		} else if (timeSpan < 60000) {
			tickFunction = d3.timeSecond.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 1000)));
			tickFormat = d3.timeFormat(use24Hour ? '%H:%M:%S' : '%I:%M:%S %p');
		} else if (timeSpan < 3600000) {
			tickFunction = d3.timeMinute.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 60000)));
			tickFormat = d3.timeFormat(use24Hour ? '%H:%M' : '%I:%M %p');
		} else if (timeSpan < 86400000) {
			tickFunction = d3.timeHour.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 3600000)));
			tickFormat = d3.timeFormat(use24Hour ? '%H:%M' : '%I:%M %p');
		} else if (timeSpan < 2592000000) {
			tickFunction = d3.timeDay.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 86400000)));
			tickFormat = d3.timeFormat('%d %b');
		} else if (timeSpan < 31536000000) {
			tickFunction = d3.timeMonth.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 2592000000)));
			tickFormat = d3.timeFormat('%b %Y');
		} else {
			tickFunction = d3.timeYear.every(Math.max(1, Math.ceil(timeSpan / maxTicks / 31536000000)));
			tickFormat = d3.timeFormat('%Y');
		}

		xAxis.call(d3.axisBottom(scale).ticks(tickFunction).tickFormat(tickFormat));
		xAxis.selectAll('text').style('text-anchor', 'middle').attr('dy', '1em');

		updateCenterDate(scale);
		updateCurrentTimeIndicatorImmediate();
	}

	function updateAxesDebounced(scale) {
		if (axisUpdateId) clearTimeout(axisUpdateId);
		axisUpdateId = setTimeout(() => updateAxesImmediate(scale), AXIS_DEBOUNCE_MS);
	}

	function updateCenterDate(scale) {
		if (!scale) return;
		try {
			const domain = scale.domain();
			const centerTime = new Date((domain[0].getTime() + domain[1].getTime()) / 2);
			if (Math.abs(centerTime.getTime() - currentCenterDate.getTime()) > 1000) {
				currentCenterDate = centerTime;
			}
		} catch {
			// ignore errors
		}
	}

	function calculateOptimalScale(start, end) {
		if (!start || !end) return 0.9;
		const timeSpan = end - start;
		const targetTimeSpan = timeSpan * (1 + MIN_ZOOM_OUT_PADDING * 2);
		return Math.min(Math.max(width / targetTimeSpan, 0.7), 0.95);
	}

	// calculate optimal view based on events and current time
	// returns { domainStart, domainEnd, centerTs } for the view
	function calculateOptimalView(filteredEvents) {
		const now = Date.now();

		if (filteredEvents.length === 0) {
			// no events: show last 24 hours centered on now
			const dayMs = 24 * 60 * 60 * 1000;
			return {
				domainStart: new Date(now - dayMs),
				domainEnd: new Date(now),
				centerTs: now - dayMs / 2
			};
		}

		// fast min/max calculation
		let minTs = filteredEvents[0]._ts;
		let maxTs = filteredEvents[0]._ts;
		for (let i = 1; i < filteredEvents.length; i++) {
			const ts = filteredEvents[i]._ts;
			if (ts < minTs) minTs = ts;
			if (ts > maxTs) maxTs = ts;
		}

		const timeSpan = maxTs - minTs;
		const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);

		if (now > maxTs) {
			// now is after last event: center view on middle of campaign
			const campaignMiddle = minTs + timeSpan / 2;
			return {
				domainStart: new Date(minTs - padding),
				domainEnd: new Date(maxTs + padding),
				centerTs: campaignMiddle
			};
		} else {
			// last event hasn't happened yet (scheduled): pan to NOW
			// use same time span as campaign but centered on now
			const viewSpan = Math.max(timeSpan + padding * 2, 60 * 60 * 1000); // at least 1 hour
			return {
				domainStart: new Date(now - viewSpan / 2),
				domainEnd: new Date(now + viewSpan / 2),
				centerTs: now
			};
		}
	}

	// cache circle nodes for fast iteration during zoom/pan
	function cacheCircleNodes() {
		if (!circlesGroup) {
			circleNodes = [];
			circleData = [];
			return;
		}
		const selection = circlesGroup.selectAll('circle');
		circleNodes = selection.nodes();
		circleData = selection.data();
	}

	// render circles - only called on data change or zoom end
	function renderCircles(scale) {
		if (!scale || !circlesGroup) return;

		const visibleData = getVisibleEvents(scale);
		const strokeColor = isDarkMode ? '#374151' : '#fff';
		const yPos = (HEIGHT - MARGIN.top - MARGIN.bottom) / 2;

		const circles = circlesGroup.selectAll('circle').data(visibleData, (d) => d.id || d._ts);

		circles.exit().remove();

		const entered = circles
			.enter()
			.append('circle')
			.attr('r', 6)
			.attr('stroke', strokeColor)
			.attr('stroke-width', 1)
			.attr('cy', yPos)
			.attr('cursor', 'pointer');

		entered
			.merge(circles)
			.attr('cx', (d) => scale(d._ts))
			.attr('fill', (d) => getEventColor(d.eventName));

		// cache nodes after render
		cacheCircleNodes();
	}

	// ultra-fast position update using cached nodes - no d3 overhead
	function updateCirclePositionsFast(scale) {
		const len = circleNodes.length;
		for (let i = 0; i < len; i++) {
			circleNodes[i].setAttribute('cx', scale(circleData[i]._ts));
		}
	}

	// RAF-batched zoom handler
	function scheduleZoomUpdate() {
		if (pendingZoomUpdate) return;
		pendingZoomUpdate = true;

		rafId = requestAnimationFrame(() => {
			pendingZoomUpdate = false;
			if (pendingScale && circleNodes.length > 0) {
				updateCirclePositionsFast(pendingScale);
			}
		});
	}

	function handleZoom(event) {
		if (!xScale) return;

		currentTransform = event.transform;
		pendingScale = currentTransform.rescaleX(xScale);

		// schedule RAF update for circle positions
		scheduleZoomUpdate();

		// hide time indicator during interaction
		if (currentTimeIndicator) currentTimeIndicator.attr('opacity', 0);
		if (currentTimeWindow) currentTimeWindow.attr('opacity', 0);

		// debounce expensive operations (axis, virtualization)
		updateAxesDebounced(pendingScale);
	}

	function handleZoomEnd() {
		if (!pendingScale) return;

		// on zoom end, do full re-render with virtualization
		renderCircles(pendingScale);
		updateAxesImmediate(pendingScale);
		updateCurrentTimeIndicatorImmediate();
	}

	function setupZoom() {
		zoom = d3
			.zoom()
			.scaleExtent([0.1, 1000000])
			.extent([
				[0, 0],
				[width, HEIGHT]
			])
			.filter((event) => {
				if (event.type === 'wheel' && currentTransform && xScale) {
					const newX = currentTransform.rescaleX(xScale);
					const visibleTimeSpan = newX.domain()[1] - newX.domain()[0];
					if (visibleTimeSpan <= MAX_ZOOM_IN && event.deltaY < 0) {
						return false;
					}
				}
				return true;
			})
			.on('zoom', handleZoom)
			.on('end', handleZoomEnd);

		d3.select(svg).call(zoom);
	}

	function updateTimeline() {
		if (!initialized || !svg || !timelineGroup || !xScale) return;

		const filteredEvents = getFilteredEvents();

		if (filteredEvents.length === 0) {
			if (circlesGroup) {
				circlesGroup.selectAll('circle').remove();
				circleNodes = [];
				circleData = [];
			}
			updateCenterDate(xScale);
			return;
		}

		// use pre-computed timestamps with fast min/max
		const timestamps = filteredEvents.map((d) => d._ts);
		let minTs = timestamps[0];
		let maxTs = timestamps[0];
		for (let i = 1; i < timestamps.length; i++) {
			if (timestamps[i] < minTs) minTs = timestamps[i];
			if (timestamps[i] > maxTs) maxTs = timestamps[i];
		}
		const timeSpan = maxTs - minTs;
		const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);

		const domainStart = new Date(minTs - padding);
		const domainEnd = new Date(maxTs + padding);

		xScale = d3.scaleTime().domain([domainStart, domainEnd]).range([0, width]);

		// use transformed scale if available, otherwise base scale
		const activeScale = currentTransform ? currentTransform.rescaleX(xScale) : xScale;

		renderCircles(activeScale);
		updateAxesImmediate(activeScale);
		updateCenterDate(xScale);
		updateCurrentTimeIndicatorImmediate();

		if (!currentTransform) {
			const optimalScale = calculateOptimalScale(domainStart, domainEnd);
			const initialTranslate = (width - width * optimalScale) / 2;
			currentTransform = d3.zoomIdentity.translate(initialTranslate, 0).scale(optimalScale);
			d3.select(svg).call(zoom.transform, currentTransform);
		}
	}

	function handleGhost() {
		if (!initialized || !timelineGroup) return;

		if (isGhost) {
			if (circlesGroup) circlesGroup.selectAll('circle').style('display', 'none');

			if (timelineGroup.selectAll('.ghost-dot').empty()) {
				const ghostCount = Math.min(12, Math.max(3, Math.floor(width / 80)));
				const ghostData = Array.from({ length: ghostCount }, (_, i) => ({
					x: (width / (ghostCount - 1)) * i,
					id: `ghost-${i}`
				}));

				timelineGroup
					.selectAll('.ghost-dot')
					.data(ghostData)
					.enter()
					.append('circle')
					.attr('class', 'ghost-dot')
					.attr('r', 6)
					.attr('cx', (d) => d.x)
					.attr('cy', (HEIGHT - MARGIN.top - MARGIN.bottom) / 2)
					.attr('fill', 'none')
					.attr('stroke', isDarkMode ? '#6b7280' : '#9ca3af')
					.attr('stroke-width', 1);
			}
		} else {
			timelineGroup.selectAll('.ghost-dot').remove();
			if (circlesGroup) circlesGroup.selectAll('circle').style('display', null);
			updateTimeline();
		}
	}

	function handleResize() {
		if (resizeTimeoutId) clearTimeout(resizeTimeoutId);
		resizeTimeoutId = setTimeout(() => {
			if (container && initialized) {
				const newWidth = container.offsetWidth - MARGIN.left - MARGIN.right;
				if (newWidth > 0 && newWidth !== width) {
					width = newWidth;
					if (xScale) {
						xScale.range([0, width]);
						updateTimeline();
					}
				}
			}
		}, RESIZE_DEBOUNCE_MS);
	}

	function initializeTimeline() {
		if (!svg || !container) return;

		const containerWidth = container.offsetWidth;
		width = containerWidth > 0 ? containerWidth - MARGIN.left - MARGIN.right : 800;

		d3.select(svg).selectAll('*').remove();

		timelineGroup = d3
			.select(svg)
			.append('g')
			.attr('transform', `translate(${MARGIN.left}, ${MARGIN.top})`);

		const yCenter = (HEIGHT - MARGIN.top - MARGIN.bottom) / 2;

		// timeline base line
		timelineGroup
			.append('line')
			.attr('class', 'timeline-line')
			.attr('x1', 0)
			.attr('y1', yCenter)
			.attr('x2', width)
			.attr('y2', yCenter)
			.attr('stroke', isDarkMode ? '#374151' : '#e5e7eb')
			.attr('stroke-width', 2);

		// center reference line
		timelineGroup
			.append('line')
			.attr('class', 'center-line')
			.attr('x1', width / 2)
			.attr('y1', 0)
			.attr('x2', width / 2)
			.attr('y2', HEIGHT - MARGIN.top - MARGIN.bottom)
			.attr('stroke', isDarkMode ? '#6b7280' : '#d1d5db')
			.attr('stroke-width', 1)
			.attr('opacity', 0.4);

		// current time window
		currentTimeWindow = timelineGroup
			.append('rect')
			.attr('class', 'current-time-window')
			.attr('y', 0)
			.attr('height', HEIGHT - MARGIN.top - MARGIN.bottom)
			.attr('fill', isDarkMode ? 'rgba(104, 135, 234, 0.15)' : 'rgba(68, 94, 204, 0.15)')
			.attr('opacity', 0.5);

		// current time indicator
		currentTimeIndicator = timelineGroup
			.append('line')
			.attr('class', 'current-time-indicator')
			.attr('y1', 0)
			.attr('y2', HEIGHT - MARGIN.top - MARGIN.bottom)
			.attr('stroke', isDarkMode ? '#6887ea' : '#445ecc')
			.attr('stroke-width', 2)
			.attr('stroke-dasharray', '4,4')
			.attr('opacity', 0.8);

		// dedicated group for circles (better for transforms)
		circlesGroup = timelineGroup.append('g').attr('class', 'circles-group');

		// x-axis
		d3.select(svg)
			.append('g')
			.attr('class', 'x-axis')
			.attr('transform', `translate(${MARGIN.left}, ${HEIGHT - MARGIN.bottom})`);

		// event delegation for tooltips on circles group
		circlesGroup.on('mouseover', handleCircleMouseOver).on('mouseout', handleCircleMouseOut);

		// initial scale using optimal view calculation
		const filteredEvents = getFilteredEvents();
		const optimalView = calculateOptimalView(filteredEvents);

		xScale = d3
			.scaleTime()
			.domain([optimalView.domainStart, optimalView.domainEnd])
			.range([0, width]);

		setupZoom();

		// theme observer
		if (typeof MutationObserver !== 'undefined') {
			themeObserver = new MutationObserver(() => {
				checkTheme();
				updateTimelineColors();
			});
			themeObserver.observe(document.documentElement, {
				attributes: true,
				attributeFilter: ['class']
			});
		}

		// resize observer
		if (typeof ResizeObserver !== 'undefined') {
			resizeObserver = new ResizeObserver(handleResize);
			resizeObserver.observe(container);
		}

		initialized = true;

		updateAxesImmediate(xScale);
		updateCenterDate(xScale);

		if (!isGhost) {
			updateTimeline();
		} else {
			handleGhost();
		}
	}

	function handleCircleMouseOver(event) {
		const target = event.target;
		if (target.tagName !== 'circle') return;

		const d = d3.select(target).datum();
		if (!d?.eventName) return;

		if (tooltipHideId) {
			clearTimeout(tooltipHideId);
			tooltipHideId = null;
		}

		const [x, y] = d3.pointer(event, container);
		const tooltipWidth = 250;
		const leftPosition = x + 10;
		const rightEdge = leftPosition + tooltipWidth;
		const adjustedLeft = rightEdge > container.offsetWidth ? x - tooltipWidth - 10 : leftPosition;

		tooltipX = Math.max(10, adjustedLeft);
		tooltipY = Math.max(10, y - 10);
		tooltipEvent = d;
		tooltipVisible = true;
	}

	function handleCircleMouseOut() {
		if (tooltipHideId) clearTimeout(tooltipHideId);
		tooltipHideId = setTimeout(() => {
			tooltipVisible = false;
			tooltipEvent = null;
		}, TOOLTIP_HIDE_DELAY);
	}

	function resetZoom() {
		if (!zoom || !svg || !xScale) return;

		const filteredEvents = getFilteredEvents();
		const optimalView = calculateOptimalView(filteredEvents);

		// update scale domain
		xScale.domain([optimalView.domainStart, optimalView.domainEnd]);

		const optimalScale = calculateOptimalScale(optimalView.domainStart, optimalView.domainEnd);

		// calculate translate to center on the desired point
		const domainSpan = optimalView.domainEnd.getTime() - optimalView.domainStart.getTime();
		const centerRatio = (optimalView.centerTs - optimalView.domainStart.getTime()) / domainSpan;
		const centerX = centerRatio * width;
		const targetCenterX = width / 2;
		const translateX = targetCenterX - centerX * optimalScale;

		const resetTransform = d3.zoomIdentity.translate(translateX, 0).scale(optimalScale);

		d3.select(svg).transition().duration(750).call(zoom.transform, resetTransform);
	}

	function handleClickOutside(event) {
		if (showFilterDropdown && !event.target.closest('.filter-dropdown')) {
			showFilterDropdown = false;
		}
	}

	function invalidateCacheAndUpdate() {
		filteredEventsCache = null;
		if (!isGhost && initialized) {
			updateTimeline();
		}
	}

	// computed tooltip values
	$: tooltipColor = tooltipEvent ? getEventColor(tooltipEvent.eventName) : DEFAULT_COLOR;
	$: tooltipIcon = tooltipEvent ? getEventIcon(tooltipEvent.eventName) : DEFAULT_ICON;
	$: tooltipInfo = tooltipEvent ? toEvent(tooltipEvent.eventName) : null;
	$: tooltipDate = tooltipEvent ? new Date(tooltipEvent.createdAt) : null;
	$: tooltipFormattedDate = tooltipDate?.toLocaleDateString() || '';
	$: tooltipFormattedTime =
		tooltipDate?.toLocaleTimeString([], {
			hour12: !use24Hour,
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		}) || '';

	// lifecycle
	onMount(() => {
		checkTheme();
	});

	onDestroy(() => {
		if (themeObserver) themeObserver.disconnect();
		if (resizeObserver) resizeObserver.disconnect();
		if (rafId) cancelAnimationFrame(rafId);
		if (axisUpdateId) clearTimeout(axisUpdateId);
		if (tooltipHideId) clearTimeout(tooltipHideId);
		if (resizeTimeoutId) clearTimeout(resizeTimeoutId);
		if (currentTimeIntervalId) clearInterval(currentTimeIntervalId);
		window.removeEventListener('click', handleClickOutside);
	});

	// reactive: process events when they change
	// use content-based hash to detect changes (not reference equality)
	// include all event ids to detect deletions from any position
	$: eventsHash = events?.map((e) => e.id).join(',') || '';
	$: if (eventsHash !== lastEventsHash) {
		lastEventsHash = eventsHash;
		processedEvents = processEvents(events);
		filteredEventsCache = null;
		circleNodes = [];
		circleData = [];
		if (initialized && !isGhost) {
			updateTimeline();
		}
	}

	// reactive: initialize timeline
	$: if (container && svg && !initialized) {
		initializeTimeline();
		if (currentTimeIntervalId) clearInterval(currentTimeIntervalId);
		currentTimeIntervalId = setInterval(() => {
			if (initialized && xScale) {
				updateCurrentTimeIndicatorImmediate();
			}
		}, CURRENT_TIME_UPDATE_MS);
	}

	// reactive: ghost mode
	$: if (initialized && timelineGroup) {
		handleGhost();
	}

	// reactive: time format changed
	$: if (initialized && xScale && use24Hour !== undefined) {
		const scale = currentTransform ? currentTransform.rescaleX(xScale) : xScale;
		updateAxesImmediate(scale);
	}

	// reactive: filter dropdown click outside
	$: if (showFilterDropdown) {
		window.addEventListener('click', handleClickOutside);
	} else {
		window.removeEventListener('click', handleClickOutside);
	}
</script>

<div class="relative cursor-move" bind:this={container}>
	<div class="transition-opacity duration-300" class:animate-pulse={isGhost}>
		<svg
			bind:this={svg}
			width="100%"
			height={HEIGHT}
			class="w-full bg-white dark:bg-gray-900/80 border border-gray-200 dark:border-gray-700/60 rounded-lg"
		></svg>

		<!-- svelte-native tooltip -->
		{#if tooltipVisible && tooltipEvent}
			<div
				class="absolute z-50 bg-white dark:bg-gray-900 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700/60 max-w-xs overflow-hidden pointer-events-none"
				style="left: {tooltipX}px; top: {tooltipY}px; will-change: transform;"
			>
				<div class="border-t-4" style="border-top-color: {tooltipColor}">
					<div class="px-4 py-3 text-gray-800 dark:text-gray-200">
						<div class="flex items-center space-x-2">
							<div class="flex-shrink-0 w-5 h-5" style="color: {tooltipColor}">
								{@html tooltipIcon}
							</div>
							<div class="flex-1 min-w-0">
								<h3 class="text-sm font-bold truncate">{tooltipInfo?.name || 'Unknown'}</h3>
							</div>
						</div>
					</div>
				</div>

				<div class="px-4 py-3 space-y-3">
					{#if tooltipEvent.recipient?.email}
						<div class="flex items-center space-x-2">
							<div class="flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400">
								<svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
									<path
										stroke-linecap="round"
										stroke-linejoin="round"
										stroke-width="2"
										d="M16 12a4 4 0 10-8 0 4 4 0 008 0zm0 0v1.5a2.5 2.5 0 005 0V12a9 9 0 10-9 9m4.5-1.206a8.959 8.959 0 01-4.5 1.207"
									/>
								</svg>
							</div>
							<span class="text-sm text-gray-700 dark:text-gray-300 truncate"
								>{tooltipEvent.recipient.email}</span
							>
						</div>
					{/if}

					<div class="flex items-center space-x-2">
						<div class="flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400">
							<svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
								/>
							</svg>
						</div>
						<div class="text-sm text-gray-700 dark:text-gray-300">
							{tooltipFormattedDate.split(',')[0]}
							{tooltipFormattedTime}
						</div>
					</div>

					{#if tooltipEvent.ip || tooltipEvent.userAgent}
						<div class="mt-3 pt-3 border-t border-gray-200 dark:border-gray-600 space-y-2">
							{#if tooltipEvent.ip}
								<div class="flex items-center space-x-2">
									<div class="flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400">
										<svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path
												stroke-linecap="round"
												stroke-linejoin="round"
												stroke-width="2"
												d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"
											/>
										</svg>
									</div>
									<span class="text-xs text-gray-600 dark:text-gray-400 truncate"
										>{tooltipEvent.ip}</span
									>
								</div>
							{/if}
							{#if tooltipEvent.userAgent}
								<div class="flex items-start space-x-2">
									<div class="flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400 mt-0.5">
										<svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
											<path
												stroke-linecap="round"
												stroke-linejoin="round"
												stroke-width="2"
												d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
											/>
										</svg>
									</div>
									<span class="text-xs text-gray-600 dark:text-gray-400 break-words">
										{tooltipEvent.userAgent.length > 80
											? tooltipEvent.userAgent.substring(0, 80) + '...'
											: tooltipEvent.userAgent}
									</span>
								</div>
							{/if}
						</div>
					{/if}

					{#if tooltipEvent.data}
						<div class="mt-3 pt-3 border-t border-gray-200 dark:border-gray-600">
							<div class="flex items-start space-x-2">
								<div class="flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400 mt-0.5">
									<svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
										<path
											stroke-linecap="round"
											stroke-linejoin="round"
											stroke-width="2"
											d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
										/>
									</svg>
								</div>
								<div class="text-xs text-gray-600 dark:text-gray-400 break-words">
									{tooltipEvent.data.length > 100
										? tooltipEvent.data.substring(0, 100) + '...'
										: tooltipEvent.data}
								</div>
							</div>
						</div>
					{/if}
				</div>
			</div>
		{/if}

		{#if !isGhost}
			<div class="absolute top-2 right-2 flex gap-2">
				<div class="relative filter-dropdown">
					<button
						on:click|stopPropagation={() => (showFilterDropdown = !showFilterDropdown)}
						class="inline-flex items-center px-2 py-1 border border-slate-300 dark:border-gray-700/60 rounded-md text-xs font-medium text-slate-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 hover:bg-gray-100 dark:hover:bg-gray-700/60 focus:outline-none focus:border-slate-400 dark:focus:border-highlight-blue/80 transition-colors duration-200"
					>
						<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z"
							></path>
						</svg>
					</button>
					{#if showFilterDropdown}
						<div
							class="absolute right-0 mt-2 w-80 bg-white dark:bg-gray-900/80 rounded-md shadow-lg border border-gray-200 dark:border-gray-700/60 z-50"
						>
							<div class="p-3">
								<h3 class="text-sm font-medium text-gray-700 dark:text-gray-200 mb-3">
									Filter Events
								</h3>

								<div class="space-y-3">
									<div>
										<h4 class="text-xs font-medium text-gray-600 dark:text-gray-300 mb-2">
											Recipient Events
										</h4>
										<div class="space-y-1 pl-2">
											{#each [['campaign_recipient_scheduled', 'Scheduled'], ['campaign_recipient_cancelled', 'Cancelled'], ['campaign_recipient_message_sent', 'Message Sent'], ['campaign_recipient_message_failed', 'Message Failed'], ['campaign_recipient_message_read', 'Message Read'], ['campaign_recipient_before_page_visited', 'Before Page Visited'], ['campaign_recipient_page_visited', 'Page Visited'], ['campaign_recipient_after_page_visited', 'After Page Visited'], ['campaign_recipient_deny_page_visited', 'Deny Page Visited'], ['campaign_recipient_submitted_data', 'Data Submitted'], ['campaign_recipient_reported', 'Reported']] as [key, label]}
												<label class="flex items-center text-xs">
													<input
														type="checkbox"
														bind:checked={eventFilters[key]}
														on:change={invalidateCacheAndUpdate}
														class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
													/>
													<span class="text-gray-600 dark:text-gray-300">{label}</span>
												</label>
											{/each}
										</div>
									</div>
								</div>

								<div class="mt-3 pt-3 border-t border-gray-200 dark:border-gray-600 flex gap-2">
									<button
										on:click={() => {
											Object.keys(eventFilters).forEach((k) => (eventFilters[k] = true));
											invalidateCacheAndUpdate();
										}}
										class="flex-1 px-2 py-1 text-xs bg-cta-blue dark:bg-highlight-blue/80 text-white rounded hover:bg-blue-500 dark:hover:bg-highlight-blue focus:outline-none transition-colors duration-200"
									>
										Select All
									</button>
									<button
										on:click={() => {
											Object.keys(eventFilters).forEach((k) => (eventFilters[k] = false));
											invalidateCacheAndUpdate();
										}}
										class="flex-1 px-2 py-1 text-xs bg-slate-500 dark:bg-gray-700 text-white rounded hover:bg-slate-600 dark:hover:bg-gray-600 focus:outline-none transition-colors duration-200"
									>
										Clear All
									</button>
									<button
										on:click={() => (showFilterDropdown = false)}
										class="px-2 py-1 text-xs text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200"
									>
										✕
									</button>
								</div>
							</div>
						</div>
					{/if}
				</div>
				<button
					on:click={resetZoom}
					class="inline-flex items-center px-2 py-1 border border-slate-300 dark:border-gray-700/60 rounded-md text-xs font-medium text-slate-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 hover:bg-gray-100 dark:hover:bg-gray-700/60 focus:outline-none focus:border-slate-400 dark:focus:border-highlight-blue/80 transition-colors duration-200"
				>
					Reset Zoom
				</button>
				<button
					on:click={() => (use24Hour = !use24Hour)}
					class="inline-flex items-center px-2 py-1 border border-slate-300 dark:border-gray-700/60 rounded-md text-xs font-medium text-slate-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 hover:bg-gray-100 dark:hover:bg-gray-700/60 focus:outline-none focus:border-slate-400 dark:focus:border-highlight-blue/80 transition-colors duration-200"
				>
					{use24Hour ? '12H' : '24H'}
				</button>
			</div>
			<div
				class="absolute top-2 left-2 text-xs text-slate-600 dark:text-gray-400 bg-grayblue-light dark:bg-gray-900/60 px-2 py-1 rounded border border-slate-300 dark:border-gray-700/60"
			>
				{currentCenterDate.toLocaleString()}
			</div>
		{/if}
	</div>
</div>

<style>
	:global(.x-axis text) {
		fill: #6b7280;
		font-size: 11px;
	}

	:global(.dark .x-axis text) {
		fill: #9ca3af;
	}

	:global(.x-axis line) {
		stroke: #e5e7eb;
	}

	:global(.dark .x-axis line) {
		stroke: #374151;
	}

	:global(.x-axis path) {
		stroke: #e5e7eb;
	}

	:global(.dark .x-axis path) {
		stroke: #374151;
	}

	:global(.timeline-line) {
		stroke: #e5e7eb;
	}

	:global(.dark .timeline-line) {
		stroke: #374151;
	}

	:global(.ghost-dot) {
		animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
	}

	:global(.center-line) {
		pointer-events: none;
	}

	:global(.current-time-indicator) {
		pointer-events: none;
		filter: drop-shadow(0 0 2px rgba(68, 94, 204, 0.5));
	}

	:global(.dark .current-time-indicator) {
		filter: drop-shadow(0 0 2px rgba(104, 135, 234, 0.6));
	}

	:global(.current-time-window) {
		pointer-events: none;
	}

	:global(.circles-group circle) {
		will-change: cx;
		shape-rendering: geometricPrecision;
	}

	:global(.circles-group) {
		contain: layout style;
	}
</style>
