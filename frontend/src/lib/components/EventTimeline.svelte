<script>
	import { onMount, onDestroy, createEventDispatcher } from 'svelte';
	import * as d3 from 'd3';
	import { toEvent } from '$lib/utils/events';

	export let events = [];
	export let isGhost = false;

	// theme management
	let isDarkMode = false;

	const checkTheme = () => {
		isDarkMode = document.documentElement.classList.contains('dark');
	};

	// dom references
	let svg;
	let tooltip;
	let container;
	let timelineGroup;

	// layout properties
	let width = 800;
	let height = 200;
	let margin = { top: 20, right: 20, bottom: 30, left: 20 };

	// timeline state
	let currentCenterDate = new Date();
	let zoom;
	let currentTransform;
	let xScale;
	let use24Hour = true;
	let initialized = false;
	let themeObserver;
	let showFilterDropdown = false;

	// event filtering
	let eventFilters = {
		campaign_recipient_scheduled: true,
		campaign_recipient_cancelled: true,
		campaign_recipient_message_sent: true,
		campaign_recipient_message_failed: true,
		campaign_recipient_message_read: true,
		campaign_recipient_before_page_visited: true,
		campaign_recipient_page_visited: true,
		campaign_recipient_after_page_visited: true,
		campaign_recipient_submitted_data: true,
		campaign_recipient_reported: true
	};
	let filterUpdateCounter = 0;

	// performance optimizations
	const MAX_ZOOM_IN = 1000; // 1 second in milliseconds
	const MIN_ZOOM_OUT_PADDING = 0.1; // 10% padding on each side when fully zoomed out

	// throttling for zoom operations
	let zoomThrottleTimeout;
	let axisUpdateTimeout;
	let filteredEventsCache = null;
	let lastFilterHash = '';

	// virtualization for large datasets
	const MAX_VISIBLE_EVENTS = 1000;
	let visibleEvents = [];

	// debounced container resize
	let resizeTimeout;
	let resizeObserver;

	// update timeline colors when theme changes
	function updateTimelineColors() {
		if (!timelineGroup) return;

		requestAnimationFrame(() => {
			timelineGroup.selectAll('circle').attr('stroke', isDarkMode ? '#374151' : '#fff');
			// update center line color
			timelineGroup.select('.center-line').attr('stroke', isDarkMode ? '#6b7280' : '#d1d5db');
		});
	}

	// optimized event filtering with caching
	function getFilteredEvents() {
		if (!events || events.length === 0) {
			filteredEventsCache = [];
			return filteredEventsCache;
		}

		// create hash of current filter state
		const filterHash = JSON.stringify(eventFilters);
		if (filterHash === lastFilterHash && filteredEventsCache) {
			return filteredEventsCache;
		}

		// check if any recipient filters are enabled
		const hasAnyRecipientFiltersEnabled = Object.values(eventFilters).some(
			(filter) => filter === true
		);

		// always show campaign events, filter only recipient events
		filteredEventsCache = events.filter((event) => {
			// always show campaign events
			if (
				event.eventName.startsWith('campaign_') &&
				!event.eventName.startsWith('campaign_recipient_')
			) {
				return true;
			}
			// if no recipient filters are enabled, hide all recipient events
			if (!hasAnyRecipientFiltersEnabled) {
				return false;
			}
			// filter recipient events based on eventFilters
			return eventFilters[event.eventName] === true;
		});

		lastFilterHash = filterHash;
		return filteredEventsCache;
	}

	// get visible events for current zoom level (virtualization)
	function getVisibleEvents(scale) {
		const filteredEvents = getFilteredEvents();

		if (!scale || filteredEvents.length <= MAX_VISIBLE_EVENTS) {
			return filteredEvents;
		}

		const domain = scale.domain();
		const visibleRange = [domain[0].getTime(), domain[1].getTime()];

		// add some padding to include events just outside visible area
		const padding = (visibleRange[1] - visibleRange[0]) * 0.1;
		const extendedRange = [visibleRange[0] - padding, visibleRange[1] + padding];

		// filter events within visible range
		const eventsInRange = filteredEvents.filter((event) => {
			const timestamp = new Date(event.createdAt).getTime();
			return timestamp >= extendedRange[0] && timestamp <= extendedRange[1];
		});

		// if still too many events, sample them
		if (eventsInRange.length > MAX_VISIBLE_EVENTS) {
			const step = Math.ceil(eventsInRange.length / MAX_VISIBLE_EVENTS);
			return eventsInRange.filter((_, index) => index % step === 0);
		}

		return eventsInRange;
	}

	// debounced container resize handler
	function handleResize() {
		if (resizeTimeout) clearTimeout(resizeTimeout);
		resizeTimeout = setTimeout(() => {
			if (container && initialized) {
				const newWidth = container.offsetWidth - margin.left - margin.right;
				if (newWidth > 0 && newWidth !== width) {
					width = newWidth;
					if (xScale) {
						xScale.range([0, width]);
						updateTimelineOptimized();
					}
				}
			}
		}, 100);
	}

	function initializeTimeline() {
		if (!svg || !container) {
			return;
		}

		const containerWidth = container.offsetWidth;
		width = containerWidth > 0 ? containerWidth - margin.left - margin.right : 800;
		height = 200;

		// clear any existing content
		d3.select(svg).selectAll('*').remove();

		// create the main timeline group
		timelineGroup = d3
			.select(svg)
			.append('g')
			.attr('transform', `translate(${margin.left}, ${margin.top})`);

		// add the timeline line
		timelineGroup
			.append('line')
			.attr('class', 'timeline-line')
			.attr('x1', 0)
			.attr('y1', (height - margin.top - margin.bottom) / 2)
			.attr('x2', width)
			.attr('y2', (height - margin.top - margin.bottom) / 2)
			.attr('stroke', isDarkMode ? '#374151' : '#e5e7eb')
			.attr('stroke-width', 2);

		// add center reference line
		timelineGroup
			.append('line')
			.attr('class', 'center-line')
			.attr('x1', width / 2)
			.attr('y1', 0)
			.attr('x2', width / 2)
			.attr('y2', height - margin.top - margin.bottom)
			.attr('stroke', isDarkMode ? '#6b7280' : '#d1d5db')
			.attr('stroke-width', 1)
			.attr('opacity', 0.4);

		// add x-axis group
		d3.select(svg)
			.append('g')
			.attr('class', 'x-axis')
			.attr('transform', `translate(${margin.left}, ${height - margin.bottom})`);

		// create initial scale (always create one)
		const now = new Date();
		const defaultStart = new Date(now.getTime() - 24 * 60 * 60 * 1000);
		xScale = d3.scaleTime().domain([defaultStart, now]).range([0, width]);

		// setup zoom behavior
		setupZoomOptimized();

		// setup theme observer
		if (!themeObserver && typeof MutationObserver !== 'undefined') {
			themeObserver = new MutationObserver(() => {
				checkTheme();
				updateTimelineColors();
			});
			themeObserver.observe(document.documentElement, {
				attributes: true,
				attributeFilter: ['class']
			});
		}

		// setup resize observer
		if (typeof ResizeObserver !== 'undefined') {
			resizeObserver = new ResizeObserver(handleResize);
			resizeObserver.observe(container);
		}

		initialized = true;

		// show initial axes
		updateAxesImmediate(xScale);
		updateCenterDate(xScale);

		// initialize with current events after everything is set up
		if (!isGhost) {
			updateTimelineOptimized();
		} else {
			handleGhost();
		}
	}

	// optimized zoom setup with better performance
	function setupZoomOptimized() {
		zoom = d3
			.zoom()
			.scaleExtent([0.1, 1000000])
			.extent([
				[0, 0],
				[width, height]
			])
			.filter((event) => {
				// performance: check zoom limits only for wheel events
				if (event.type === 'wheel') {
					if (!currentTransform || !xScale) return true;

					const newX = currentTransform.rescaleX(xScale);
					const visibleDomain = newX.domain();
					const visibleTimeSpan = visibleDomain[1] - visibleDomain[0];

					// block zoom in when already at max zoom
					if (visibleTimeSpan <= MAX_ZOOM_IN && event.deltaY < 0) {
						return false;
					}
				}
				return true;
			})
			.on('zoom', handleZoomThrottled);

		d3.select(svg).call(zoom);
	}

	// throttled zoom handler for better performance
	function handleZoomThrottled(event) {
		if (!xScale) return;

		// store the current transform immediately for responsive feel
		currentTransform = event.transform;
		const newX = event.transform.rescaleX(xScale);

		// immediately update circle positions for smooth panning
		timelineGroup.selectAll('circle').attr('cx', (d) => newX(new Date(d.createdAt)));

		// throttle expensive operations
		if (zoomThrottleTimeout) clearTimeout(zoomThrottleTimeout);
		zoomThrottleTimeout = setTimeout(() => {
			updateAxesOptimized(newX);
			updateVisibleEventsOptimized(newX);
		}, 16); // ~60fps
	}

	// optimized timeline update with minimal DOM manipulation
	function updateTimelineOptimized() {
		if (!initialized || !svg || !timelineGroup || !xScale) {
			return;
		}

		const filteredEvents = getFilteredEvents();

		// handle empty state
		if (filteredEvents.length === 0) {
			timelineGroup.selectAll('circle').remove();
			// keep current scale but update center date
			updateCenterDate(xScale);
			return;
		}

		// calculate time domain
		const timestamps = filteredEvents.map((d) => new Date(d.createdAt));
		const timeRange = d3.extent(timestamps);
		const timeSpan = timeRange[1] - timeRange[0];
		const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);

		const domainStart = new Date(timeRange[0].getTime() - padding);
		const domainEnd = new Date(timeRange[1].getTime() + padding);

		xScale = d3.scaleTime().domain([domainStart, domainEnd]).range([0, width]);

		// update visible events
		updateVisibleEventsOptimized(xScale);
		updateAxesImmediate(xScale);
		updateCenterDate(xScale);

		// apply current transform if exists
		if (currentTransform) {
			const newX = currentTransform.rescaleX(xScale);
			updateAxesImmediate(newX);
			timelineGroup.selectAll('circle').attr('cx', (d) => newX(new Date(d.createdAt)));
		} else {
			// set initial zoom
			const optimalScale = calculateOptimalScale(domainStart, domainEnd);
			const initialTranslate = (width - width * optimalScale) / 2;
			currentTransform = d3.zoomIdentity.translate(initialTranslate, 0).scale(optimalScale);
			d3.select(svg).call(zoom.transform, currentTransform);
		}
	}

	// optimized visible events update with virtualization
	function updateVisibleEventsOptimized(scale) {
		const currentScale = scale || xScale;
		if (!currentScale) return;

		visibleEvents = getVisibleEvents(currentScale);

		// use d3's data join for efficient updates
		const circles = timelineGroup
			.selectAll('circle')
			.data(visibleEvents, (d) => d.id || `${d.createdAt}-${d.eventName}`);

		// remove events no longer visible
		circles.exit().remove();

		// add new visible events
		circles
			.enter()
			.append('circle')
			.attr('r', 6)
			.attr('stroke', isDarkMode ? '#374151' : '#fff')
			.attr('stroke-width', 1)
			.attr('cy', (height - margin.top - margin.bottom) / 2)
			.merge(circles)
			.attr('cx', (d) => currentScale(new Date(d.createdAt)))
			.attr('fill', (d) => getEventColor(d.eventName))
			.on('mouseover', showTooltipOptimized)
			.on('mouseout', hideTooltipOptimized);
	}

	function calculateOptimalScale(start, end) {
		if (!start || !end) return 0.9;

		const timeSpan = end - start;
		const targetTimeSpan = timeSpan * (1 + MIN_ZOOM_OUT_PADDING * 2);
		const currentScale = width / targetTimeSpan;

		return Math.min(Math.max(currentScale, 0.7), 0.95);
	}

	// optimized axis updates with throttling
	function updateAxesOptimized(scale) {
		if (axisUpdateTimeout) clearTimeout(axisUpdateTimeout);

		axisUpdateTimeout = setTimeout(() => {
			updateAxesImmediate(scale);
		}, 32); // ~30fps for axis updates
	}

	function updateAxesImmediate(scale) {
		if (!scale || !scale.domain || typeof scale.domain !== 'function') {
			console.error('Invalid scale provided to updateAxes');
			return;
		}

		const domain = scale.domain();
		const timeSpan = domain[1] - domain[0];
		const minTickSpacing = 80;
		const maxTicks = Math.floor(width / minTickSpacing);

		const xAxis = d3.select(svg).select('.x-axis');
		if (!xAxis.empty()) {
			let tickFunction;
			let tickFormat;

			// optimize tick calculation
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
		const currentScale = scale || xScale;
		if (!currentScale) return;

		try {
			const domain = currentScale.domain();
			const centerTime = new Date((domain[0].getTime() + domain[1].getTime()) / 2);

			// only update if significantly different to avoid unnecessary reactivity
			if (Math.abs(centerTime.getTime() - currentCenterDate.getTime()) > 1000) {
				currentCenterDate = centerTime;
			}
		} catch (e) {
			console.error('Error updating center date:', e);
		}
	}

	function handleGhost() {
		if (!initialized || !timelineGroup) return;

		if (isGhost) {
			// hide event dots and show ghost dots when in ghost mode
			timelineGroup.selectAll('circle:not(.ghost-dot)').style('display', 'none');

			// create ghost dots if they don't exist
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
					.attr('cy', (height - margin.top - margin.bottom) / 2)
					.attr('fill', 'none')
					.attr('stroke', isDarkMode ? '#6b7280' : '#9ca3af')
					.attr('stroke-width', 1);
			}
		} else {
			// remove ghost dots and show real events
			timelineGroup.selectAll('.ghost-dot').remove();
			timelineGroup.selectAll('circle:not(.ghost-dot)').style('display', null);
			// update timeline with real data
			updateTimelineOptimized();
		}
	}

	// optimized tooltip with reduced DOM manipulation
	let tooltipTimeout;
	function showTooltipOptimized(event, d) {
		if (!tooltip || !d || !d.eventName) return;

		// clear any pending hide
		if (tooltipTimeout) clearTimeout(tooltipTimeout);

		// use requestAnimationFrame for smooth tooltip positioning
		requestAnimationFrame(() => {
			const [x, y] = d3.pointer(event, container);
			tooltip.style.display = 'block';

			const tooltipWidth = 250;
			const containerRect = container.getBoundingClientRect();
			const leftPosition = x + 10;
			const rightEdge = leftPosition + tooltipWidth;
			const adjustedLeft = rightEdge > container.offsetWidth ? x - tooltipWidth - 10 : leftPosition;

			tooltip.style.left = `${Math.max(10, adjustedLeft)}px`;
			tooltip.style.top = `${Math.max(10, y - 10)}px`;

			updateTooltipContent(d);
		});
	}

	function hideTooltipOptimized() {
		if (tooltipTimeout) clearTimeout(tooltipTimeout);
		tooltipTimeout = setTimeout(() => {
			if (tooltip) {
				tooltip.style.display = 'none';
			}
		}, 100); // small delay to prevent flickering
	}

	// separate tooltip content update for better performance
	function updateTooltipContent(d) {
		if (!tooltip) return;

		try {
			const eventColor = getEventColor(d.eventName);
			const eventInfo = toEvent(d.eventName);
			const eventIcon = getEventIcon(d.eventName);
			const dateObj = new Date(d.createdAt);
			const formattedDate = dateObj.toLocaleDateString();
			const formattedTime = dateObj.toLocaleTimeString([], {
				hour12: !use24Hour,
				hour: '2-digit',
				minute: '2-digit',
				second: '2-digit'
			});

			tooltip.innerHTML = '';

			// create main container
			const container = document.createElement('div');
			container.className = 'overflow-hidden';

			// create header with colored border
			const header = document.createElement('div');
			header.className = 'border-t-4';
			header.style.borderTopColor = eventColor;

			const headerContent = document.createElement('div');
			headerContent.className = 'px-4 py-3 text-gray-800 dark:text-gray-200';

			const headerFlex = document.createElement('div');
			headerFlex.className = 'flex items-center space-x-2';

			// create icon container
			const iconContainer = document.createElement('div');
			iconContainer.className = 'flex-shrink-0 w-5 h-5';
			iconContainer.style.color = eventColor;
			iconContainer.innerHTML = eventIcon; // eventIcon is safe SVG from getEventIcon function

			const textContainer = document.createElement('div');
			textContainer.className = 'flex-1 min-w-0';

			const title = document.createElement('h3');
			title.className = 'text-sm font-bold truncate';
			title.textContent = eventInfo.name;

			textContainer.appendChild(title);
			headerFlex.appendChild(iconContainer);
			headerFlex.appendChild(textContainer);
			headerContent.appendChild(headerFlex);
			header.appendChild(headerContent);
			container.appendChild(header);

			// create body content
			const body = document.createElement('div');
			body.className = 'px-4 py-3 space-y-3';

			// email section
			if (d.recipient?.email) {
				const emailSection = document.createElement('div');
				emailSection.className = 'flex items-center space-x-2';

				const emailIcon = document.createElement('div');
				emailIcon.className = 'flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400';
				emailIcon.innerHTML =
					'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 12a4 4 0 10-8 0 4 4 0 008 0zm0 0v1.5a2.5 2.5 0 005 0V12a9 9 0 10-9 9m4.5-1.206a8.959 8.959 0 01-4.5 1.207"/></svg>';

				const emailText = document.createElement('span');
				emailText.className = 'text-sm text-gray-700 dark:text-gray-300 truncate';
				emailText.textContent = d.recipient.email;

				emailSection.appendChild(emailIcon);
				emailSection.appendChild(emailText);
				body.appendChild(emailSection);
			}

			// time section
			const timeSection = document.createElement('div');
			timeSection.className = 'flex items-center space-x-2';

			const timeIcon = document.createElement('div');
			timeIcon.className = 'flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400';
			timeIcon.innerHTML =
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>';

			const timeText = document.createElement('div');
			timeText.className = 'text-sm text-gray-700 dark:text-gray-300';
			timeText.textContent = `${formattedDate.split(',')[0]} ${formattedTime}`;

			timeSection.appendChild(timeIcon);
			timeSection.appendChild(timeText);
			body.appendChild(timeSection);

			// ip/useragent section
			if (d.ip || d.userAgent) {
				const techSection = document.createElement('div');
				techSection.className = 'mt-3 pt-3 border-t border-gray-200 dark:border-gray-600 space-y-2';

				if (d.ip) {
					const ipSection = document.createElement('div');
					ipSection.className = 'flex items-center space-x-2';

					const ipIcon = document.createElement('div');
					ipIcon.className = 'flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400';
					ipIcon.innerHTML =
						'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"/></svg>';

					const ipText = document.createElement('span');
					ipText.className = 'text-xs text-gray-600 dark:text-gray-400 truncate';
					ipText.textContent = d.ip;

					ipSection.appendChild(ipIcon);
					ipSection.appendChild(ipText);
					techSection.appendChild(ipSection);
				}

				if (d.userAgent) {
					const uaSection = document.createElement('div');
					uaSection.className = 'flex items-start space-x-2';

					const uaIcon = document.createElement('div');
					uaIcon.className = 'flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400 mt-0.5';
					uaIcon.innerHTML =
						'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>';

					const uaText = document.createElement('span');
					uaText.className = 'text-xs text-gray-600 dark:text-gray-400 break-words';
					uaText.textContent =
						d.userAgent.length > 80 ? d.userAgent.substring(0, 80) + '...' : d.userAgent;

					uaSection.appendChild(uaIcon);
					uaSection.appendChild(uaText);
					techSection.appendChild(uaSection);
				}

				body.appendChild(techSection);
			}

			// data section
			if (d.data) {
				const dataSection = document.createElement('div');
				dataSection.className = 'mt-3 pt-3 border-t border-gray-200 dark:border-gray-600';

				const dataFlex = document.createElement('div');
				dataFlex.className = 'flex items-start space-x-2';

				const dataIcon = document.createElement('div');
				dataIcon.className = 'flex-shrink-0 w-4 h-4 text-gray-500 dark:text-gray-400 mt-0.5';
				dataIcon.innerHTML =
					'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>';

				const dataText = document.createElement('div');
				dataText.className = 'text-xs text-gray-600 dark:text-gray-400 break-words';
				dataText.textContent = d.data.length > 100 ? d.data.substring(0, 100) + '...' : d.data;

				dataFlex.appendChild(dataIcon);
				dataFlex.appendChild(dataText);
				dataSection.appendChild(dataFlex);
				body.appendChild(dataSection);
			}

			container.appendChild(body);
			tooltip.appendChild(container);
		} catch (e) {
			console.error('Error updating tooltip content:', e);
			hideTooltipOptimized();
		}
	}

	function resetZoom() {
		if (!zoom || !svg || !xScale) return;

		const filteredEvents = getFilteredEvents();
		if (filteredEvents.length === 0) return;

		const timestamps = filteredEvents.map((d) => new Date(d.createdAt));
		const timeRange = d3.extent(timestamps);
		const timeSpan = timeRange[1] - timeRange[0];
		const padding = Math.max(timeSpan * MIN_ZOOM_OUT_PADDING, 60000);

		const domainStart = new Date(timeRange[0].getTime() - padding);
		const domainEnd = new Date(timeRange[1].getTime() + padding);

		const optimalScale = calculateOptimalScale(domainStart, domainEnd);
		const initialTranslate = (width - width * optimalScale) / 2;

		const resetTransform = d3.zoomIdentity.translate(initialTranslate, 0).scale(optimalScale);

		d3.select(svg).transition().duration(750).call(zoom.transform, resetTransform);
	}

	function getEventColor(eventName) {
		const eventColors = {
			campaign_scheduled: '#62aded',
			campaign_active: '#5557f6',
			campaign_self_managed: '#9622fc',
			campaign_closed: '#9f9f9f',
			campaign_recipient_scheduled: '#4e68d8',
			campaign_recipient_cancelled: '#161692',
			campaign_recipient_message_sent: '#94cae6',
			campaign_recipient_message_failed: '#f2bb58',
			campaign_recipient_message_read: '#4cb5b5',
			campaign_recipient_before_page_visited: '#eea5fa',
			campaign_recipient_page_visited: '#f96dcf',
			campaign_recipient_after_page_visited: '#f6287b',
			campaign_recipient_submitted_data: '#f42e41',
			campaign_recipient_reported: '#2c3e50'
		};
		return eventColors[eventName] || '#6b7280';
	}

	function getEventIcon(eventName) {
		const eventIcons = {
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
			campaign_recipient_before_page_visited:
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"/></svg>',
			campaign_recipient_page_visited:
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9"/></svg>',
			campaign_recipient_after_page_visited:
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/></svg>',
			campaign_recipient_submitted_data:
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
			campaign_recipient_reported:
				'<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.072 16.5c-.77.833.192 2.5 1.732 2.5z"/></svg>'
		};
		return (
			eventIcons[eventName] ||
			'<svg fill="currentColor" viewBox="0 0 20 20"><circle cx="10" cy="10" r="3"></circle></svg>'
		);
	}

	function getEventDescription(eventName) {
		const descriptions = {
			campaign_scheduled: 'Campaign scheduled for future execution',
			campaign_active: 'Campaign is currently running',
			campaign_self_managed: 'Campaign is self-managed',
			campaign_closed: 'Campaign has been completed',
			campaign_recipient_scheduled: 'Recipient scheduled for campaign',
			campaign_recipient_cancelled: 'Campaign cancelled for recipient',
			campaign_recipient_message_sent: 'Email successfully delivered',
			campaign_recipient_message_failed: 'Email delivery failed',
			campaign_recipient_message_read: 'Recipient opened the email',
			campaign_recipient_before_page_visited: 'Recipient browsing before target page',
			campaign_recipient_page_visited: 'Recipient visited the target page',
			campaign_recipient_after_page_visited: 'Recipient continued browsing after target',
			campaign_recipient_submitted_data: 'Recipient submitted form data',
			campaign_recipient_reported: 'Email was reported as spam'
		};
		return descriptions[eventName] || 'Unknown event';
	}

	function getEventStatus(eventName) {
		if (eventName.startsWith('campaign_recipient_')) {
			return 'recipient';
		}
		return 'campaign';
	}

	function handleClickOutside(event) {
		if (showFilterDropdown && !event.target.closest('.filter-dropdown')) {
			showFilterDropdown = false;
		}
	}

	// lifecycle
	onMount(() => {
		checkTheme();
	});

	onDestroy(() => {
		if (themeObserver) {
			themeObserver.disconnect();
		}
		if (resizeObserver) {
			resizeObserver.disconnect();
		}
		if (zoomThrottleTimeout) clearTimeout(zoomThrottleTimeout);
		if (axisUpdateTimeout) clearTimeout(axisUpdateTimeout);
		if (tooltipTimeout) clearTimeout(tooltipTimeout);
		if (resizeTimeout) clearTimeout(resizeTimeout);
		window.removeEventListener('click', handleClickOutside);
	});

	// reactive statements - optimized to prevent unnecessary updates
	$: if (container && svg && !initialized) {
		initializeTimeline();
	}

	// simple width reactive update
	$: if (container) {
		const newWidth = container.offsetWidth - margin.left - margin.right;
		if (newWidth > 0 && newWidth !== width) {
			width = newWidth;
		}
	}

	// debounce timeline updates
	let timelineUpdateTimeout;
	$: if (initialized && xScale) {
		if (timelineUpdateTimeout) clearTimeout(timelineUpdateTimeout);
		timelineUpdateTimeout = setTimeout(() => {
			filteredEventsCache = null; // invalidate cache
			if (!isGhost) {
				updateTimelineOptimized();
			}
		}, 50);
	}

	// debounce filter updates
	$: if (initialized && filterUpdateCounter >= 0 && xScale) {
		if (timelineUpdateTimeout) clearTimeout(timelineUpdateTimeout);
		timelineUpdateTimeout = setTimeout(() => {
			filteredEventsCache = null; // invalidate cache
			updateTimelineOptimized();
		}, 50);
	}

	// ghost mode handling
	$: if (initialized && timelineGroup) {
		handleGhost();
	}

	// click outside handler
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
			{height}
			class="w-full bg-white dark:bg-gray-900/80 border border-gray-200 dark:border-gray-700/60 rounded-lg"
		></svg>
		<div
			bind:this={tooltip}
			class="absolute z-50 hidden bg-white dark:bg-gray-900 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700/60 max-w-xs"
		></div>
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
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_scheduled}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Scheduled</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_cancelled}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Cancelled</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_message_sent}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Message Sent</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_message_failed}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Message Failed</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_message_read}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Message Read</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_before_page_visited}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Before Page Visited</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_page_visited}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Page Visited</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_after_page_visited}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">After Page Visited</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_submitted_data}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Data Submitted</span>
											</label>
											<label class="flex items-center text-xs">
												<input
													type="checkbox"
													bind:checked={eventFilters.campaign_recipient_reported}
													on:change={() => filterUpdateCounter++}
													class="mr-2 rounded border-slate-300 dark:border-gray-700/60"
												/>
												<span class="text-gray-600 dark:text-gray-300">Reported</span>
											</label>
										</div>
									</div>
								</div>

								<div class="mt-3 pt-3 border-t border-gray-200 dark:border-gray-600 flex gap-2">
									<button
										on:click={() => {
											Object.keys(eventFilters).forEach((key) => {
												eventFilters[key] = true;
											});
											filterUpdateCounter++;
										}}
										class="flex-1 px-2 py-1 text-xs bg-cta-blue dark:bg-highlight-blue/80 text-white rounded hover:bg-blue-500 dark:hover:bg-highlight-blue focus:outline-none transition-colors duration-200"
									>
										Select All
									</button>
									<button
										on:click={() => {
											Object.keys(eventFilters).forEach((key) => {
												eventFilters[key] = false;
											});
											filterUpdateCounter++;
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

	:global(.ghost-dot-fill) {
		fill: #e5e7eb;
		animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
	}

	:global(.dark .ghost-dot-fill) {
		fill: #374151;
	}

	:global(.ghost-dot) {
		animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
	}

	:global(.center-line) {
		pointer-events: none;
	}
</style>
