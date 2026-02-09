<script>
	import { onMount, onDestroy, tick } from 'svelte';
	let resizeObserver;

	// Theme detection for chart reactivity
	let isDarkMode = false;

	// Check theme on mount and when it changes
	const checkTheme = () => {
		const newIsDarkMode = document.documentElement.classList.contains('dark');
		if (newIsDarkMode !== isDarkMode) {
			isDarkMode = newIsDarkMode;
			// Recreate chart when theme changes
			if (containerReady && chartData.length >= 2) {
				tick().then(() => createChart());
			}
		}
	};

	export let campaignStats = [];
	export let isLoading = false;

	// Debounced loading state to prevent flash
	let debouncedIsLoading = false;
	let debouncedShowPending = false;
	let loadingTimeout;
	let pendingTimeout;
	let hasAttemptedLoad = false;
	const LOADING_DEBOUNCE_MS = 200;
	const PENDING_DEBOUNCE_MS = 300;

	$: {
		if (isLoading) {
			// Mark that we've attempted to load
			hasAttemptedLoad = true;
			// Clear pending timeout since we're now loading
			if (pendingTimeout) {
				clearTimeout(pendingTimeout);
				pendingTimeout = null;
			}
			debouncedShowPending = false;
			// Start showing loading after a delay
			if (!loadingTimeout) {
				loadingTimeout = setTimeout(() => {
					debouncedIsLoading = true;
				}, LOADING_DEBOUNCE_MS);
			}
		} else {
			// Immediately hide loading and clear any pending timeout
			if (loadingTimeout) {
				clearTimeout(loadingTimeout);
				loadingTimeout = null;
			}
			debouncedIsLoading = false;
		}
	}

	// Show pending message only if we haven't attempted load and it's taking a while
	$: {
		if (!hasAttemptedLoad && !pendingTimeout) {
			pendingTimeout = setTimeout(() => {
				if (!hasAttemptedLoad) {
					debouncedShowPending = true;
				}
			}, PENDING_DEBOUNCE_MS);
		} else if (hasAttemptedLoad) {
			if (pendingTimeout) {
				clearTimeout(pendingTimeout);
				pendingTimeout = null;
			}
			debouncedShowPending = false;
		}
	}

	// localStorage keys for persisting chart settings
	const VISIBLE_METRICS_KEY = 'campaignTrend.visibleMetrics';
	const TIME_RANGE_KEY = 'campaignTrend.selectedTimeRange';
	const TREND_N_KEY = 'campaignTrend.trendN';
	const MOVING_AVG_N_KEY = 'campaignTrend.movingAvgN';
	const LOG_SCALE_KEY = 'campaignTrend.useLogScale';
	const RELATIVE_METRICS_KEY = 'campaignTrend.useRelativeMetrics';

	let chartContainer;
	let sizingContainer;
	let width = 300;
	let height = 240; // Increased height for better label spacing
	let containerReady = false;
	let tooltip;
	let tooltipTimeout;

	// User controls for N
	let trendN = 4;
	let movingAvgN = 4;
	let userHasSetTrendN = false; // track if user has manually changed trendN

	// load saved trendN and movingAvgN from localStorage
	try {
		const storedTrendN = localStorage.getItem(TREND_N_KEY);
		if (storedTrendN !== null) {
			const parsed = Number(storedTrendN);
			if (!isNaN(parsed) && parsed > 0) {
				trendN = parsed;
				userHasSetTrendN = true;
			}
		}
	} catch (e) {
		// ignore errors
	}

	try {
		const storedMovingAvgN = localStorage.getItem(MOVING_AVG_N_KEY);
		if (storedMovingAvgN !== null) {
			const parsed = Number(storedMovingAvgN);
			if (!isNaN(parsed) && parsed > 0) {
				movingAvgN = parsed;
			}
		}
	} catch (e) {
		// ignore errors
	}

	// Auto-adjust trendN and movingAvgN based on available data
	$: {
		if (chartData.length > 0) {
			// Set trendN to default to the number of campaigns for overall average only if user hasn't set it
			if (!userHasSetTrendN) {
				trendN = chartData.length;
			}

			// If current movingAvgN is larger than available data, adjust it
			if (movingAvgN > chartData.length) {
				movingAvgN = chartData.length;
			}
			// If movingAvgN is less than 1, set it to 1 (minimum)
			if (movingAvgN < 1) {
				movingAvgN = 1;
			}
		}
	}

	// legend visibility state - persisted in localstorage when possible
	const defaultVisibleMetrics = {
		openRate: true,
		clickRate: true,
		submissionRate: true,
		reportRate: true,
		'mavg-clickRate': false,
		'mavg-submissionRate': false,
		'mavg-reportRate': false
	};
	let visibleMetrics;
	try {
		const stored = localStorage.getItem(VISIBLE_METRICS_KEY);
		visibleMetrics = stored ? JSON.parse(stored) : { ...defaultVisibleMetrics };
	} catch (e) {
		visibleMetrics = { ...defaultVisibleMetrics };
	}

	// whenever metrics change, merge into visibleMetrics but preserve user choices.
	// reassign the object to ensure svelte reactivity fires.
	$: if (metrics && Array.isArray(metrics)) {
		const merged = { ...visibleMetrics };
		// add any newly introduced metrics (default visible)
		for (const metric of metrics) {
			if (!(metric.key in merged)) {
				merged[metric.key] = true;
			}
			// ensure moving-average keys exist for relevant metrics (keep existing default)
			const mavgKey = `mavg-${metric.key}`;
			if (!(mavgKey in merged)) {
				merged[mavgKey] = merged[mavgKey] ?? false;
			}
		}
		// remove keys that no longer correspond to current metrics
		for (const key of Object.keys(merged)) {
			if (key.startsWith('mavg-')) {
				const base = key.slice(5);
				if (!metrics.find((m) => m.key === base)) {
					delete merged[key];
				}
			} else {
				if (!metrics.find((m) => m.key === key)) {
					delete merged[key];
				}
			}
		}
		visibleMetrics = merged;
	}

	// persist visibleMetrics whenever it changes
	$: try {
		localStorage.setItem(VISIBLE_METRICS_KEY, JSON.stringify(visibleMetrics));
	} catch (e) {
		// ignore errors
	}

	// save selected time range
	$: try {
		localStorage.setItem(TIME_RANGE_KEY, selectedTimeRange);
	} catch (e) {
		// ignore errors
	}

	// save trend n
	$: try {
		localStorage.setItem(TREND_N_KEY, trendN.toString());
	} catch (e) {
		// ignore errors
	}

	// save moving avg n
	$: try {
		localStorage.setItem(MOVING_AVG_N_KEY, movingAvgN.toString());
	} catch (e) {
		// ignore errors
	}

	// save log scale
	$: try {
		localStorage.setItem(LOG_SCALE_KEY, useLogScale.toString());
	} catch (e) {
		// ignore errors
	}

	// save relative metrics
	$: try {
		localStorage.setItem(RELATIVE_METRICS_KEY, useRelativeMetrics.toString());
	} catch (e) {
		// ignore errors
	}

	// Responsive margins based on container width
	$: margin = {
		top: 15,
		right: Math.min(180, Math.max(160, width * 0.28)), // 28% of width, min 160px, max 180px
		bottom: 50, // Increased bottom margin for better label spacing
		left: 50
	};

	// Responsive: width is always 100% of container, SVG uses viewBox for scaling
	$: {
		// width is set by ResizeObserver below
		innerWidth = width - margin.left - margin.right;
	}

	let chartData = [];
	let xScale, yScale;
	let useLogScale = false;
	let useRelativeMetrics = false;

	// load saved settings from localStorage
	try {
		const storedLogScale = localStorage.getItem(LOG_SCALE_KEY);
		if (storedLogScale !== null) {
			useLogScale = storedLogScale === 'true';
		}
	} catch (e) {
		// ignore errors
	}

	try {
		const storedRelativeMetrics = localStorage.getItem(RELATIVE_METRICS_KEY);
		if (storedRelativeMetrics !== null) {
			useRelativeMetrics = storedRelativeMetrics === 'true';
		}
	} catch (e) {
		// ignore errors
	}

	// Time range filter for campaigns
	const timeRanges = [
		{ label: 'Last 3 months', value: '3' },
		{ label: 'Last 6 months', value: '6' },
		{ label: 'Last 12 months', value: '12' },
		{ label: 'Last 24 months', value: '24' },
		{ label: 'Last 36 months', value: '36' }
	];
	let selectedTimeRange = '12'; // default to last 12 months

	// load saved time range from localStorage
	try {
		const storedTimeRange = localStorage.getItem(TIME_RANGE_KEY);
		if (storedTimeRange !== null && timeRanges.some((r) => r.value === storedTimeRange)) {
			selectedTimeRange = storedTimeRange;
		}
	} catch (e) {
		// ignore errors
	}

	// Filtered campaigns based on selected time range (using sendStartAt)
	$: filteredCampaignStats = (() => {
		if (!campaignStats || campaignStats.length === 0) return [];
		const range = Number(selectedTimeRange);
		const now = new Date();
		const cutoff = new Date(now.getFullYear(), now.getMonth() - range + 1, 1);
		return campaignStats.filter((c) => {
			// filter out stats with no dates (data integrity issue)
			if (!c.campaignStartDate && !c.createdAt) {
				console.warn('Filtering out campaign stat with missing dates:', c);
				return false;
			}
			const startDate = c.campaignStartDate ? new Date(c.campaignStartDate) : new Date(c.createdAt);
			return startDate >= cutoff;
		});
	})();

	const metrics = [
		{ key: 'openRate', label: 'Read Rate', color: '#4cb5b5', suffix: '%' },
		{ key: 'clickRate', label: 'Click Rate', color: '#f96dcf', suffix: '%' },
		{ key: 'submissionRate', label: 'Submission Rate', color: '#f42e41', suffix: '%' },
		{ key: 'reportRate', label: 'Report Rate', color: '#1e40af', suffix: '%' }
	];

	// toggle metric visibility (reassign to trigger svelte reactivity and persist)
	function toggleMetric(metricKey) {
		visibleMetrics = { ...visibleMetrics, [metricKey]: !visibleMetrics[metricKey] };
		if (containerReady) {
			createChart();
		}
	}

	// --- Trendline stats for last N campaigns (user selectable) ---
	$: trendStats = (() => {
		const n = Math.min(trendN, chartData.length);
		if (n === 0) return null;
		const slice = chartData.slice(-n);
		const avg = (arr, key) => arr.reduce((sum, d) => sum + (d[key] || 0), 0) / n;
		return {
			n,
			openRate: avg(slice, 'openRate'),
			clickRate: avg(slice, 'clickRate'),
			submissionRate: avg(slice, 'submissionRate'),
			reportRate: avg(slice, 'reportRate')
		};
	})();

	// Update chartData to use filteredCampaignStats, using sendStartAt as date, and sort by date ascending
	$: chartData = filteredCampaignStats
		.filter((c) => {
			if (!c.campaignStartDate && !c.createdAt) {
				console.warn('Skipping campaign stat with missing dates in chart data:', c);
				return false;
			}
			return true;
		})
		.map((c) => ({
			...c,
			date: c.campaignStartDate ? new Date(c.campaignStartDate) : new Date(c.createdAt),
			name: c.campaignName || c.name || c.title || ''
		}))
		.sort((a, b) => a.date.getTime() - b.date.getTime());

	// --- Force chart rerender ---
	let chartKey = '';
	$: chartKey = chartData
		.map((d) => d.name + (d.date instanceof Date ? d.date.toISOString() : d.date))
		.join('-');

	// --- Data processing ---
	function processData(stats, useRelativeMetrics) {
		const sortedStats = [...(stats || [])]
			.filter((stat) => stat.campaignClosedAt)
			.sort(
				(a, b) => new Date(a.campaignClosedAt).getTime() - new Date(b.campaignClosedAt).getTime()
			);

		// calculate percentage with one decimal place
		function pct(n, d) {
			return d > 0 ? Math.round((n / d) * 1000) / 10 : 0;
		}

		return sortedStats.map((stat, index) => ({
			index: index + 1,
			date: stat.campaignClosedAt ? new Date(stat.campaignClosedAt) : null,
			name: stat.campaignName || `Campaign ${index + 1}`,
			openRate: pct(stat.trackingPixelLoaded, stat.totalRecipients),
			clickRate: useRelativeMetrics
				? pct(stat.websiteVisits, stat.trackingPixelLoaded)
				: pct(stat.websiteVisits, stat.totalRecipients),
			submissionRate: useRelativeMetrics
				? pct(stat.dataSubmissions, stat.websiteVisits)
				: pct(stat.dataSubmissions, stat.totalRecipients),
			reportRate: pct(stat.reported, stat.totalRecipients),
			totalRecipients: stat.totalRecipients
		}));
	}

	// Use filtered campaign stats based on selected time range and relative metrics toggle
	$: chartData = processData(filteredCampaignStats, useRelativeMetrics);

	function createChart() {
		if (!chartContainer || chartData.length < 2) return;

		// Remove all children (safer than innerHTML)
		while (chartContainer.firstChild) {
			chartContainer.removeChild(chartContainer.firstChild);
		}

		const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
		svg.setAttribute('width', '100%');
		svg.setAttribute('height', height.toString());
		svg.setAttribute(
			'viewBox',
			`0 0 ${Math.min(width, chartContainer.clientWidth || width)} ${height}`
		);
		svg.setAttribute('preserveAspectRatio', 'xMidYMid meet');
		svg.style.maxWidth = '100%';
		svg.style.width = '100%';
		svg.style.overflow = 'hidden';
		svg.style.display = 'block';
		svg.setAttribute('class', 'campaign-trend-chart');
		chartContainer.appendChild(svg);

		// Add dark background rectangle
		const bgRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
		bgRect.setAttribute('width', '100%');
		bgRect.setAttribute('height', '100%');
		bgRect.setAttribute(
			'fill',
			document.documentElement.classList.contains('dark') ? '#111827' : '#ffffff'
		);
		svg.appendChild(bgRect);

		const xExtent = [0, chartData.length - 1];
		// Map 0-100% directly to chart boundaries so 0% aligns with X-axis
		const yExtent = [0, 100];

		xScale = createLinearScale(xExtent, [margin.left, width - margin.right]);
		yScale = useLogScale
			? createLogScale(yExtent, [height - margin.bottom, margin.top])
			: createLinearScale(yExtent, [height - margin.bottom, margin.top]);

		createGridLines(svg);
		createAxes(svg);

		metrics.forEach((metric) => {
			createLine(svg, metric);
			// Hide line if not visible
			if (!visibleMetrics[metric.key]) {
				const lines = svg.querySelectorAll(`.main-line-${metric.key}`);
				lines.forEach((line) => {
					if (line instanceof HTMLElement) line.style.display = 'none';
				});
			}
		});
		// Only draw moving average for clickRate, submissionRate, and reportRate that are visible
		['clickRate', 'submissionRate', 'reportRate'].forEach((metricKey) => {
			const metric = metrics.find((m) => m.key === metricKey);
			if (metric && visibleMetrics[`mavg-${metricKey}`]) {
				createMovingAverageLine(svg, metric, movingAvgN);
			}
		});
		metrics.forEach((metric) => {
			createDataPoints(svg, metric);
			// Hide data points if not visible
			if (!visibleMetrics[metric.key]) {
				const points = svg.querySelectorAll(`[data-metric="${metric.key}"]`);
				const glows = svg.querySelectorAll(`.chart-point-glow-${metric.key}`);
				points.forEach((point) => {
					if (point instanceof HTMLElement) point.style.display = 'none';
				});
				glows.forEach((glow) => {
					if (glow instanceof HTMLElement) glow.style.display = 'none';
				});
			}
		});
		createLegend(svg, svg);
		createTooltip(svg, svg.querySelectorAll('.chart-point'));

		// Add hover listeners for legend enhancement
		setupLegendHover(svg);

		// apply stored visibility to DOM elements after chart rebuilt
		// ensures user toggles remain applied even after hover interactions and data refreshes
		if (typeof applyVisibility === 'function') {
			applyVisibility(svg);
		}
	}

	// Calculate moving average for a metric, window N (default 4, min 3)
	function getMovingAverageData(metricKey, windowN = 4) {
		const result = [];
		for (let i = 0; i < chartData.length; i++) {
			const n = Math.min(windowN, i + 1);
			let sum = 0;
			for (let j = i - n + 1; j <= i; j++) {
				sum += chartData[j][metricKey] || 0;
			}
			result.push(sum / n);
		}
		return result;
	}

	// Draw moving average line for a metric
	function createMovingAverageLine(svg, metric, windowN = 4) {
		if (chartData.length < 2) return;
		const movingAvg = getMovingAverageData(metric.key, windowN);
		const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
		let pathData = '';
		let started = false;
		movingAvg.forEach((val, i) => {
			if (val === null) return;
			const x = xScale(i);
			// Clamp values to 0-100 range to prevent lines from going outside chart bounds
			const clampedValue = Math.max(0, Math.min(100, val));
			const y = yScale(clampedValue);
			if (!started) {
				pathData += `M ${x} ${y}`;
				started = true;
			} else {
				pathData += ` L ${x} ${y}`;
			}
		});
		if (started) {
			path.setAttribute('d', pathData);
			path.setAttribute('fill', 'none');
			// Use a lighter shade of the metric color for moving average
			let avgColor = metric.color;
			if (metric.key === 'openRate') {
				avgColor = '#93c5fd'; // light blue
			} else if (metric.key === 'submissionRate') {
				avgColor = '#ff6a91'; // lighter red, closer to #f42e41
			} else if (metric.key === 'reportRate') {
				avgColor = '#60a5fa'; // lighter blue for report rate
			}
			path.setAttribute('stroke', avgColor);
			path.setAttribute('stroke-width', '1.2');
			path.setAttribute('stroke-dasharray', '6,4');
			path.setAttribute('opacity', '0.95');
			path.setAttribute('class', `moving-average-line moving-average-${metric.key}`);
			svg.appendChild(path);
		}
	}

	function createLinearScale(domain, range) {
		const [d0, d1] = domain;
		const [r0, r1] = range;
		const k = (r1 - r0) / (d1 - d0 || 1);
		return (value) => r0 + k * (value - d0);
	}

	// log scale for y axis (clamps to min > 0)
	function createLogScale(domain, range) {
		const [d0, d1] = domain;
		const [r0, r1] = range;
		// clamp minimum to 0.5 to avoid wasting space on very low values
		const min = Math.max(d0, 0.5);
		const logd0 = Math.log10(min);
		const logd1 = Math.log10(d1);
		const k = (r1 - r0) / (logd1 - logd0 || 1);
		return (value) => {
			const v = Math.max(value, 0.5);
			return r0 + k * (Math.log10(v) - logd0);
		};
	}

	function createGridLines(svg) {
		// Create subtle gradient for grid lines
		const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
		const gradient = document.createElementNS('http://www.w3.org/2000/svg', 'linearGradient');
		gradient.setAttribute('id', 'gridGradient');
		gradient.setAttribute('x1', '0%');
		gradient.setAttribute('x2', '100%');

		const stop1 = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
		stop1.setAttribute('offset', '0%');
		stop1.setAttribute(
			'stop-color',
			document.documentElement.classList.contains('dark') ? '#1f2937' : '#f8f9fa'
		);
		stop1.setAttribute('stop-opacity', '0.3');

		const stop2 = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
		stop2.setAttribute('offset', '50%');
		stop2.setAttribute(
			'stop-color',
			document.documentElement.classList.contains('dark') ? '#374151' : '#e9ecef'
		);
		stop2.setAttribute('stop-opacity', '0.4');

		const stop3 = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
		stop3.setAttribute('offset', '100%');
		stop3.setAttribute(
			'stop-color',
			document.documentElement.classList.contains('dark') ? '#1f2937' : '#f8f9fa'
		);
		stop3.setAttribute('stop-opacity', '0.3');

		gradient.appendChild(stop1);
		gradient.appendChild(stop2);
		gradient.appendChild(stop3);
		defs.appendChild(gradient);
		svg.appendChild(defs);

		// horizontal percentage reference lines
		// use logarithmic tick values when log scale is enabled for better distribution
		// starts at 0.5 to match the clamped log scale minimum
		const tickValues = useLogScale
			? [0.5, 1, 2, 5, 10, 20, 30, 50, 70, 100]
			: [0, 20, 40, 60, 80, 100];

		tickValues.forEach((value) => {
			const y = yScale(value);
			// skip if y is out of bounds
			if (y < margin.top || y > height - margin.bottom) return;
			const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
			line.setAttribute('x1', margin.left.toString());
			line.setAttribute('x2', (width - margin.right).toString());
			line.setAttribute('y1', y.toString());
			line.setAttribute('y2', y.toString());
			line.setAttribute(
				'stroke',
				document.documentElement.classList.contains('dark') ? '#4b5563' : '#E5E7EB'
			);
			line.setAttribute('stroke-width', '1');
			line.setAttribute('opacity', '0.4');
			svg.appendChild(line);
		});

		// Thin vertical background lines for data points
		chartData.forEach((d, i) => {
			const x = xScale(i);
			const vLine = document.createElementNS('http://www.w3.org/2000/svg', 'line');
			vLine.setAttribute('x1', x.toString());
			vLine.setAttribute('x2', x.toString());
			vLine.setAttribute('y1', margin.top.toString());
			vLine.setAttribute('y2', (height - margin.bottom).toString());
			vLine.setAttribute(
				'stroke',
				document.documentElement.classList.contains('dark') ? '#374151' : '#e5e7eb'
			);
			vLine.setAttribute('stroke-width', '0.5');
			vLine.setAttribute('opacity', '0.3');
			svg.appendChild(vLine);
		});
	}

	function createAxes(svg) {
		const yAxis = document.createElementNS('http://www.w3.org/2000/svg', 'line');
		yAxis.setAttribute('x1', margin.left.toString());
		yAxis.setAttribute('x2', margin.left.toString());
		yAxis.setAttribute('y1', margin.top.toString());
		yAxis.setAttribute('y2', (height - margin.bottom).toString());
		yAxis.setAttribute(
			'stroke',
			document.documentElement.classList.contains('dark') ? '#6887ea' : '#6B7280'
		);
		yAxis.setAttribute('stroke-width', '2');
		svg.appendChild(yAxis);

		const xAxis = document.createElementNS('http://www.w3.org/2000/svg', 'line');
		xAxis.setAttribute('x1', margin.left.toString());
		xAxis.setAttribute('x2', (width - margin.right).toString());
		xAxis.setAttribute('y1', (height - margin.bottom).toString());
		xAxis.setAttribute('y2', (height - margin.bottom).toString());
		xAxis.setAttribute(
			'stroke',
			document.documentElement.classList.contains('dark') ? '#6887ea' : '#6B7280'
		);
		xAxis.setAttribute('stroke-width', '2');
		svg.appendChild(xAxis);

		// use logarithmic tick values when log scale is enabled for better distribution
		// starts at 0.5 to match the clamped log scale minimum
		const axisTickValues = useLogScale
			? [0.5, 1, 2, 5, 10, 20, 30, 50, 70, 100]
			: [0, 20, 40, 60, 80, 100];

		axisTickValues.forEach((value) => {
			const y = yScale(value);
			// skip if y is out of bounds
			if (y < margin.top || y > height - margin.bottom) return;
			const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
			text.setAttribute('x', (margin.left - 10).toString());
			text.setAttribute('y', (y - 2).toString());
			text.setAttribute('text-anchor', 'end');
			text.setAttribute('font-size', useLogScale ? '9' : '11');
			text.setAttribute(
				'fill',
				document.documentElement.classList.contains('dark') ? '#e5e7eb' : '#6B7280'
			);
			text.setAttribute('font-weight', '500');
			text.setAttribute('alignment-baseline', 'middle');
			// format small values without trailing zeros
			text.textContent = value < 1 ? `${value}%` : `${Math.round(value)}%`;
			svg.appendChild(text);
		});

		// Show labels for EVERY campaign with valid dates
		chartData.forEach((d, i) => {
			// Show label for every campaign that has a valid date
			if (d.date instanceof Date && !isNaN(d.date.getTime())) {
				const x = xScale(i);
				const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
				text.setAttribute('x', x.toString());
				text.setAttribute('y', (height - margin.bottom + 25).toString());
				text.setAttribute('text-anchor', 'middle');
				text.setAttribute('font-size', '11');
				text.setAttribute(
					'fill',
					document.documentElement.classList.contains('dark') ? '#f3f4f6' : '#000'
				);
				text.setAttribute('alignment-baseline', 'middle');
				// Show mm-yy format
				const labelSpan = document.createElementNS('http://www.w3.org/2000/svg', 'tspan');
				labelSpan.setAttribute('x', x.toString());
				labelSpan.setAttribute('dy', '0');
				const month = String(d.date.getMonth() + 1).padStart(2, '0');
				const year = String(d.date.getFullYear()).slice(-2);
				labelSpan.textContent = `${month}-${year}`;
				text.appendChild(labelSpan);

				svg.appendChild(text);
			}
		});
	}

	function createLine(svg, metric) {
		if (chartData.length < 2) return;
		const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
		let pathData = '';
		chartData.forEach((d, i) => {
			const x = xScale(i);
			// Clamp values to 0-100 range to prevent lines from going outside chart bounds
			const clampedValue = Math.max(0, Math.min(100, d[metric.key] || 0));
			const y = yScale(clampedValue);
			if (i === 0) {
				pathData += `M ${x} ${y}`;
			} else {
				pathData += ` L ${x} ${y}`;
			}
		});
		path.setAttribute('d', pathData);
		path.setAttribute('fill', 'none');
		path.setAttribute('stroke', metric.color);
		path.setAttribute('stroke-width', '3');
		path.setAttribute('stroke-linecap', 'round');
		path.setAttribute('stroke-linejoin', 'round');
		path.setAttribute('class', `main-line main-line-${metric.key}`);
		svg.appendChild(path);
	}

	function createDataPoints(svg, metric) {
		chartData.forEach((point, i) => {
			const x = xScale(i);
			// Clamp values to 0-100 range to prevent points from going outside chart bounds
			const clampedValue = Math.max(0, Math.min(100, point[metric.key] || 0));
			const y = yScale(clampedValue);

			// Main circle
			const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
			circle.setAttribute('cx', x.toString());
			circle.setAttribute('cy', y.toString());
			circle.setAttribute('r', '5');
			circle.setAttribute('fill', '#ffffff');
			circle.setAttribute('stroke', metric.color);
			circle.setAttribute('stroke-width', '3');
			circle.setAttribute('class', 'chart-point');
			circle.setAttribute('data-index', i.toString());
			circle.setAttribute('data-metric', metric.key);
			circle.style.filter = 'drop-shadow(0 2px 4px rgba(0,0,0,0.15))';
			svg.appendChild(circle);

			// per-point hover listeners to control only the matching glow element.
			// this avoids affecting adjacent points while keeping the original hover UX.
			circle.addEventListener('mouseenter', () => {
				try {
					const glow = svg.querySelector(`.chart-point-glow-${metric.key}[data-index="${i}"]`);
					if (glow) {
						glow.setAttribute('r', '12');
						glow.style.opacity = '0.4';
					}
				} catch (e) {
					// ignore
				}
			});
			circle.addEventListener('mouseleave', () => {
				try {
					const glow = svg.querySelector(`.chart-point-glow-${metric.key}[data-index="${i}"]`);
					if (glow) {
						glow.setAttribute('r', '8');
						glow.style.opacity = '0.2';
					}
				} catch (e) {
					// ignore
				}
			});
		});
	}

	function applyVisibility(svg) {
		// enforce visibility state for all chart elements based on visibleMetrics
		const allMainLines = svg.querySelectorAll('.main-line');
		const allMovingAvgLines = svg.querySelectorAll('.moving-average-line');
		const allDataPoints = svg.querySelectorAll('.chart-point');
		const allGlowPoints = svg.querySelectorAll('.chart-point-glow');

		allMainLines.forEach((line) => {
			const metricKey = line.classList.toString().match(/main-line-(\w+)/)?.[1];
			if (metricKey && visibleMetrics[metricKey]) {
				line.style.display = 'block';
			} else {
				line.style.display = 'none';
			}
			line.classList.remove('line-enhanced', 'line-faded');
		});

		// handle moving average lines by metric keys to avoid matching the 'moving-average-line' token
		metrics.forEach((metric) => {
			const lines = svg.querySelectorAll(`.moving-average-${metric.key}`);
			lines.forEach((line) => {
				if (visibleMetrics[`mavg-${metric.key}`]) {
					line.style.display = 'block';
				} else {
					line.style.display = 'none';
				}
				line.classList.remove('line-enhanced', 'line-faded');
			});
		});

		allDataPoints.forEach((point) => {
			const metricKey = point.getAttribute('data-metric');
			if (metricKey && visibleMetrics[metricKey]) {
				point.style.display = 'block';
				point.style.opacity = '';
			} else {
				point.style.display = 'none';
			}
		});

		allGlowPoints.forEach((glow) => {
			const metricKey = glow.classList.toString().match(/chart-point-glow-(\w+)/)?.[1];
			if (metricKey && visibleMetrics[metricKey]) {
				glow.style.display = 'block';
				glow.style.opacity = '';
			} else {
				glow.style.display = 'none';
			}
		});
	}

	function createLegend(svg, svgRoot) {
		const legendY = margin.top + 5; // Align with chart top area
		const legendX = width - margin.right + 10;
		const legendSpacing = 18; // More spacing between legend items

		// Build a flat list of legend items (main and moving averages)
		const legendItems = [];
		metrics.forEach((metric) => {
			legendItems.push({
				type: 'main',
				key: metric.key,
				label: metric.label,
				color: metric.color,
				class: `legend-line legend-${metric.key}`,
				labelClass: `legend-label legend-${metric.key}`,
				dataMetric: metric.key,
				strokeWidth: 3,
				strokeDasharray: null,
				opacity: 1
			});
			if (
				metric.key === 'clickRate' ||
				metric.key === 'submissionRate' ||
				metric.key === 'reportRate'
			) {
				// Use a lighter version of the main color for moving averages
				let avgColor = metric.color;
				let avgLabel = '';
				if (metric.key === 'clickRate') {
					avgColor = '#eea5fa'; // before-page-visited, lighter pink
					avgLabel = 'Click MA';
				} else if (metric.key === 'submissionRate') {
					avgColor = '#ff6a91'; // lighter red, closer to #f42e41
					avgLabel = 'Submit MA';
				} else if (metric.key === 'reportRate') {
					avgColor = '#60a5fa'; // lighter blue for report rate
					avgLabel = 'Report MA';
				}
				legendItems.push({
					type: 'mavg',
					key: metric.key,
					label: avgLabel,
					color: avgColor,
					class: `legend-line legend-mavg legend-mavg-${metric.key}`,
					labelClass: `legend-label legend-mavg legend-mavg-${metric.key}`,
					dataMetric: `mavg-${metric.key}`,
					strokeWidth: 1.2,
					strokeDasharray: '6,4',
					opacity: 0.95
				});
			}
		});

		legendItems.forEach((item, i) => {
			const y = legendY + i * legendSpacing;

			// Create clickable group for legend item
			const group = document.createElementNS('http://www.w3.org/2000/svg', 'g');
			group.setAttribute('class', 'legend-item');
			group.setAttribute('data-metric', item.dataMetric);
			group.style.cursor = 'pointer';

			const isVisible = visibleMetrics[item.dataMetric];
			const opacity = isVisible ? '1' : '0.3';

			const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
			line.setAttribute('x1', legendX.toString());
			line.setAttribute('x2', (legendX + 18).toString());
			line.setAttribute('y1', y.toString());
			line.setAttribute('y2', y.toString());
			line.setAttribute('stroke', item.color);
			line.setAttribute('stroke-width', item.strokeWidth.toString());
			if (item.strokeDasharray) line.setAttribute('stroke-dasharray', item.strokeDasharray);
			line.setAttribute('opacity', opacity);
			line.setAttribute('class', item.class);

			const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
			text.setAttribute('x', (legendX + 24).toString());
			text.setAttribute('y', y.toString());
			text.setAttribute('font-size', '11');
			text.setAttribute('font-weight', '500');
			text.setAttribute('fill', item.color);
			text.setAttribute('opacity', opacity);
			text.setAttribute('alignment-baseline', 'middle');
			text.setAttribute('class', item.labelClass);
			text.textContent = item.label;

			group.appendChild(line);
			group.appendChild(text);

			// Add click handler for toggling
			group.addEventListener('click', () => {
				toggleMetric(item.dataMetric);
			});

			svg.appendChild(group);
		});
	}

	// Enhance line on legend hover
	function setupLegendHover(svg) {
		const legendItems = svg.querySelectorAll('.legend-item');
		const allMainLines = svg.querySelectorAll('.main-line');
		const allMovingAvgLines = svg.querySelectorAll('.moving-average-line');
		const allDataPoints = svg.querySelectorAll('.chart-point');
		const allGlowPoints = svg.querySelectorAll('.chart-point-glow');

		legendItems.forEach((item) => {
			item.addEventListener('mouseenter', (e) => {
				const hoveredMetric = e.currentTarget.getAttribute('data-metric');

				// Show all lines with faded opacity first
				allMainLines.forEach((line) => {
					const metricKey = line.classList.toString().match(/main-line-(\w+)/)?.[1];
					if (metricKey && visibleMetrics[metricKey]) {
						line.style.display = 'block';
					}
					line.classList.add('line-faded');
					line.classList.remove('line-enhanced');
				});
				allMovingAvgLines.forEach((line) => {
					line.classList.add('line-faded');
					line.classList.remove('line-enhanced');
				});
				allDataPoints.forEach((point) => {
					const metricKey = point.getAttribute('data-metric');
					if (metricKey && visibleMetrics[metricKey]) {
						point.style.display = 'block';
						point.style.opacity = '0.2';
					}
				});
				allGlowPoints.forEach((glow) => {
					const metricKey = glow.classList.toString().match(/chart-point-glow-(\w+)/)?.[1];
					if (metricKey && visibleMetrics[metricKey]) {
						glow.style.display = 'block';
						glow.style.opacity = '0.04';
					}
				});

				// Enhance only the hovered metric
				if (hoveredMetric.startsWith('mavg-')) {
					const baseMetric = hoveredMetric.replace('mavg-', '');
					allMovingAvgLines.forEach((line) => {
						if (line.classList.contains(`moving-average-${baseMetric}`)) {
							line.style.display = 'block';
							line.classList.add('line-enhanced');
							line.classList.remove('line-faded');
						}
					});
				} else {
					allMainLines.forEach((line) => {
						if (line.classList.contains(`main-line-${hoveredMetric}`)) {
							line.style.display = 'block';
							line.classList.add('line-enhanced');
							line.classList.remove('line-faded');
						}
					});
					// Show and enhance data points for the hovered metric
					allDataPoints.forEach((point) => {
						if (point.getAttribute('data-metric') === hoveredMetric) {
							point.style.display = 'block';
							point.style.opacity = '1';
						}
					});
					allGlowPoints.forEach((glow) => {
						if (glow.classList.contains(`chart-point-glow-${hoveredMetric}`)) {
							glow.style.display = 'block';
							glow.style.opacity = '0.2';
						}
					});
				}
			});

			item.addEventListener('mouseleave', () => {
				// restore visibility based on persisted visibleMetrics
				if (typeof applyVisibility === 'function') {
					applyVisibility(svg);
				}
			});
		});
	}

	function showTooltip(event, data, index, metricKey) {
		if (!tooltip || !data) return;

		// clear any pending hide
		if (tooltipTimeout) clearTimeout(tooltipTimeout);

		// show tooltip
		tooltip.style.display = 'block';
		tooltip.style.width = '320px';

		// glow handled by per-point listeners; no programmatic highlight here

		// use requestAnimationFrame for positioning similar to EventTimeline
		requestAnimationFrame(() => {
			// get cursor position relative to the chart container
			const containerRect = chartContainer.getBoundingClientRect();
			const x = event.clientX - containerRect.left;
			const y = event.clientY - containerRect.top;

			const tooltipWidth = 320;
			const containerWidth = chartContainer.offsetWidth;
			const leftPosition = x + 25; // increased offset to avoid cursor
			const rightEdge = leftPosition + tooltipWidth;

			// better positioning logic - more balanced positioning
			let adjustedLeft;
			if (rightEdge > containerWidth - 20) {
				// position to the left with some offset, not too far
				adjustedLeft = x - tooltipWidth - 15; // position to left of cursor
			} else {
				adjustedLeft = leftPosition;
			}

			tooltip.style.left = `${Math.max(10, adjustedLeft)}px`;
			tooltip.style.top = `${Math.max(10, y - 40)}px`; // position above cursor

			// update tooltip content
			updateTooltipContent(data, metricKey);
		});
	}

	function hideTooltip(index, metricKey) {
		if (tooltipTimeout) clearTimeout(tooltipTimeout);
		tooltipTimeout = setTimeout(() => {
			if (tooltip) {
				tooltip.style.display = 'none';
			}
		}, 150);
	}

	function updateTooltipContent(data, hoveredMetric) {
		if (!tooltip) return;

		try {
			const formattedDate =
				data.date instanceof Date
					? `${data.date.getFullYear()}/${String(data.date.getMonth() + 1).padStart(2, '0')}`
					: 'Unknown Date';

			const visibleMetricsList = metrics.filter((metric) => visibleMetrics[metric.key]);
			const hoveredMetricInfo = metrics.find((metric) => metric.key === hoveredMetric);

			// clear tooltip and build content safely using DOM methods
			tooltip.innerHTML = '';

			// create main container
			const container = document.createElement('div');
			container.className = 'overflow-hidden';

			// create header with colored border
			const header = document.createElement('div');
			header.className = 'border-t-4';
			header.style.borderTopColor = hoveredMetricInfo?.color || '#3b82f6';

			const headerContent = document.createElement('div');
			headerContent.className = 'px-4 py-3 text-gray-800 dark:text-gray-200';

			const headerFlex = document.createElement('div');
			headerFlex.className = 'flex items-center space-x-2';

			// no indicator dot needed for cleaner look

			const textContainer = document.createElement('div');
			textContainer.className = 'flex-1 min-w-0';

			const title = document.createElement('h3');
			title.className = 'text-sm font-bold truncate';
			title.textContent = data.name;

			const date = document.createElement('p');
			date.className = 'text-xs text-gray-600 dark:text-gray-400';
			date.textContent = formattedDate;

			textContainer.appendChild(title);
			textContainer.appendChild(date);
			headerFlex.appendChild(textContainer);
			headerContent.appendChild(headerFlex);
			header.appendChild(headerContent);
			container.appendChild(header);

			// create body content
			const body = document.createElement('div');
			body.className = 'px-4 py-3 space-y-3';

			// recipients section
			const recipientsSection = document.createElement('div');
			recipientsSection.className = 'flex items-center space-x-2';

			// no icon needed for cleaner look

			const recipientsText = document.createElement('span');
			recipientsText.className = 'text-sm text-gray-700 dark:text-gray-300';
			recipientsText.textContent = `Total Recipients: ${data.totalRecipients || 0}`;

			recipientsSection.appendChild(recipientsText);
			body.appendChild(recipientsSection);

			// metrics section
			if (visibleMetricsList.length > 0) {
				const metricsSection = document.createElement('div');
				metricsSection.className =
					'mt-3 pt-3 border-t border-gray-200 dark:border-gray-600 space-y-2';

				visibleMetricsList.forEach((metric) => {
					const metricRow = document.createElement('div');
					metricRow.className = 'flex items-center justify-between';

					const metricLeft = document.createElement('div');
					metricLeft.className = 'flex items-center space-x-2';

					const metricDot = document.createElement('div');
					metricDot.className = 'w-3 h-3 rounded-full';
					metricDot.style.backgroundColor = metric.color;

					const metricLabel = document.createElement('span');
					metricLabel.className = 'text-sm text-gray-700 dark:text-gray-300 flex-1 pr-4';
					metricLabel.textContent = metric.label;

					const metricValue = document.createElement('span');
					metricValue.className = 'text-sm font-semibold ml-auto';
					metricValue.style.color = metric.color;
					metricValue.textContent = Math.round(data[metric.key] || 0).toString();

					metricLeft.appendChild(metricDot);
					metricLeft.appendChild(metricLabel);
					metricRow.appendChild(metricLeft);
					metricRow.appendChild(metricValue);
					metricsSection.appendChild(metricRow);
				});

				body.appendChild(metricsSection);
			}

			container.appendChild(body);
			tooltip.appendChild(container);
		} catch (e) {
			console.error('Error updating tooltip content:', e);
			hideTooltip();
		}
	}

	function createTooltip(svg, points) {
		points.forEach((point) => {
			point.addEventListener('mouseenter', (e) => {
				const index = parseInt(e.target.getAttribute('data-index'));
				const metricKey = e.target.getAttribute('data-metric');
				const data = chartData[index];
				showTooltip(e, data, index, metricKey);
			});

			point.addEventListener('mouseleave', (e) => {
				const index = parseInt(e.target.getAttribute('data-index'));
				const metricKey = e.target.getAttribute('data-metric');
				hideTooltip(index, metricKey);
			});
		});
	}

	let themeObserver;

	onMount(async () => {
		await tick(); // Wait for DOM/layout

		// Initialize theme detection
		checkTheme();

		// Watch for theme changes
		themeObserver = new MutationObserver(checkTheme);
		themeObserver.observe(document.documentElement, {
			attributes: true,
			attributeFilter: ['class']
		});

		if (sizingContainer) {
			const containerWidth = sizingContainer.parentElement?.clientWidth || 0;
			width = Math.min(Math.max(containerWidth, 300), containerWidth); // Minimum 300px but never exceed container
			if (width > 0) containerReady = true;
			resizeObserver = new ResizeObserver((entries) => {
				for (const entry of entries) {
					const newWidth = entry.contentRect.width;
					if (newWidth > 0) {
						width = Math.min(Math.max(newWidth, 300), newWidth); // Minimum 300px but never exceed container
						containerReady = true;
					}
				}
			});
			resizeObserver.observe(sizingContainer.parentElement || sizingContainer);
		}
	});

	onDestroy(() => {
		if (resizeObserver && sizingContainer) {
			resizeObserver.unobserve(sizingContainer.parentElement || sizingContainer);
		}
		// Clean up theme observer
		if (themeObserver) {
			themeObserver.disconnect();
		}
		if (loadingTimeout) {
			clearTimeout(loadingTimeout);
		}
		if (pendingTimeout) {
			clearTimeout(pendingTimeout);
		}
		if (tooltipTimeout) {
			clearTimeout(tooltipTimeout);
		}
	});

	// Only create chart when width is set and chartData is ready
	$: {
		if (containerReady && chartContainer && width > 0 && chartData.length > 1 && movingAvgN) {
			// reference toggles so Svelte tracks them for reactivity
			useLogScale;
			useRelativeMetrics;
			createChart();
		}
	}
</script>

<div class="w-full box-border" style="contain: layout style;">
	<!-- Hidden sizing element for ResizeObserver -->
	<div
		bind:this={sizingContainer}
		class="chart-container w-full overflow-x-auto"
		style="height:0;overflow:hidden;visibility:hidden;position:absolute;"
	></div>
	{#if !containerReady}
		<div class="flex items-center justify-center min-h-[100px]">
			<span class="text-gray-400 dark:text-gray-500 text-sm transition-colors duration-200"
				>Preparing chart…</span
			>
		</div>
	{:else}
		<div>
			{#if debouncedIsLoading}
				<div class="flex items-center justify-center h-64">
					<div
						class="animate-spin rounded-full h-8 w-8 border-b-2 border-cta-blue dark:border-highlight-blue transition-colors duration-200"
					></div>
					<span class="ml-2 text-gray-600 dark:text-gray-300 transition-colors duration-200"
						>Loading trend data...</span
					>
				</div>
			{:else if !hasAttemptedLoad && debouncedShowPending}
				<div class="flex items-center justify-center h-64">
					<span class="text-gray-400 dark:text-gray-500 text-sm transition-colors duration-200"
						>Preparing trend data...</span
					>
				</div>
			{:else if hasAttemptedLoad && !isLoading && !debouncedIsLoading && campaignStats.length < 2}
				<div
					class="bg-white dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-700 p-6 transition-colors duration-200"
				>
					<!-- Match the height structure of the data container -->
					<div class="flex flex-row items-center justify-between mb-2 pb-0 flex-wrap gap-2">
						<h4
							class="text-sm font-medium text-gray-600 dark:text-gray-300 m-0 transition-colors duration-200"
						>
							Trend Analytics
						</h4>
					</div>
					<div class="mb-8"></div>

					<!-- Centered content matching stats grid height -->
					<div class="flex items-center justify-center min-h-[350px] py-16">
						<div class="text-center">
							<svg
								class="mx-auto h-12 w-12 text-cta-blue dark:text-highlight-blue transition-colors duration-200"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								aria-hidden="true"
							>
								<path
									stroke-linecap="round"
									stroke-linejoin="round"
									stroke-width="2"
									d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
								/>
							</svg>
							<h3
								class="mt-3 text-lg font-medium text-gray-900 dark:text-gray-100 transition-colors duration-200"
							>
								Insufficient data for trend analysis
							</h3>
							<p
								class="mt-2 text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200"
							>
								Trend analysis requires at least 2 completed campaigns to show meaningful patterns.
							</p>
						</div>
					</div>
				</div>
			{:else if hasAttemptedLoad && !isLoading && !debouncedIsLoading && campaignStats.length >= 2}
				<div
					class="bg-white dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-700 p-6 transition-colors duration-200"
				>
					<!-- Trendline stats and controls above chart -->
					<div class="flex flex-row items-center justify-between mb-2 pb-0 flex-wrap gap-2">
						<h4
							class="text-sm font-medium text-gray-600 dark:text-gray-300 m-0 transition-colors duration-200"
						>
							Trendline: Last {trendStats ? trendStats.n : campaignStats.length} Campaigns (average)
						</h4>
						<div class="flex flex-wrap items-center gap-2 mb-0">
							<label
								class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>
								Time range:
								<select
									bind:value={selectedTimeRange}
									class="border border-gray-300 dark:border-gray-600 rounded px-1 py-0 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
									style="height: 1.5rem;"
								>
									{#each timeRanges as range}
										<option value={range.value}>{range.label}</option>
									{/each}
								</select>
							</label>
							{#if campaignStats.length > 1}
								<label
									class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
								>
									Trendline N:
									<input
										type="text"
										min="1"
										max={campaignStats.length}
										bind:value={trendN}
										on:input={() => (userHasSetTrendN = true)}
										class="h-6 w-8 px-2 text-center border border-gray-300 dark:border-gray-600 rounded py-0 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
									/>
								</label>
							{/if}
							<label
								class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>
								Moving Avg N:
								<input
									type="text"
									min="2"
									max={campaignStats.length}
									bind:value={movingAvgN}
									class="h-6 w-8 px-2 text-center border border-gray-300 dark:border-gray-600 rounded py-0 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
								/>
							</label>
							<label
								class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>
								Log scale:
								<input type="checkbox" bind:checked={useLogScale} class="accent-blue-600" />
							</label>
							<div class="ml-auto flex items-center">
								<label
									class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
								>
									Relative metrics:
									<input
										type="checkbox"
										bind:checked={useRelativeMetrics}
										class="accent-blue-600"
									/>
								</label>
							</div>
						</div>
					</div>
					<div class="mb-8"></div>
					{#if campaignStats.length > 0}
						<div class="grid grid-cols-4 gap-2 sm:gap-4">
							{#each metrics as metric}
								<div class="text-center">
									<div class="flex items-center justify-center">
										<div
											class="w-3 h-3 rounded-full mr-2"
											style="background-color: {metric.color}"
										></div>
										<span
											class="text-sm font-medium text-gray-700 dark:text-gray-300 transition-colors duration-200"
											>{metric.label}</span
										>
									</div>
									<div class="mt-1">
										<span class="text-2xl font-bold" style="color: {metric.color}">
											{trendStats &&
											typeof trendStats[metric.key] === 'number' &&
											!isNaN(trendStats[metric.key])
												? trendStats[metric.key].toFixed(1) + '%'
												: '—'}
										</span>
									</div>
								</div>
							{/each}
						</div>
					{:else}
						<div
							class="text-center text-gray-400 dark:text-gray-500 text-sm py-4 transition-colors duration-200"
						>
							No trendline stats to dsplay (trendStats is null or not enough data).
						</div>
					{/if}
					<div class="mt-8 mb-6"></div>
					{#key chartKey}
						{#if containerReady}
							<div class="relative">
								<div
									bind:this={chartContainer}
									class="min-h-[220px] max-h-[280px] w-full box-border relative rounded-md bg-white dark:bg-gray-900 m-1 transition-colors duration-200"
									style="contain: layout style;"
								></div>
								<div
									bind:this={tooltip}
									class="absolute z-50 hidden bg-white dark:bg-gray-900 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700/60 max-w-xs transition-colors duration-200"
								></div>
							</div>
						{/if}
					{/key}
					<!-- DEBUG: Show moving average arrays for openRate and submissionRate
					<div class="text-xs text-red-600 mt-2">
						openRate movingAvg: {JSON.stringify(getMovingAverageData('openRate', movingAvgN))}
						<br />
						submissionRate movingAvg: {JSON.stringify(
							getMovingAverageData('submissionRate', movingAvgN)
						)}
					</div>
					 -->
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	:global(.chart-point) {
		cursor: pointer;
		transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
	}
	:global(.chart-point:hover) {
		r: 7;
		stroke-width: 4;
		filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.25)) !important;
	}
	:global(.chart-point-glow) {
		transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
	}
	:global(.chart-point:hover + .chart-point-glow) {
		r: 12;
		opacity: 0.4;
	}

	:global(.legend-item) {
		transition: all 0.2s ease;
	}
	:global(.legend-item:hover) {
		opacity: 0.8 !important;
	}
	:global(.value-label) {
		pointer-events: none;
		transition: opacity 0.3s ease;
	}
	:global(.campaign-trend-chart) {
		width: 100% !important;
		height: auto !important;
		max-width: 100% !important;
		overflow: hidden !important;
		box-sizing: border-box !important;
		display: block !important;
		contain: layout style !important;
		background: white;
		transition: background-color 0.2s ease;
	}
	:global(.dark .campaign-trend-chart) {
		background: #1f2937 !important;
	}
	:global(.line-enhanced) {
		stroke-width: 5 !important;
		opacity: 1 !important;
		filter: drop-shadow(0 0 4px #0006);
	}
	:global(.line-faded) {
		opacity: 0.2 !important;
	}
</style>
