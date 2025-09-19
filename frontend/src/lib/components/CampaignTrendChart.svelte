<script>
	import { onMount, onDestroy, tick } from 'svelte';
	let resizeObserver;

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

	let chartContainer;
	let sizingContainer;
	let width = 300;
	let height = 240; // Increased height for better label spacing
	let containerReady = false;

	// User controls for N
	let trendN = 4;
	let movingAvgN = 4;

	// Auto-adjust trendN based on available data
	$: {
		if (chartData.length > 0) {
			// If current trendN is larger than available data, adjust it
			if (trendN > chartData.length) {
				trendN = chartData.length;
			}
			// If trendN is less than 1, set it to 1 (minimum)
			if (trendN < 1) {
				trendN = 1;
			}
		}
	}

	// Legend visibility state
	let visibleMetrics = {
		openRate: true,
		clickRate: true,
		submissionRate: true,
		reportRate: true,
		'mavg-clickRate': true,
		'mavg-submissionRate': true,
		'mavg-reportRate': true
	};

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

	// Time range filter for campaigns
	const timeRanges = [
		{ label: 'Last 3 months', value: '3' },
		{ label: 'Last 6 months', value: '6' },
		{ label: 'Last 12 months', value: '12' },
		{ label: 'Last 24 months', value: '24' },
		{ label: 'All time', value: 'all' }
	];
	let selectedTimeRange = '12'; // default to last 12 months

	// Filtered campaigns based on selected time range (using sendStartAt or createdAt)
	$: filteredCampaignStats = (() => {
		if (!campaignStats || campaignStats.length === 0) return [];
		if (selectedTimeRange === 'all') return campaignStats;
		const range = Number(selectedTimeRange);
		const now = new Date();
		const cutoff = new Date(now.getFullYear(), now.getMonth() - range + 1, 1);
		return campaignStats.filter((c) => {
			const startDate = c.sendStartAt
				? new Date(c.sendStartAt)
				: c.createdAt
					? new Date(c.createdAt)
					: c.date instanceof Date
						? c.date
						: new Date(c.date);
			return startDate >= cutoff;
		});
	})();

	const metrics = [
		{ key: 'openRate', label: 'Read Rate', color: '#4cb5b5', suffix: '%' },
		{ key: 'clickRate', label: 'Click Rate', color: '#f96dcf', suffix: '%' },
		{ key: 'submissionRate', label: 'Submission Rate', color: '#f42e41', suffix: '%' },
		{ key: 'reportRate', label: 'Report Rate', color: '#1e40af', suffix: '%' }
	];

	// Toggle metric visibility
	function toggleMetric(metricKey) {
		visibleMetrics[metricKey] = !visibleMetrics[metricKey];
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

	// Update chartData to use filteredCampaignStats, using sendStartAt or createdAt as date, and sort by date ascending
	$: chartData = filteredCampaignStats
		.map((c) => ({
			...c,
			date: c.sendStartAt
				? new Date(c.sendStartAt)
				: c.createdAt
					? new Date(c.createdAt)
					: c.date instanceof Date
						? c.date
						: new Date(c.date),
			name: c.campaignName || c.name || c.title || ''
		}))
		.sort((a, b) => a.date.getTime() - b.date.getTime());

	// --- Force chart rerender ---
	let chartKey = '';
	$: chartKey = chartData
		.map((d) => d.name + (d.date instanceof Date ? d.date.toISOString() : d.date))
		.join('-');

	// --- Data processing ---
	$: chartData = processData(campaignStats);

	function processData(stats) {
		const sortedStats = [...(stats || [])]
			.filter((stat) => stat.campaignClosedAt)
			.sort(
				(a, b) => new Date(a.campaignClosedAt).getTime() - new Date(b.campaignClosedAt).getTime()
			)
			.slice(-12);

		return sortedStats.map((stat, index) => ({
			index: index + 1,
			date: new Date(stat.campaignClosedAt),
			name: stat.campaignName,
			// If your backend provides fractions (e.g., 0.425 for 42.5%), multiply by 100 below.
			// If it provides percentages (e.g., 42.5 for 42.5%), leave as is.
			openRate: Math.round((stat.openRate || 0) * (stat.openRate > 1 ? 1 : 100) * 10) / 10,
			clickRate: Math.round((stat.clickRate || 0) * (stat.clickRate > 1 ? 1 : 100) * 10) / 10,
			submissionRate:
				Math.round((stat.submissionRate || 0) * (stat.submissionRate > 1 ? 1 : 100) * 10) / 10,
			reportRate: Math.round((stat.reportRate || 0) * (stat.reportRate > 1 ? 1 : 100) * 10) / 10,
			totalRecipients: stat.totalRecipients
		}));
	}

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

		const xExtent = [0, chartData.length - 1];
		// Map 0-100% directly to chart boundaries so 0% aligns with X-axis
		const yExtent = [0, 100];

		xScale = createLinearScale(xExtent, [margin.left, width - margin.right]);
		yScale = createLinearScale(yExtent, [height - margin.bottom, margin.top]);

		createGridLines(svg);
		createAxes(svg);

		metrics.forEach((metric) => {
			if (visibleMetrics[metric.key]) {
				createLine(svg, metric);
			}
		});
		// Only draw moving average for clickRate, submissionRate, and reportRate, using user-selected N
		['clickRate', 'submissionRate', 'reportRate'].forEach((metricKey) => {
			const metric = metrics.find((m) => m.key === metricKey);
			if (metric && visibleMetrics[`mavg-${metricKey}`]) {
				createMovingAverageLine(svg, metric, movingAvgN);
			}
		});
		metrics.forEach((metric) => {
			if (visibleMetrics[metric.key]) {
				createDataPoints(svg, metric);
			}
		});
		createLegend(svg, svg);
		createTooltip(svg, svg.querySelectorAll('.chart-point'));

		// Add hover listeners for legend enhancement
		setupLegendHover(svg);
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
			document.documentElement.classList.contains('dark') ? '#374151' : '#f8f9fa'
		);
		stop1.setAttribute('stop-opacity', '0.3');

		const stop2 = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
		stop2.setAttribute('offset', '50%');
		stop2.setAttribute(
			'stop-color',
			document.documentElement.classList.contains('dark') ? '#4b5563' : '#e9ecef'
		);
		stop2.setAttribute('stop-opacity', '0.5');

		const stop3 = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
		stop3.setAttribute('offset', '100%');
		stop3.setAttribute(
			'stop-color',
			document.documentElement.classList.contains('dark') ? '#374151' : '#f8f9fa'
		);
		stop3.setAttribute('stop-opacity', '0.3');

		gradient.appendChild(stop1);
		gradient.appendChild(stop2);
		gradient.appendChild(stop3);
		defs.appendChild(gradient);
		svg.appendChild(defs);

		// Horizontal percentage reference lines
		for (let i = 0; i <= 5; i++) {
			const value = (100 / 5) * i;
			const y = yScale(value);
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
		}

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
				document.documentElement.classList.contains('dark') ? '#4b5563' : '#e5e7eb'
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
			document.documentElement.classList.contains('dark') ? '#9ca3af' : '#6B7280'
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
			document.documentElement.classList.contains('dark') ? '#9ca3af' : '#6B7280'
		);
		xAxis.setAttribute('stroke-width', '2');
		svg.appendChild(xAxis);

		for (let i = 0; i <= 5; i++) {
			const value = (100 / 5) * i;
			const y = yScale(value);
			const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
			text.setAttribute('x', (margin.left - 10).toString());
			text.setAttribute('y', (y - 2).toString());
			text.setAttribute('text-anchor', 'end');
			text.setAttribute('font-size', '11');
			text.setAttribute(
				'fill',
				document.documentElement.classList.contains('dark') ? '#d1d5db' : '#6B7280'
			);
			text.setAttribute('font-weight', '500');
			text.setAttribute('alignment-baseline', 'middle');
			text.textContent = `${value}%`;
			svg.appendChild(text);
		}

		chartData.forEach((d, i) => {
			if (i % Math.max(1, Math.floor(chartData.length / 6)) === 0) {
				const x = xScale(i);
				const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
				text.setAttribute('x', x.toString());
				text.setAttribute('y', (height - margin.bottom + 25).toString());
				text.setAttribute('text-anchor', 'middle');
				text.setAttribute('font-size', '11');
				text.setAttribute(
					'fill',
					document.documentElement.classList.contains('dark') ? '#f9fafb' : '#000'
				);
				text.setAttribute('alignment-baseline', 'middle');
				// Show date (YYYY/MM) and campaign name on the same line
				const labelSpan = document.createElementNS('http://www.w3.org/2000/svg', 'tspan');
				labelSpan.setAttribute('x', x.toString());
				labelSpan.setAttribute('dy', '0');
				if (d.date instanceof Date) {
					if (chartData.length > 5 || width < 1000) {
						// Only show month (MM) if there are more than 5 campaigns or chart is narrow
						labelSpan.textContent = `${String(d.date.getMonth() + 1).padStart(2, '0')}`;
					} else {
						labelSpan.textContent = `${d.date.getFullYear()}/${String(d.date.getMonth() + 1).padStart(2, '0')} ${d.name}`;
					}
				} else {
					labelSpan.textContent = d.name;
				}
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

			// Create glow effect
			const glowCircle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
			glowCircle.setAttribute('cx', x.toString());
			glowCircle.setAttribute('cy', y.toString());
			glowCircle.setAttribute('r', '8');
			glowCircle.setAttribute('fill', metric.color);
			glowCircle.setAttribute('opacity', '0.2');
			glowCircle.setAttribute('class', `chart-point-glow chart-point-glow-${metric.key}`);
			svg.appendChild(glowCircle);

			// Main circle with enhanced styling
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

			// Add value label above each point
			const value = point[metric.key] || 0;
			if (value > 0) {
				const valueLabel = document.createElementNS('http://www.w3.org/2000/svg', 'text');
				valueLabel.setAttribute('x', x.toString());
				valueLabel.setAttribute('y', (y - 12).toString());
				valueLabel.setAttribute('text-anchor', 'middle');
				valueLabel.setAttribute('font-size', '9');
				valueLabel.setAttribute('font-weight', '600');
				valueLabel.setAttribute('fill', metric.color);
				valueLabel.setAttribute('opacity', '0');
				valueLabel.setAttribute('class', `value-label value-label-${metric.key}-${i}`);
				valueLabel.textContent = `${Math.round(value)}`;
				valueLabel.style.transition = 'opacity 0.3s ease';
				svg.appendChild(valueLabel);
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
		const legendItems = svg.querySelectorAll('.legend-line, .legend-label');
		const allMainLines = svg.querySelectorAll('.main-line');
		const allMovingAvgLines = svg.querySelectorAll('.moving-average-line');
		legendItems.forEach((item) => {
			item.addEventListener('mouseenter', (e) => {
				let metric = e.target.getAttribute('data-metric');
				let isMavg = false;
				if (metric && metric.startsWith('mavg-')) {
					isMavg = true;
					metric = metric.replace('mavg-', '');
				}
				// Main lines
				allMainLines.forEach((line) => {
					if (!isMavg && line.classList.contains(`main-line-${metric}`)) {
						line.classList.add('line-enhanced');
						line.classList.remove('line-faded');
					} else {
						line.classList.add('line-faded');
						line.classList.remove('line-enhanced');
					}
				});
				// Moving average lines
				allMovingAvgLines.forEach((line) => {
					if (isMavg && line.classList.contains(`moving-average-${metric}`)) {
						line.classList.add('line-enhanced');
						line.classList.remove('line-faded');
					} else {
						line.classList.add('line-faded');
						line.classList.remove('line-enhanced');
					}
				});
			});
			item.addEventListener('mouseleave', () => {
				allMainLines.forEach((line) => {
					line.classList.remove('line-enhanced', 'line-faded');
				});
				allMovingAvgLines.forEach((line) => {
					line.classList.remove('line-enhanced', 'line-faded');
				});
			});
		});
	}

	function createTooltip(svg, points) {
		const tooltipGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g');
		tooltipGroup.setAttribute('class', 'tooltip-group');
		tooltipGroup.setAttribute('display', 'none');

		const tooltipRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
		tooltipRect.setAttribute('rx', '4');
		tooltipRect.setAttribute(
			'fill',
			document.documentElement.classList.contains('dark') ? '#111827' : '#1F2937'
		);
		tooltipRect.setAttribute(
			'stroke',
			document.documentElement.classList.contains('dark') ? '#374151' : '#4b5563'
		);
		tooltipRect.setAttribute('opacity', '0.95');

		const tooltipText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
		tooltipText.setAttribute('fill', 'white');
		tooltipText.setAttribute('font-size', '12');
		tooltipText.setAttribute('font-weight', '500');

		tooltipGroup.appendChild(tooltipRect);
		tooltipGroup.appendChild(tooltipText);
		svg.appendChild(tooltipGroup);

		points.forEach((point) => {
			point.addEventListener('mouseenter', (e) => {
				const index = parseInt(e.target.getAttribute('data-index'));
				const metricKey = e.target.getAttribute('data-metric');
				const data = chartData[index];

				// Show only the value label for the hovered metric
				const valueLabel = svg.querySelector(`.value-label-${metricKey}-${index}`);
				if (valueLabel) {
					valueLabel.setAttribute('opacity', '1');
				}

				while (tooltipText.firstChild) tooltipText.removeChild(tooltipText.firstChild);

				const labelSpan = document.createElementNS('http://www.w3.org/2000/svg', 'tspan');
				labelSpan.setAttribute('x', '10');
				labelSpan.setAttribute('dy', '15');
				if (data.date instanceof Date) {
					labelSpan.textContent = `${data.date.getFullYear()}/${String(data.date.getMonth() + 1).padStart(2, '0')} ${data.name}`;
				} else {
					labelSpan.textContent = data.name;
				}
				tooltipText.appendChild(labelSpan);

				metrics.forEach((metric, i) => {
					if (visibleMetrics[metric.key]) {
						const metricSpan = document.createElementNS('http://www.w3.org/2000/svg', 'tspan');
						metricSpan.setAttribute('x', '10');
						metricSpan.setAttribute('dy', '15');
						metricSpan.setAttribute('fill', metric.color);
						metricSpan.textContent = `${metric.label}: ${Math.round(data[metric.key] || 0)}`;
						tooltipText.appendChild(metricSpan);
					}
				});

				const recipientsSpan = document.createElementNS('http://www.w3.org/2000/svg', 'tspan');
				recipientsSpan.setAttribute('x', '10');
				recipientsSpan.setAttribute('dy', '15');
				recipientsSpan.textContent = `Recipients: ${data.totalRecipients}`;
				tooltipText.appendChild(recipientsSpan);

				const bbox = tooltipText.getBBox();
				tooltipRect.setAttribute('x', (bbox.x - 5).toString());
				tooltipRect.setAttribute('y', (bbox.y - 5).toString());
				tooltipRect.setAttribute('width', (bbox.width + 10).toString());
				tooltipRect.setAttribute('height', (bbox.height + 10).toString());

				const svgRect = svg.getBoundingClientRect();
				const x = parseFloat(e.target.getAttribute('cx'));
				const y = parseFloat(e.target.getAttribute('cy'));
				const tooltipWidth = bbox.width + 10;
				let tooltipX = x + 10;
				// If tooltip would overflow right edge, show to the left
				if (tooltipX + tooltipWidth > width - 10) {
					tooltipX = x - tooltipWidth - 10;
				}
				// Prevent tooltip from being cut off at the top
				let tooltipY = y - 60;
				if (tooltipY < 0) {
					tooltipY = y + 10;
				}
				tooltipGroup.setAttribute('transform', `translate(${tooltipX}, ${tooltipY})`);
				tooltipGroup.setAttribute('display', 'block');
			});

			point.addEventListener('mouseleave', (e) => {
				const index = parseInt(e.target.getAttribute('data-index'));
				const metricKey = e.target.getAttribute('data-metric');

				// Hide the value label for this metric
				const valueLabel = svg.querySelector(`.value-label-${metricKey}-${index}`);
				if (valueLabel) {
					valueLabel.setAttribute('opacity', '0');
				}

				tooltipGroup.setAttribute('display', 'none');
			});
		});
	}

	onMount(async () => {
		await tick(); // Wait for DOM/layout
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
		if (loadingTimeout) {
			clearTimeout(loadingTimeout);
		}
		if (pendingTimeout) {
			clearTimeout(pendingTimeout);
		}
	});

	// Only create chart when width is set and chartData is ready
	$: if (containerReady && chartContainer && width > 0 && chartData.length > 1 && movingAvgN) {
		createChart();
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
			<span class="text-gray-400 text-sm">Preparing chart…</span>
		</div>
	{:else}
		<div>
			{#if debouncedIsLoading}
				<div class="flex items-center justify-center h-64">
					<div
						class="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 dark:border-blue-400"
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
			{:else if hasAttemptedLoad && !isLoading && !debouncedIsLoading && chartData.length === 0}
				<div
					class="flex items-center justify-center h-64 bg-gray-50 dark:bg-gray-800 rounded-lg transition-colors duration-200"
				>
					<div class="text-center">
						<svg
							class="mx-auto h-12 w-12 text-gray-400 dark:text-gray-500 transition-colors duration-200"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="1.5"
								d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
							/>
						</svg>
						<h3
							class="mt-2 text-sm font-medium text-gray-900 dark:text-gray-200 transition-colors duration-200"
						>
							No campaign data
						</h3>
						<p class="mt-1 text-sm text-gray-500 dark:text-gray-400 transition-colors duration-200">
							Campaign statistics will appear here once campaigns are completed.
						</p>
					</div>
				</div>
			{:else if hasAttemptedLoad && !isLoading && !debouncedIsLoading && chartData.length === 1}
				<div
					class="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-6 transition-colors duration-200"
				>
					<div class="flex items-center">
						<svg
							class="h-6 w-6 text-blue-600 dark:text-blue-400 mr-2 transition-colors duration-200"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
							/>
						</svg>
						<div>
							<h4
								class="text-sm font-medium text-blue-900 dark:text-blue-200 transition-colors duration-200"
							>
								Single Campaign Data
							</h4>
							<p class="text-sm text-blue-700 dark:text-blue-300 transition-colors duration-200">
								Trends will appear when you have 2 or more completed campaigns.
							</p>
						</div>
					</div>
					<div class="grid grid-cols-4 gap-4">
						<div class="text-center">
							<div
								class="text-2xl font-bold text-blue-600 dark:text-blue-400 transition-colors duration-200"
							>
								{chartData[0].openRate}%
							</div>
							<div class="text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200">
								Open Rate
							</div>
						</div>
						<div class="text-center">
							<div
								class="text-2xl font-bold text-green-600 dark:text-green-400 transition-colors duration-200"
							>
								{chartData[0].clickRate}%
							</div>
							<div class="text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200">
								Click Rate
							</div>
						</div>
						<div class="text-center">
							<div
								class="text-2xl font-bold text-yellow-600 dark:text-yellow-400 transition-colors duration-200"
							>
								{chartData[0].submissionRate}%
							</div>
							<div class="text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200">
								Submission Rate
							</div>
						</div>
						<div class="text-center">
							<div
								class="text-2xl font-bold text-indigo-600 dark:text-indigo-400 transition-colors duration-200"
							>
								{chartData[0].reportRate}%
							</div>
							<div class="text-sm text-gray-600 dark:text-gray-400 transition-colors duration-200">
								Report Rate
							</div>
						</div>
					</div>
				</div>
			{:else if hasAttemptedLoad && !isLoading && !debouncedIsLoading && chartData.length >= 2}
				<div
					class="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-600 p-6 transition-colors duration-200"
				>
					<!-- Trendline stats and controls above chart -->
					<div class="flex flex-row items-center justify-between mb-2 pb-0 flex-wrap gap-2">
						<h4
							class="text-sm font-medium text-gray-600 dark:text-gray-300 m-0 transition-colors duration-200"
						>
							Trendline: Last {trendStats ? trendStats.n : chartData.length} Campaigns (average)
						</h4>
						<div class="flex flex-wrap items-center gap-2 mb-0">
							<label
								class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>
								Time range:
								<select
									bind:value={selectedTimeRange}
									class="border border-gray-300 dark:border-gray-600 rounded px-1 py-0 text-xs bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
									style="height: 1.5rem;"
								>
									{#each timeRanges as range}
										<option value={range.value}>{range.label}</option>
									{/each}
								</select>
							</label>
							{#if chartData.length > 1}
								<label
									class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
								>
									Trendline N:
									<input
										type="number"
										min="1"
										max={chartData.length}
										bind:value={trendN}
										class="border border-gray-300 dark:border-gray-600 rounded px-1 py-0 w-10 text-xs bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
										style="height: 1.5rem;"
									/>
								</label>
							{/if}
							<label
								class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
							>
								Moving Avg N:
								<input
									type="number"
									min="2"
									max={chartData.length}
									bind:value={movingAvgN}
									class="border border-gray-300 dark:border-gray-600 rounded px-1 py-0 w-10 text-xs bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 transition-colors duration-200"
									style="height: 1.5rem;"
								/>
							</label>
						</div>
					</div>
					<div style="height: 1.25rem;"></div>
					{#if chartData.length > 0}
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
							No trendline stats to display (trendStats is null or not enough data).
						</div>
					{/if}
					{#key chartKey}
						{#if containerReady}
							<div
								bind:this={chartContainer}
								class="min-h-[220px] max-h-[280px] w-full box-border relative rounded-md bg-white dark:bg-gray-800 m-1 transition-colors duration-200"
								style="contain: layout style;"
							></div>
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
		border-radius: 8px;
		padding: 8px;
		transition: background-color 0.2s ease;
	}
	:global(.dark .campaign-trend-chart) {
		background: #1f2937 !important;
	}
	:global(.line-enhanced) {
		stroke-width: 5 !important;
		opacity: 1 !important;
		filter: drop-shadow(0 0 4px #0006);
		mix-blend-mode: multiply;
	}
	:global(.line-faded) {
		opacity: 0.2 !important;
	}
</style>
