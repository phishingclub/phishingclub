<script>
	import { onMount } from 'svelte';
	import {
		format,
		addMonths,
		subMonths,
		addWeeks,
		subWeeks,
		startOfMonth,
		endOfMonth,
		startOfWeek,
		endOfWeek,
		startOfDay,
		endOfDay,
		addDays,
		subDays,
		isSameDay,
		isSameMonth,
		getDay,
		getDaysInMonth
	} from 'date-fns';
	import { scrollBarClassesHorizontal, scrollBarClassesVertical } from '$lib/utils/scrollbar';

	// localStorage keys
	const STORAGE_KEY_VIEW_MODE = 'calendar_view_mode';
	const STORAGE_KEY_START_FROM_TODAY = 'calendar_start_from_today';
	const STORAGE_KEY_FILTERS = 'calendar_filters';
	const STORAGE_KEY_WEEK_STARTS_ON = 'calendar_week_starts_on';

	/** @type * */
	export let campaigns = [];
	/** @type {Date} */
	export let start = startOfMonth(new Date());
	/** @type {Date} */
	export let end = endOfMonth(new Date());

	export let onChangeDate;

	/** @type {boolean} - when true, shows company name on campaign items (for global context) */
	export let showCompany = false;

	let container;
	let currentDate = new Date();
	let isLoadingNewMonth = false;
	let weeks = [];
	let isInitialized = false;

	// view options with localStorage persistence
	/** @type {'month' | 'week'} */
	let viewMode = 'month';
	let startFromToday = false;
	/** @type {0 | 1} - 0 = Sunday, 1 = Monday */
	let weekStartsOn = 0;

	let activeFilters = {
		SCHEDULED: true,
		ACTIVE: true,
		COMPLETED: true,
		SELF_MANAGED: true
	};

	let isGeneratingCalendar = false;

	// use consistent colors that match the design system - these work well in both light and dark modes
	const COLORS = {
		SCHEDULED: '#62aded', // campaign-scheduled
		ACTIVE: '#5557f6', // campaign-active
		COMPLETED: '#4cb5b5', // message-read - much more muted than bright green
		SELF_MANAGED: '#7C3AED' // darker purple
	};

	// load settings from localStorage
	function loadSettings() {
		try {
			const storedViewMode = localStorage.getItem(STORAGE_KEY_VIEW_MODE);
			if (storedViewMode === 'month' || storedViewMode === 'week') {
				viewMode = storedViewMode;
			}

			const storedStartFromToday = localStorage.getItem(STORAGE_KEY_START_FROM_TODAY);
			if (storedStartFromToday !== null) {
				startFromToday = storedStartFromToday === 'true';
			}

			const storedFilters = localStorage.getItem(STORAGE_KEY_FILTERS);
			if (storedFilters) {
				const parsed = JSON.parse(storedFilters);
				activeFilters = { ...activeFilters, ...parsed };
			}

			const storedWeekStartsOn = localStorage.getItem(STORAGE_KEY_WEEK_STARTS_ON);
			if (storedWeekStartsOn === '0' || storedWeekStartsOn === '1') {
				weekStartsOn = /** @type {0 | 1} */ (parseInt(storedWeekStartsOn));
			}
		} catch (e) {
			console.warn('failed to load calendar settings from localStorage', e);
		}
	}

	// save settings to localStorage
	function saveViewMode(mode) {
		viewMode = mode;
		try {
			localStorage.setItem(STORAGE_KEY_VIEW_MODE, mode);
		} catch (e) {
			console.warn('failed to save view mode to localStorage', e);
		}
	}

	function saveStartFromToday(value) {
		startFromToday = value;
		try {
			localStorage.setItem(STORAGE_KEY_START_FROM_TODAY, String(value));
		} catch (e) {
			console.warn('failed to save startFromToday to localStorage', e);
		}
	}

	function saveFilters() {
		try {
			localStorage.setItem(STORAGE_KEY_FILTERS, JSON.stringify(activeFilters));
		} catch (e) {
			console.warn('failed to save filters to localStorage', e);
		}
	}

	/** @param {0 | 1} value */
	function saveWeekStartsOn(value) {
		weekStartsOn = value;
		try {
			localStorage.setItem(STORAGE_KEY_WEEK_STARTS_ON, String(value));
		} catch (e) {
			console.warn('failed to save weekStartsOn to localStorage', e);
		}
	}

	/** @param {0 | 1} value */
	async function handleWeekStartsOnChange(value) {
		saveWeekStartsOn(value);
		await generateCalendarData();
	}

	function sortCampaignsByPriority(campaigns, day) {
		const dayTime = startOfDay(day).getTime();

		// helper function to get campaign type priority
		function getTypePriority(campaign) {
			if (campaign.sendStartAt && campaign.sendStartAt > new Date().toISOString()) {
				return 0; // scheduled gets highest priority
			}
			if (campaign.sendStartAt && !campaign.anonymizedAt && !campaign.closedAt) {
				return 1; // active gets second priority
			}
			if (!campaign.sendStartAt || !campaign.sendEndAt) {
				return 2; // self-managed gets third priority
			}
			return 3; // completed gets lowest priority
		}

		return campaigns.sort((a, b) => {
			// 1. absolute highest priority: campaign's sendStartAt is on this day
			const aSendStartTime = a.sendStartAt ? startOfDay(new Date(a.sendStartAt)).getTime() : null;
			const bSendStartTime = b.sendStartAt ? startOfDay(new Date(b.sendStartAt)).getTime() : null;

			if (aSendStartTime === dayTime && bSendStartTime !== dayTime) return -1;
			if (bSendStartTime === dayTime && aSendStartTime !== dayTime) return 1;

			// 2. second priority: type of campaign (scheduled > active > self-managed > completed)
			const aTypePriority = getTypePriority(a);
			const bTypePriority = getTypePriority(b);
			if (aTypePriority !== bTypePriority) {
				return aTypePriority - bTypePriority;
			}

			// 3. if same type, sort by sendStartAt date (if exists) or start date
			const aTime = aSendStartTime || startOfDay(a.start).getTime();
			const bTime = bSendStartTime || startOfDay(b.start).getTime();
			return aTime - bTime;
		});
	}

	function sortByName(campaigns) {
		return [...campaigns].sort((a, b) => a.name.localeCompare(b.name));
	}

	function updateDateRange() {
		if (viewMode === 'week') {
			// week view
			let weekStart;
			if (startFromToday) {
				// start from today, show 7 days forward
				weekStart = startOfDay(currentDate);
			} else {
				// start from beginning of the week containing currentDate
				weekStart = startOfWeek(currentDate, { weekStartsOn });
			}
			const weekEnd = addDays(weekStart, 6);

			start = weekStart;
			end = endOfDay(weekEnd);
		} else {
			// month view
			if (startFromToday && isSameMonth(currentDate, new Date())) {
				// if viewing current month and startFromToday is enabled, start from today
				const today = startOfDay(new Date());

				// end at the last day of next month to show more upcoming campaigns
				const nextMonth = addDays(endOfMonth(currentDate), 1);
				const lastDayOfNextMonth = endOfMonth(nextMonth);
				const lastDayOfWeek = getDay(lastDayOfNextMonth);
				const calendarEnd = addDays(lastDayOfNextMonth, 6 - lastDayOfWeek);

				start = today;
				end = calendarEnd;
			} else {
				// standard month view
				// calculate first day of calendar (might be in previous month)
				let calendarStart = startOfMonth(currentDate);
				const firstDayOfWeek = getDay(calendarStart);
				if (firstDayOfWeek > 0) {
					calendarStart = subDays(calendarStart, firstDayOfWeek);
				}

				// calculate last day of calendar (might be in next month)
				const lastDayOfMonth = endOfMonth(currentDate);
				const lastDayOfWeek = getDay(lastDayOfMonth);
				const calendarEnd = addDays(lastDayOfMonth, 6 - lastDayOfWeek);

				start = calendarStart;
				end = calendarEnd;
			}
		}
	}

	/** @type * */
	$: calendarCampaigns = campaigns.map((campaign) => {
		const campaignStart = campaign.sendStartAt
			? new Date(campaign.sendStartAt)
			: new Date(campaign.createdAt);

		let endDate = null;

		// 1. priority when it was closed
		if (campaign.closedAt) {
			endDate = new Date(campaign.closedAt);
		}
		// 2. .. when it should close
		else if (campaign.closeAt) {
			endDate = new Date(campaign.closeAt);
		}
		// 3.  .. when it will be anonymized (also close)
		else if (campaign.anonymizeAt) {
			endDate = new Date(campaign.anonymizeAt);
			// 4. .. if not self-managed then end delivery time
		} else if (campaign.sendEndAt) {
			endDate = new Date(campaign.sendEndAt);
		} else {
			// .. else the campaign runs endlessly into the future
			const farFuture = new Date(campaignStart);
			farFuture.setFullYear(farFuture.getFullYear() + 420); // 420 years in the future
			endDate = farFuture;
		}

		const c = {
			...campaign,
			isSelfManaged: !campaign.sendStartAt || !campaign.sendEndAt,
			start: campaignStart,
			end: endDate,
			color: getCampaignColor(campaign)
		};
		return c;
	});

	function getCampaignColor(campaign) {
		if (campaign.anonymizedAt || campaign.closedAt) {
			return COLORS.COMPLETED;
		}
		if (!campaign.sendStartAt) {
			return COLORS.SELF_MANAGED;
		}
		if (campaign.sendStartAt && campaign.sendStartAt > new Date().toISOString()) {
			return COLORS.SCHEDULED;
		}
		return COLORS.ACTIVE;
	}

	function truncateText(text, maxLength) {
		if (!text) return '';
		if (text.length <= maxLength) return text;
		return text.substring(0, maxLength - 2) + '...';
	}

	function formatDateString(date) {
		return format(date, 'MMMM dd, yyyy');
	}

	function calculateCampaignLayers(campaigns, calendarStart, calendarEnd) {
		const dayMap = new Map();

		// generate days
		let currentDay = new Date(calendarStart);
		while (currentDay < calendarEnd) {
			dayMap.set(currentDay.getTime(), []);
			currentDay = addDays(currentDay, 1);
		}

		// filter campaigns based on active filters
		const filteredCampaigns = campaigns.filter((campaign) => {
			if (campaign.anonymizedAt || campaign.closedAt) {
				return activeFilters.COMPLETED;
			}
			if (!campaign.sendStartAt) {
				return activeFilters.SELF_MANAGED;
			}
			if (campaign.sendStartAt && campaign.sendStartAt > new Date().toISOString()) {
				return activeFilters.SCHEDULED;
			}
			return activeFilters.ACTIVE;
		});

		// map filtered campaigns to days
		filteredCampaigns.forEach((campaign) => {
			const campaignStart = startOfDay(campaign.start);
			const campaignEnd = startOfDay(campaign.end);

			let day = new Date(Math.max(campaignStart.getTime(), calendarStart.getTime()));
			const endDay = new Date(Math.min(addDays(campaignEnd, 1).getTime(), calendarEnd.getTime()));

			while (day < endDay) {
				const dayTime = day.getTime();
				if (dayMap.has(dayTime)) {
					dayMap.get(dayTime).push(campaign);
				}
				day = addDays(day, 1);
			}
		});

		// sort campaigns for each day
		const visibleDayMap = new Map();

		dayMap.forEach((dayCampaigns, dayTime) => {
			const sortedCampaigns = sortCampaignsByPriority(dayCampaigns, new Date(Number(dayTime)));

			visibleDayMap.set(dayTime, {
				campaigns: sortedCampaigns,
				total: sortedCampaigns.length
			});
		});

		return visibleDayMap;
	}

	async function generateCalendarData() {
		isGeneratingCalendar = true;

		updateDateRange();

		const dayMap = calculateCampaignLayers(calendarCampaigns, start, end);

		// create weeks array
		weeks = [];
		let week = [];
		let currentDateIter = new Date(start);

		while (currentDateIter <= end) {
			// get campaign data for this day
			const dayTime = currentDateIter.getTime();
			const dayData = dayMap.get(dayTime) || { campaigns: [], total: 0 };

			// sort campaigns
			const sortedCampaigns = sortByName(dayData.campaigns);

			// determine if this day should be highlighted based on view mode
			let isInCurrentPeriod;
			if (viewMode === 'week') {
				isInCurrentPeriod = true; // in week view, all days are "current"
			} else if (startFromToday && isSameMonth(currentDate, new Date())) {
				// when starting from today, current month and next month are both "current"
				const today = new Date();
				isInCurrentPeriod =
					isSameMonth(currentDateIter, today) ||
					isSameMonth(currentDateIter, addDays(endOfMonth(today), 1));
			} else {
				isInCurrentPeriod = isSameMonth(currentDateIter, currentDate);
			}

			// determine if day is in next month (for visual separation when "start from today" is enabled)
			const isNextMonth =
				startFromToday &&
				viewMode === 'month' &&
				isSameMonth(currentDate, new Date()) &&
				!isSameMonth(currentDateIter, new Date());

			// create day object
			const day = {
				date: new Date(currentDateIter),
				isToday: isSameDay(currentDateIter, new Date()),
				isCurrentMonth: isInCurrentPeriod,
				isNextMonth,
				campaigns: sortedCampaigns,
				totalCampaigns: dayData.total
			};

			week.push(day);

			// start a new week if we've filled one
			if (week.length === 7) {
				weeks.push(week);
				week = [];
			}

			// move to next day
			currentDateIter = addDays(currentDateIter, 1);
		}

		// for week view, we might have a partial week
		if (week.length > 0) {
			weeks.push(week);
		}

		isGeneratingCalendar = false;
	}

	async function navigatePrevious() {
		isLoadingNewMonth = true;
		if (viewMode === 'week') {
			currentDate = subWeeks(currentDate, 1);
		} else {
			currentDate = subMonths(currentDate, 1);
		}
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function navigateNext() {
		isLoadingNewMonth = true;
		if (viewMode === 'week') {
			currentDate = addWeeks(currentDate, 1);
		} else {
			currentDate = addMonths(currentDate, 1);
		}
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function goToToday() {
		isLoadingNewMonth = true;
		currentDate = new Date();
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function handleViewModeChange(mode) {
		if (mode === viewMode) return;
		isLoadingNewMonth = true;
		saveViewMode(mode);
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function handleStartFromTodayChange(event) {
		const checked = event.target.checked;
		isLoadingNewMonth = true;
		saveStartFromToday(checked);
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function toggleFilter(key) {
		activeFilters[key] = !activeFilters[key];
		saveFilters();
		await generateCalendarData();
	}

	/**
	 * builds the tooltip text for a campaign
	 * @param {*} campaign
	 */
	function buildTooltip(campaign) {
		const status = campaign.isSelfManaged
			? 'Self-managed'
			: campaign.end < new Date()
				? 'Completed'
				: campaign.start <= new Date()
					? 'Active'
					: 'Scheduled';

		const dateRange = campaign.isSelfManaged
			? `${formatDateString(new Date(campaign.createdAt))} - ?`
			: `${formatDateString(campaign.start)} - ${formatDateString(campaign.end)}`;

		let tooltip = `${campaign.name} - ${status}, ${dateRange}`;

		if (showCompany && campaign.company?.name) {
			tooltip = `[${campaign.company.name}] ${tooltip}`;
		}

		return tooltip;
	}

	/**
	 * formats the header title based on view mode
	 */
	function getHeaderTitle() {
		if (viewMode === 'week') {
			const weekEnd = addDays(start, 6);
			if (isSameMonth(start, weekEnd)) {
				return `${format(start, 'MMMM d')} - ${format(weekEnd, 'd, yyyy')}`;
			} else if (start.getFullYear() === weekEnd.getFullYear()) {
				return `${format(start, 'MMM d')} - ${format(weekEnd, 'MMM d, yyyy')}`;
			} else {
				return `${format(start, 'MMM d, yyyy')} - ${format(weekEnd, 'MMM d, yyyy')}`;
			}
		}
		// when starting from today in month view, show the date range
		if (startFromToday && isSameMonth(currentDate, new Date())) {
			const today = new Date();
			const nextMonthEnd = endOfMonth(addDays(endOfMonth(today), 1));
			if (today.getFullYear() === nextMonthEnd.getFullYear()) {
				return `${format(today, 'MMM d')} - ${format(nextMonthEnd, 'MMM d, yyyy')}`;
			} else {
				return `${format(today, 'MMM d, yyyy')} - ${format(nextMonthEnd, 'MMM d, yyyy')}`;
			}
		}
		return format(currentDate, 'MMMM yyyy');
	}

	onMount(async () => {
		loadSettings();
		await generateCalendarData();
		isInitialized = true;
	});

	$: if (calendarCampaigns && isInitialized) {
		generateCalendarData();
	}
</script>

<div
	class="w-full bg-white dark:bg-gray-900/80 rounded-lg shadow-sm p-2 sm:p-4 border border-gray-200 dark:border-gray-700/60 transition-colors duration-200"
>
	<div class="space-y-3 sm:space-y-4 min-h-[400px] sm:min-h-[600px] max-w-[1600px] mx-auto">
		<!-- Controls Row -->
		<div class="flex flex-col gap-3 px-1 sm:px-2">
			<!-- Top row: Navigation (centered on mobile, left on desktop) -->
			<div class="flex items-center justify-center sm:justify-between gap-2 order-1 sm:order-2">
				<!-- Left: View Mode & Options (hidden on mobile, shown in second row) -->
				<div class="hidden sm:flex flex-wrap items-center gap-3">
					<label
						class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
					>
						View:
						<select
							value={viewMode}
							on:change={(e) =>
								handleViewModeChange(/** @type {HTMLSelectElement} */ (e.target).value)}
							class="border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
						>
							<option value="month">Month</option>
							<option value="week">Week</option>
						</select>
					</label>

					<label
						class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
					>
						Start from today:
						<input
							type="checkbox"
							checked={startFromToday}
							on:change={handleStartFromTodayChange}
							class="h-4 w-4 border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-900 text-blue-600 accent-blue-600 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
						/>
					</label>

					{#if viewMode === 'week' && !startFromToday}
						<label
							class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300 transition-colors duration-200"
						>
							Week starts:
							<select
								value={weekStartsOn}
								on:change={(e) =>
									handleWeekStartsOnChange(
										/** @type {0 | 1} */ (
											parseInt(/** @type {HTMLSelectElement} */ (e.target).value)
										)
									)}
								class="border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
							>
								<option value={0}>Sunday</option>
								<option value={1}>Monday</option>
							</select>
						</label>
					{/if}
				</div>

				<!-- Center: Navigation -->
				<div class="flex items-center gap-1 sm:gap-2">
					<button
						class="p-2 sm:p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800/60 transition-colors duration-200"
						on:click={navigatePrevious}
						disabled={isLoadingNewMonth}
						title="Previous {viewMode}"
					>
						<svg
							xmlns="http://www.w3.org/2000/svg"
							class="h-5 w-5 sm:h-4 sm:w-4 text-gray-600 dark:text-gray-400"
							fill="none"
							viewBox="0 0 24 24"
							stroke="currentColor"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M15 19l-7-7 7-7"
							/>
						</svg>
					</button>

					<button
						class="px-3 py-1 sm:px-2 sm:py-0.5 text-xs font-medium text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200 border border-gray-300 dark:border-gray-600 hover:border-cta-blue dark:hover:border-highlight-blue rounded transition-colors duration-200"
						on:click={goToToday}
						disabled={isLoadingNewMonth}
					>
						Today
					</button>

					<h2
						class="text-xs sm:text-sm font-semibold text-gray-900 dark:text-gray-300 min-w-[140px] sm:min-w-[180px] text-center"
					>
						{getHeaderTitle()}
					</h2>

					<button
						class="p-2 sm:p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800/60 transition-colors duration-200"
						on:click={navigateNext}
						disabled={isLoadingNewMonth}
						title="Next {viewMode}"
					>
						<svg
							xmlns="http://www.w3.org/2000/svg"
							class="h-5 w-5 sm:h-4 sm:w-4 text-gray-600 dark:text-gray-400"
							fill="none"
							viewBox="0 0 24 24"
							stroke="currentColor"
						>
							<path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M9 5l7 7-7 7"
							/>
						</svg>
					</button>
				</div>

				<!-- Right: Legend/Filters (hidden on mobile) -->
				<div class="hidden sm:flex flex-wrap items-center gap-3">
					{#each [{ key: 'SCHEDULED', color: COLORS.SCHEDULED, label: 'Scheduled' }, { key: 'ACTIVE', color: COLORS.ACTIVE, label: 'Active' }, { key: 'COMPLETED', color: COLORS.COMPLETED, label: 'Completed' }, { key: 'SELF_MANAGED', color: COLORS.SELF_MANAGED, label: 'Self-managed' }] as item}
						<button
							class="flex items-center cursor-pointer select-none hover:opacity-80 transition-opacity duration-200 text-xs text-gray-700 dark:text-gray-300"
							on:click={() => toggleFilter(item.key)}
						>
							<div
								class="w-2.5 h-2.5 rounded-full mr-1.5 transition-opacity duration-200"
								style="background-color: {item.color}; opacity: {activeFilters[item.key]
									? '1'
									: '0.3'}"
							></div>
							<span
								class="transition-opacity duration-200"
								style="opacity: {activeFilters[item.key] ? '1' : '0.5'}"
							>
								{item.label}
							</span>
						</button>
					{/each}
				</div>
			</div>

			<!-- Mobile controls row -->
			<div class="flex sm:hidden flex-wrap items-center justify-center gap-x-4 gap-y-2 order-2">
				<label class="flex items-center gap-1.5 text-xs text-gray-700 dark:text-gray-300">
					<select
						value={viewMode}
						on:change={(e) =>
							handleViewModeChange(/** @type {HTMLSelectElement} */ (e.target).value)}
						class="border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200"
					>
						<option value="month">Month</option>
						<option value="week">Week</option>
					</select>
				</label>

				<label class="flex items-center gap-1 text-xs text-gray-700 dark:text-gray-300">
					Today:
					<input
						type="checkbox"
						checked={startFromToday}
						on:change={handleStartFromTodayChange}
						class="h-4 w-4 border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-900 text-blue-600 accent-blue-600 hover:border-cta-blue dark:hover:border-highlight-blue focus:border-cta-blue dark:focus:border-highlight-blue transition-colors duration-200"
					/>
				</label>

				{#if viewMode === 'week' && !startFromToday}
					<label class="flex items-center gap-1.5 text-xs text-gray-700 dark:text-gray-300">
						<select
							value={weekStartsOn}
							on:change={(e) =>
								handleWeekStartsOnChange(
									/** @type {0 | 1} */ (parseInt(/** @type {HTMLSelectElement} */ (e.target).value))
								)}
							class="border border-gray-300 dark:border-gray-600 rounded px-2 py-1 text-xs bg-white dark:bg-gray-900 text-gray-700 dark:text-gray-200"
						>
							<option value={0}>Sun</option>
							<option value={1}>Mon</option>
						</select>
					</label>
				{/if}
			</div>

			<!-- Mobile filters row -->
			<div class="flex sm:hidden flex-wrap items-center justify-center gap-x-3 gap-y-1 order-3">
				{#each [{ key: 'SCHEDULED', color: COLORS.SCHEDULED, label: 'Sch' }, { key: 'ACTIVE', color: COLORS.ACTIVE, label: 'Act' }, { key: 'COMPLETED', color: COLORS.COMPLETED, label: 'Done' }, { key: 'SELF_MANAGED', color: COLORS.SELF_MANAGED, label: 'Self' }] as item}
					<button
						class="flex items-center cursor-pointer select-none hover:opacity-80 transition-opacity duration-200 text-[10px] text-gray-700 dark:text-gray-300"
						on:click={() => toggleFilter(item.key)}
					>
						<div
							class="w-2 h-2 rounded-full mr-1 transition-opacity duration-200"
							style="background-color: {item.color}; opacity: {activeFilters[item.key]
								? '1'
								: '0.3'}"
						></div>
						<span
							class="transition-opacity duration-200"
							style="opacity: {activeFilters[item.key] ? '1' : '0.5'}"
						>
							{item.label}
						</span>
					</button>
				{/each}
			</div>
		</div>

		<!-- Calendar Grid -->
		<div class="calendar-container" class:week-view={viewMode === 'week'}>
			<!-- Day headers -->
			{#if !(startFromToday && viewMode === 'month' && isSameMonth(currentDate, new Date()))}
				{@const dayHeaders =
					viewMode === 'week' && !startFromToday && weekStartsOn === 1
						? ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
						: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']}
				{@const dayHeadersShort =
					viewMode === 'week' && !startFromToday && weekStartsOn === 1
						? ['M', 'T', 'W', 'T', 'F', 'S', 'S']
						: ['S', 'M', 'T', 'W', 'T', 'F', 'S']}
				<div class="grid grid-cols-7 text-center mb-1">
					{#each dayHeaders as day, i}
						<div
							class="text-[10px] sm:text-sm font-medium text-gray-600 dark:text-gray-400 py-1 sm:py-2"
						>
							<span class="hidden sm:inline">{day}</span>
							<span class="sm:hidden">{dayHeadersShort[i]}</span>
						</div>
					{/each}
				</div>
			{/if}

			<!-- Calendar grid -->
			{#if !isInitialized || isGeneratingCalendar || isLoadingNewMonth}
				<div class="min-h-[450px] flex items-center justify-center">
					<div class="flex items-center space-x-2">
						<div
							class="animate-spin rounded-full h-6 w-6 border-b-2 border-blue-600 dark:border-highlight-blue/80"
						></div>
						<span class="text-gray-600 dark:text-gray-400">Loading calendar...</span>
					</div>
				</div>
			{:else}
				<div class="grid gap-1 min-h-[450px]" style="grid-template-columns: repeat(7, 1fr);">
					{#each weeks as week, weekIndex}
						{#each week as day}
							<div
								class="calendar-day relative border rounded-md overflow-hidden {scrollBarClassesHorizontal} {day.isToday
									? 'border-blue-500 dark:border-blue-400 bg-blue-50/50 dark:bg-blue-900/20 ring-1 ring-blue-500/30 dark:ring-blue-400/30'
									: 'border-gray-200 dark:border-gray-700/60'}
										{day.isToday
									? ''
									: day.isCurrentMonth && !day.isNextMonth
										? 'bg-white dark:bg-gray-900/60'
										: 'bg-gray-50 dark:bg-gray-800/40'} transition-colors duration-200"
							>
								<!-- date number -->
								<div
									class="date-header text-xs sm:text-sm px-1 sm:px-2 py-0.5 sm:py-1 {day.isToday
										? 'font-bold text-blue-600 dark:text-blue-400'
										: day.isCurrentMonth && !day.isNextMonth
											? 'text-gray-600 dark:text-gray-400'
											: 'text-gray-400 dark:text-gray-600'}"
								>
									{day.date.getDate()}
								</div>
								<!-- campaigns list -->
								<div class="campaign-container {scrollBarClassesVertical}">
									{#each day.campaigns as campaign}
										<a
											href={`/campaign/${campaign.id}`}
											class="campaign-item group flex items-start gap-1 sm:gap-2 mb-1 sm:mb-1.5 rounded px-1 sm:px-2 py-1 sm:py-1.5 overflow-hidden transition-all duration-150 hover:scale-[1.01] hover:shadow-sm bg-white/70 dark:bg-gray-800/50 border border-gray-200/70 dark:border-gray-700/40 hover:border-gray-300 dark:hover:border-gray-600"
											title={buildTooltip(campaign)}
										>
											<div
												class="status-indicator flex-shrink-0 w-0.5 sm:w-1 self-stretch rounded-full"
												style="background-color: {campaign.color};"
											></div>
											<div class="flex-1 min-w-0">
												{#if showCompany && campaign.company?.name}
													<div
														class="company-name truncate text-gray-500 dark:text-gray-400 text-[8px] sm:text-[10px] font-medium uppercase tracking-wide leading-tight"
													>
														{truncateText(campaign.company.name, 22)}
													</div>
												{/if}
												<div
													class="campaign-name truncate text-[10px] sm:text-xs leading-snug font-medium text-gray-700 dark:text-gray-300 group-hover:text-gray-900 dark:group-hover:text-gray-100"
												>
													{truncateText(campaign.name, 24)}
												</div>
											</div>
										</a>
									{/each}
								</div>
							</div>
						{/each}
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.calendar-container {
		--cell-height: 80px;
	}

	.calendar-day {
		height: var(--cell-height);
		display: flex;
		flex-direction: column;
	}

	.date-header {
		flex-shrink: 0;
	}

	.campaign-container {
		flex: 1;
		overflow-y: auto;
		overflow-x: hidden;
		padding: 1px 2px 2px 2px;
		min-height: 0; /* important for flex child overflow */
		scrollbar-width: thin;
		scrollbar-color: var(--color-scrollbar-thumb) var(--color-scrollbar-track);
	}

	.campaign-item {
		min-height: fit-content;
	}

	.status-indicator {
		min-height: 8px;
	}

	/* mobile-first: small screens */
	@media (max-width: 639px) {
		.calendar-day :global(.campaign-item) {
			padding: 0.25rem 0.375rem;
			gap: 0.25rem;
			margin-bottom: 0.25rem;
		}

		.calendar-day :global(.campaign-name) {
			font-size: 0.625rem;
			line-height: 1.1;
		}

		.calendar-day :global(.company-name) {
			font-size: 0.5rem;
		}

		.calendar-day :global(.status-indicator) {
			width: 2px;
			min-height: 6px;
		}

		.date-header {
			font-size: 0.625rem !important;
			padding: 0.125rem 0.25rem !important;
		}
	}

	@media (min-width: 640px) {
		.calendar-container {
			--cell-height: 130px;
		}

		.campaign-container {
			padding: 2px 4px 4px 4px;
		}

		.status-indicator {
			min-height: 12px;
		}
	}

	@media (min-width: 768px) {
		.calendar-container {
			--cell-height: 150px;
		}
	}

	@media (min-width: 1024px) {
		.calendar-container {
			--cell-height: 170px;
		}
	}

	@media (min-width: 1280px) {
		.calendar-container {
			--cell-height: 190px;
		}
	}

	@media (min-width: 1536px) {
		.calendar-container {
			--cell-height: 200px;
		}
	}
</style>
