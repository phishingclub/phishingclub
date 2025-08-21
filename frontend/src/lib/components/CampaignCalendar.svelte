<script>
	import { onMount } from 'svelte';
	import {
		format,
		addMonths,
		subMonths,
		startOfMonth,
		endOfMonth,
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

	/** @type * */
	export let campaigns = [];
	/** @type {Date} */
	export let start = startOfMonth(new Date());
	/** @type {Date} */
	export let end = endOfMonth(new Date());

	export let onChangeDate;

	let container;
	let currentMonth = startOfMonth(new Date());
	let isLoadingNewMonth = false;
	let weeks = [];
	let isInitialized = false;

	let activeFilters = {
		SCHEDULED: true,
		ACTIVE: true,
		COMPLETED: true,
		SELF_MANAGED: true
	};

	let isGeneratingCalendar = false;

	const COLORS = {
		SCHEDULED: '#62aded',
		ACTIVE: '#5557f6',
		COMPLETED: '#69e1ab',
		SELF_MANAGED: '#9F7AEA'
	};

	function sortCampaignsByPriority(campaigns, day) {
		const dayTime = startOfDay(day).getTime();

		// Helper function to get campaign type priority
		function getTypePriority(campaign) {
			if (campaign.sendStartAt && campaign.sendStartAt > new Date().toISOString()) {
				return 0; // Scheduled gets highest priority
			}
			if (campaign.sendStartAt && !campaign.anonymizedAt && !campaign.closedAt) {
				return 1; // Active gets second priority
			}
			if (!campaign.sendStartAt || !campaign.sendEndAt) {
				return 2; // Self-managed gets third priority
			}
			return 3; // Completed gets lowest priority
		}

		return campaigns.sort((a, b) => {
			// 1. Absolute highest priority: campaign's sendStartAt is on this day
			const aSendStartTime = a.sendStartAt ? startOfDay(new Date(a.sendStartAt)).getTime() : null;
			const bSendStartTime = b.sendStartAt ? startOfDay(new Date(b.sendStartAt)).getTime() : null;

			if (aSendStartTime === dayTime && bSendStartTime !== dayTime) return -1;
			if (bSendStartTime === dayTime && aSendStartTime !== dayTime) return 1;

			// 2. Second priority: type of campaign (scheduled > active > self-managed > completed)
			const aTypePriority = getTypePriority(a);
			const bTypePriority = getTypePriority(b);
			if (aTypePriority !== bTypePriority) {
				return aTypePriority - bTypePriority;
			}

			// 3. If same type, sort by sendStartAt date (if exists) or start date
			const aTime = aSendStartTime || startOfDay(a.start).getTime();
			const bTime = bSendStartTime || startOfDay(b.start).getTime();
			return aTime - bTime;
		});
	}

	function sortByName(campaigns) {
		return [...campaigns].sort((a, b) => a.name.localeCompare(b.name));
	}

	function updateDateRange() {
		const monthStart = startOfMonth(currentMonth);
		const monthEnd = endOfMonth(currentMonth);

		// Calculate first day of calendar (might be in previous month)
		let calendarStart = monthStart;
		const firstDayOfWeek = getDay(monthStart);
		if (firstDayOfWeek > 0) {
			calendarStart = subDays(monthStart, firstDayOfWeek);
		}

		// Calculate last day of calendar (might be in next month)
		const lastDayOfMonth = endOfMonth(monthStart);
		const lastDayOfWeek = getDay(lastDayOfMonth);
		const calendarEnd = addDays(lastDayOfMonth, 6 - lastDayOfWeek);

		start = calendarStart;
		end = calendarEnd;
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

		// Generate days
		let currentDay = new Date(calendarStart);
		while (currentDay < calendarEnd) {
			dayMap.set(currentDay.getTime(), []);
			currentDay = addDays(currentDay, 1);
		}

		// Filter campaigns based on active filters
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

		// Map filtered campaigns to days
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

		// Sort campaigns for each day
		const visibleDayMap = new Map();

		dayMap.forEach((dayCampaigns, dayTime) => {
			const sortedCampaigns = sortCampaignsByPriority(dayCampaigns, new Date(Number(dayTime)));

			visibleDayMap.set(dayTime, {
				campaigns: sortedCampaigns, // All campaigns are now visible
				total: sortedCampaigns.length
			});
		});

		return visibleDayMap;
	}

	async function generateCalendarData() {
		isGeneratingCalendar = true;

		updateDateRange();

		const dayMap = calculateCampaignLayers(calendarCampaigns, start, end);

		// Create weeks array
		weeks = [];
		let week = [];
		let currentDate = new Date(start);

		while (currentDate <= end) {
			// Get campaign data for this day
			const dayTime = currentDate.getTime();
			const dayData = dayMap.get(dayTime) || { campaigns: [], total: 0 };

			// Sort campaigns
			const sortedCampaigns = sortByName(dayData.campaigns);

			// Create day object
			const day = {
				date: new Date(currentDate),
				isToday: isSameDay(currentDate, new Date()),
				isCurrentMonth: isSameMonth(currentDate, currentMonth),
				campaigns: sortedCampaigns, // All campaigns are visible now
				totalCampaigns: dayData.total
			};

			week.push(day);

			// Start a new week if we've filled one
			if (week.length === 7) {
				weeks.push(week);
				week = [];
			}

			// Move to next day
			currentDate = addDays(currentDate, 1);
		}

		isGeneratingCalendar = false;
	}

	async function previousMonth() {
		isLoadingNewMonth = true;
		currentMonth = subMonths(currentMonth, 1);
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function nextMonth() {
		isLoadingNewMonth = true;
		currentMonth = addMonths(currentMonth, 1);
		updateDateRange();
		await onChangeDate();
		isLoadingNewMonth = false;
		await generateCalendarData();
	}

	async function toggleFilter(key) {
		activeFilters[key] = !activeFilters[key];
		await generateCalendarData();
	}

	onMount(async () => {
		await generateCalendarData();
		isInitialized = true;
	});

	$: if (calendarCampaigns) {
		generateCalendarData();
	}
</script>

<div class="w-full bg-white rounded-lg shadow-sm p-4 border border-gray-200">
	<div class="space-y-4 max-w-5xl mx-auto min-h-[600px]">
		<!-- Navigation Controls -->
		<div class="flex justify-center items-center">
			<button
				class="p-2 rounded hover:bg-gray-100 mx-4"
				on:click={previousMonth}
				disabled={isLoadingNewMonth}
			>
				<svg
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 text-gray-600"
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

			<h2 class="text-lg font-semibold">
				{format(currentMonth, 'MMMM yyyy')}
			</h2>

			<button
				class="p-2 rounded hover:bg-gray-100 mx-4"
				on:click={nextMonth}
				disabled={isLoadingNewMonth}
			>
				<svg
					xmlns="http://www.w3.org/2000/svg"
					class="h-5 w-5 text-gray-600"
					fill="none"
					viewBox="0 0 24 24"
					stroke="currentColor"
				>
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
				</svg>
			</button>
		</div>

		<!-- Legend -->
		<div class="flex justify-center flex-wrap gap-4 text-sm">
			{#each [{ key: 'SCHEDULED', color: COLORS.SCHEDULED, label: 'Scheduled' }, { key: 'ACTIVE', color: COLORS.ACTIVE, label: 'Active' }, { key: 'COMPLETED', color: COLORS.COMPLETED, label: 'Completed' }, { key: 'SELF_MANAGED', color: COLORS.SELF_MANAGED, label: 'Self-managed' }] as item}
				<button
					class="flex items-center cursor-pointer select-none"
					on:click={() => toggleFilter(item.key)}
				>
					<div
						class="w-3 h-3 rounded mr-2 transition-opacity duration-200"
						style="background-color: {item.color}; opacity: {activeFilters[item.key] ? '1' : '0.3'}"
					></div>
					<span
						class="transition-opacity duration-200"
						style="opacity: {activeFilters[item.key] ? '1' : '0.5'}">{item.label}</span
					>
				</button>
			{/each}
		</div>

		<!-- Calendar Grid -->
		<div class="calendar-container min-h-[480px]">
			<!-- Day headers -->
			<div class="grid grid-cols-7 text-center mb-1">
				{#each ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as day}
					<div class="text-sm font-medium text-gray-600">{day}</div>
				{/each}
			</div>

			<!-- Calendar grid -->
			{#if !isInitialized || isGeneratingCalendar || isLoadingNewMonth}
				<div class="min-h-[450px] flex items-center justify-center">
					<div class="flex items-center space-x-2">
						<div class="animate-spin rounded-full h-6 w-6 border-b-2 border-blue-600"></div>
						<span class="text-gray-600">Loading calendar...</span>
					</div>
				</div>
			{:else}
				<div class="grid grid-rows-{weeks.length} gap-1 min-h-[450px]">
					{#each weeks as week, weekIndex}
						<div class="grid grid-cols-7 gap-1">
							{#each week as day}
								<div
									class="calendar-day relative border rounded-md overflow-hidden {scrollBarClassesHorizontal}  {day.isToday
										? 'border-gray-700 bg-gray-50'
										: 'border-gray-200'}
	                                {day.isCurrentMonth ? 'bg-white' : 'bg-gray-50'}"
								>
									<!-- Date number -->
									<div
										class="text-sm p-1 {day.isToday
											? 'font-bold text-gray-800'
											: day.isCurrentMonth
												? 'text-gray-600'
												: 'text-gray-400'}"
									>
										{day.date.getDate()}
									</div>
									<!-- Campaigns list -->
									<div
										class="campaign-container {scrollBarClassesVertical} {scrollBarClassesHorizontal}"
									>
										{#each day.campaigns as campaign}
											<a
												href={`/campaign/${campaign.id}`}
												class="block mb-1 rounded-sm text-white text-xs p-1 overflow-hidden hover:opacity-90 transition-opacity"
												style="background-color: {campaign.color};"
												title="{campaign.name} - {campaign.isSelfManaged
													? 'Self-managed'
													: campaign.end < new Date()
														? 'Completed'
														: campaign.start <= new Date()
															? 'Active'
															: 'Scheduled'}, {campaign.isSelfManaged
													? `${formatDateString(new Date(campaign.createdAt))} - ?`
													: `${formatDateString(campaign.start)} - ${formatDateString(campaign.end)}`}"
											>
												<div class="campaign-name truncate text-white">
													{truncateText(campaign.name, 18)}
												</div>
											</a>
										{/each}
									</div>
								</div>
							{/each}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	.calendar-container {
		--cell-aspect-ratio: 0.75; /* Height to width ratio */
	}

	.calendar-day {
		height: 0;
		padding-bottom: calc(100% / var(--cell-aspect-ratio));
		position: relative;
	}

	.campaign-container {
		position: absolute;
		top: 25px; /* Space for the date number */
		bottom: 4px;
		left: 4px;
		right: 4px;
		overflow-y: auto;
		overflow-x: hidden;
	}

	/* Custom scrollbar for campaign container
	.campaign-container::-webkit-scrollbar {
		width: 12px;
	}

	.campaign-container::-webkit-scrollbar-track {
		background: transparent;
	}

	.campaign-container::-webkit-scrollbar-thumb {
		background-color: rgba(0, 255, 255, 0.2);
		border-radius: 24px;
	}
	 */

	/* For Firefox */
	.campaign-container {
		scrollbar-width: thin;
		/*
		scrollbar-color: rgba(0, 255, 255, 0.2) transparent;
		*/
	}

	@media (min-width: 640px) {
		.calendar-container {
			--cell-aspect-ratio: 0.8;
		}
	}

	@media (min-width: 768px) {
		.calendar-container {
			--cell-aspect-ratio: 0.85;
		}
	}

	@media (min-width: 1024px) {
		.calendar-container {
			--cell-aspect-ratio: 0.9;
		}
	}

	@media (min-width: 1280px) {
		.calendar-container {
			--cell-aspect-ratio: 1;
		}
	}
</style>
