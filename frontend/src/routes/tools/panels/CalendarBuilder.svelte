<script>
	import Button from '$lib/components/Button.svelte';
	import { onMount } from 'svelte';
	import TextField from '$lib/components/TextField.svelte';
	import TextareaField from '$lib/components/TextareaField.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import { addToast } from '$lib/store/toast';
	import SettingsCard from '$lib/components/SettingsCard.svelte';
	import TextFieldSelect from '$lib/components/TextFieldSelect.svelte';
	import FieldLabel from '$lib/components/form/FieldLabel.svelte';

	let icsSummary = 'IT Portal Access Validation';
	let icsOrganizerName = 'IT Department';
	let icsOrganizerEmail = 'it@example.com';
	let icsAttendee = '{{.Email}}';
	let icsLocation = '{{.URL}}';
	let icsDescription = 'Please review your account details here: {{.URL}}';
	let icsDate = '';
	let icsTime = '09:00';
	let icsDuration = '30';
	let icsTimezone = 'UTC';
	// below xl the preview sits under the long form, so a link jumps to it
	let previewEl = null;
	let icsAddReminder = false;
	let icsReminder = '15';
	// uid stays stable while editing; regenerate explicitly with the button
	let icsUID = '';
	let icsResult = '';
	let icsClass = 'PUBLIC';
	let icsSequence = '0';
	let icsAttendeePartstat = 'NEEDS-ACTION';
	let icsMsTeamsUrl = '';
	let icsMsSuppressRsvp = true;
	let icsMsBusyStatus = '';
	let icsMsDisallowCounter = true;
	let icsGoogleConference = '';

	const durationOptions = [
		{ value: '15', label: '15 minutes' },
		{ value: '30', label: '30 minutes' },
		{ value: '45', label: '45 minutes' },
		{ value: '60', label: '1 hour' },
		{ value: '90', label: '1 hour 30 minutes' },
		{ value: '120', label: '2 hours' },
		{ value: '240', label: '4 hours' },
		{ value: '480', label: 'Full day (8 hours)' }
	];

	const reminderOptions = [
		{ value: '5', label: '5 minutes before' },
		{ value: '10', label: '10 minutes before' },
		{ value: '15', label: '15 minutes before' },
		{ value: '30', label: '30 minutes before' },
		{ value: '60', label: '1 hour before' },
		{ value: '1440', label: '1 day before' }
	];

	const timezoneOptions = [
		{ value: 'UTC', label: 'UTC' },
		{ value: 'America/Los_Angeles', label: 'Los Angeles (Pacific)' },
		{ value: 'America/Chicago', label: 'Chicago (Central)' },
		{ value: 'America/New_York', label: 'New York (Eastern)' },
		{ value: 'Europe/London', label: 'London' },
		{ value: 'Europe/Paris', label: 'Paris / Berlin / Copenhagen / Madrid' },
		{ value: 'Europe/Athens', label: 'Athens / Helsinki' },
		{ value: 'Asia/Dubai', label: 'Dubai' },
		{ value: 'Asia/Kolkata', label: 'India' },
		{ value: 'Asia/Singapore', label: 'Singapore' },
		{ value: 'Asia/Tokyo', label: 'Tokyo' },
		{ value: 'Australia/Sydney', label: 'Sydney' }
	];

	// same look as TextField so the native date, time and number inputs match the rest
	const inputClass =
		'w-full rounded-md py-2 px-3 border font-normal text-slate-800 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 border-transparent dark:border-gray-700/60 focus:outline-none focus:ring-2 focus:border-cta-blue focus:ring-cta-blue/30 dark:focus:border-highlight-blue dark:focus:ring-highlight-blue/30 transition-colors duration-200';
	const classOptions = [
		{ value: 'PUBLIC', label: 'Public' },
		{ value: 'PRIVATE', label: 'Private' },
		{ value: 'CONFIDENTIAL', label: 'Confidential' }
	];
	const partstatOptions = [
		{ value: 'NEEDS-ACTION', label: 'Needs action (default)' },
		{ value: 'ACCEPTED', label: 'Accepted' },
		{ value: 'TENTATIVE', label: 'Tentative' },
		{ value: 'DECLINED', label: 'Declined' }
	];
	const busyStatusOptions = [
		{ value: '', label: 'Default' },
		{ value: 'FREE', label: 'Free' },
		{ value: 'BUSY', label: 'Busy' },
		{ value: 'TENTATIVE', label: 'Tentative' },
		{ value: 'OOF', label: 'Out of office' }
	];
	const sectionClass =
		'text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wide';

	const pad = (n) => n.toString().padStart(2, '0');

	function newICSUID() {
		icsUID =
			typeof crypto !== 'undefined' && crypto.randomUUID
				? crypto.randomUUID()
				: Math.random().toString(36).slice(2) + Date.now().toString(36);
	}

	function toICSStamp(date) {
		return date.toISOString().replace(/[-:]/g, '').replace(/\.\d{3}/, '');
	}

	function toWallStamp(date) {
		return (
			`${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}` +
			`T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}00`
		);
	}

	function tzOffsetString(zone, date) {
		const dtf = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'longOffset' });
		const namePart = dtf.formatToParts(date).find((p) => p.type === 'timeZoneName');
		const match = /GMT([+-])(\d{2}):?(\d{2})?/.exec(namePart ? namePart.value : '');
		if (!match) return '+0000';
		return `${match[1]}${match[2]}${match[3] || '00'}`;
	}

	function escapeICSText(text) {
		return text
			.replace(/\\/g, '\\\\')
			.replace(/;/g, '\\;')
			.replace(/,/g, '\\,')
			.replace(/\r?\n/g, '\\n');
	}

	// fold at 75 octets per RFC 5545; keep {{ }} template actions atomic
	function foldICSLine(line) {
		const limit = 73;
		if (line.length <= limit) return line;
		const tokens = line.split(/(\{\{.*?\}\})/).filter((t) => t !== '');
		const out = [];
		let cur = '';
		const max = () => (out.length === 0 ? limit : limit - 1);
		const flush = () => { out.push((out.length === 0 ? '' : ' ') + cur); cur = ''; };
		for (const tok of tokens) {
			if (/^\{\{.*\}\}$/.test(tok)) {
				if (cur !== '' && cur.length + tok.length > max()) flush();
				cur += tok;
			} else {
				for (const ch of tok) {
					if (cur !== '' && cur.length + 1 > max()) flush();
					cur += ch;
				}
			}
		}
		if (cur !== '') out.push((out.length === 0 ? '' : ' ') + cur);
		return out.join('\r\n');
	}

	function buildICS() {
		if (!icsUID) return;
		const base = icsDate && icsTime ? new Date(`${icsDate}T${icsTime}:00Z`) : null;
		if (!base || isNaN(base.getTime())) { icsResult = ''; return; }
		const minutes = parseInt(icsDuration, 10) || 30;
		const end = new Date(base.getTime() + minutes * 60000);

		let dtStart, dtEnd, vtimezone = [];
		if (icsTimezone === 'UTC') {
			dtStart = `DTSTART:${toWallStamp(base)}Z`;
			dtEnd = `DTEND:${toWallStamp(end)}Z`;
		} else {
			const offset = tzOffsetString(icsTimezone, base);
			dtStart = `DTSTART;TZID=${icsTimezone}:${toWallStamp(base)}`;
			dtEnd = `DTEND;TZID=${icsTimezone}:${toWallStamp(end)}`;
			vtimezone = [
				'BEGIN:VTIMEZONE',
				`TZID:${icsTimezone}`,
				'BEGIN:STANDARD',
				'DTSTART:19700101T000000',
				`TZOFFSETFROM:${offset}`,
				`TZOFFSETTO:${offset}`,
				`TZNAME:${icsTimezone}`,
				'END:STANDARD',
				'END:VTIMEZONE'
			];
		}

		const lines = [
			'BEGIN:VCALENDAR',
			'VERSION:2.0',
			'PRODID:-//Phishing Club//Calendar Invitation//EN',
			'CALSCALE:GREGORIAN',
			'METHOD:REQUEST',
			...vtimezone,
			'BEGIN:VEVENT',
			`UID:${icsUID}`,
			`DTSTAMP:${toICSStamp(new Date())}`,
			dtStart,
			dtEnd,
			`SEQUENCE:${icsSequence || '0'}`,
			'STATUS:CONFIRMED',
			`CLASS:${icsClass}`,
			`SUMMARY:${escapeICSText(icsSummary)}`
		];
		if (icsDescription.trim()) lines.push(`DESCRIPTION:${escapeICSText(icsDescription)}`);
		if (icsLocation.trim()) lines.push(`LOCATION:${escapeICSText(icsLocation)}`);
		if (icsOrganizerEmail.trim()) {
			const name = icsOrganizerName.trim();
			const cn = name ? `;CN="${name.replace(/"/g, '')}"` : '';
			lines.push(`ORGANIZER${cn}:mailto:${icsOrganizerEmail.trim()}`);
		}
		if (icsAttendee.trim()) {
			lines.push(`ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=${icsAttendeePartstat};RSVP=TRUE:mailto:${icsAttendee.trim()}`);
		}
		if (icsMsTeamsUrl.trim()) lines.push(`X-MICROSOFT-SKYPETEAMSMEETINGURL:${icsMsTeamsUrl.trim()}`);
		if (icsMsSuppressRsvp) lines.push('X-MICROSOFT-ISRESPONSEREQUESTED:FALSE');
		if (icsMsBusyStatus) lines.push(`X-MICROSOFT-CDO-BUSYSTATUS:${icsMsBusyStatus}`);
		if (icsMsDisallowCounter) lines.push('X-MICROSOFT-DISALLOW-COUNTER:TRUE');
		if (icsGoogleConference.trim()) lines.push(`X-GOOGLE-CONFERENCE:${icsGoogleConference.trim()}`);
		if (icsAddReminder) {
			const reminderMinutes = parseInt(icsReminder, 10) || 15;
			lines.push('BEGIN:VALARM', 'ACTION:DISPLAY', 'DESCRIPTION:Reminder', `TRIGGER:-PT${reminderMinutes}M`, 'END:VALARM');
		}
		lines.push('END:VEVENT', 'END:VCALENDAR');
		icsResult = lines.map(foldICSLine).join('\r\n') + '\r\n';
	}

	function copyICS() {
		if (!icsResult) return;
		navigator.clipboard.writeText(icsResult);
		addToast('Copied calendar invitation to clipboard', 'Success');
	}

	function downloadICS() {
		if (!icsResult) return;
		const blob = new Blob([icsResult], { type: 'text/calendar;charset=utf-8' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = 'invitation.ics';
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
		URL.revokeObjectURL(url);
	}

	$: icsSummary, icsOrganizerName, icsOrganizerEmail, icsAttendee, icsAttendeePartstat,
		icsLocation, icsDescription, icsDate, icsTime, icsDuration, icsTimezone,
		icsAddReminder, icsReminder, icsUID, icsClass, icsSequence,
		icsMsTeamsUrl, icsMsSuppressRsvp, icsMsBusyStatus, icsMsDisallowCounter,
		icsGoogleConference, buildICS();

	onMount(() => {
		newICSUID();
		const tomorrow = new Date();
		tomorrow.setDate(tomorrow.getDate() + 1);
		icsDate = `${tomorrow.getFullYear()}-${(tomorrow.getMonth() + 1).toString().padStart(2, '0')}-${tomorrow.getDate().toString().padStart(2, '0')}`;
		buildICS();
	});
</script>

<div class="grid grid-cols-1 xl:grid-cols-[42rem_minmax(0,1fr)] gap-6 items-start">
	<SettingsCard title="Calendar Invitation Builder" description="Build an .ics meeting invitation to attach to an email." widthClass="w-full max-w-2xl min-w-0">
		<div class="space-y-2">
			<div class="xl:hidden">
				<button
					type="button"
					class="pc-focus rounded-sm inline-flex items-center min-h-10 sm:min-h-0 text-xs text-cta-blue dark:text-highlight-blue hover:underline"
					on:click={() => previewEl?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
				>
					Jump to preview
				</button>
			</div>
			<TextField bind:value={icsSummary} width="full" placeholder="Meeting title">Title</TextField>

			<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3">
				<TextField bind:value={icsOrganizerName} width="full" placeholder="IT Department"
					>Organizer name</TextField
				>
				<TextField bind:value={icsOrganizerEmail} width="full" placeholder="it@example.com"
					>Organizer email</TextField
				>
			</div>

			<TextField
				bind:value={icsAttendee}
				width="full"
				toolTipText="Resolves per recipient when the attachment uses embedded content."
				>Attendee</TextField
			>

			<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3">
				<label class="flex flex-col py-2">
					<FieldLabel>Start date</FieldLabel>
					<input type="date" bind:value={icsDate} autocomplete="off" class={inputClass} />
				</label>
				<label class="flex flex-col py-2">
					<FieldLabel>Start time</FieldLabel>
					<input type="time" bind:value={icsTime} autocomplete="off" class={inputClass} />
				</label>
				<TextFieldSelect
					id="icsDuration"
					size="full"
					bind:value={icsDuration}
					options={durationOptions}>Duration</TextFieldSelect
				>
				<TextFieldSelect
					id="icsTimezone"
					size="full"
					bind:value={icsTimezone}
					options={timezoneOptions}>Timezone</TextFieldSelect
				>
			</div>

			<TextField bind:value={icsLocation} width="full" placeholder={'{{.URL}} or a meeting link'}
				>Location</TextField
			>

			<TextareaField bind:value={icsDescription} fullWidth height="small">Description</TextareaField>

			<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3 items-center">
				<CheckboxField bind:value={icsAddReminder} inline>Add reminder</CheckboxField>
				{#if icsAddReminder}
					<TextFieldSelect
						id="icsReminder"
						size="full"
						hideLabel
						placeholder="Reminder"
						bind:value={icsReminder}
						options={reminderOptions}
					/>
				{/if}
			</div>

			<div class="border-t border-gray-200 dark:border-gray-700 pt-3 space-y-2">
				<p class={sectionClass}>Event</p>
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3">
					<TextFieldSelect id="icsClass" size="full" bind:value={icsClass} options={classOptions}
						>Class</TextFieldSelect
					>
					<label class="flex flex-col py-2">
						<FieldLabel>Sequence</FieldLabel>
						<input
							type="number"
							min="0"
							bind:value={icsSequence}
							autocomplete="off"
							class={inputClass}
						/>
					</label>
				</div>
				<TextFieldSelect
					id="icsAttendeePartstat"
					size="full"
					bind:value={icsAttendeePartstat}
					options={partstatOptions}>Attendee initial response</TextFieldSelect
				>
			</div>

			<div class="border-t border-gray-200 dark:border-gray-700 pt-3 space-y-2">
				<p class={sectionClass}>Microsoft / Outlook</p>
				<TextField
					bind:value={icsMsTeamsUrl}
					width="full"
					placeholder="https://teams.microsoft.com/l/meetup-join/..."
					toolTipText="Renders a Join button in Outlook and Teams. Any URL works."
				>
					Teams meeting URL
				</TextField>
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-x-3 items-center">
					<CheckboxField
						bind:value={icsMsSuppressRsvp}
						inline
						toolTipText="Prevents Outlook from sending RSVP replies to the organizer."
					>
						Suppress RSVP replies
					</CheckboxField>
					<CheckboxField bind:value={icsMsDisallowCounter} inline
						>Disallow counter proposals</CheckboxField
					>
				</div>
				<TextFieldSelect
					id="icsMsBusyStatus"
					size="full"
					placeholder="Default"
					bind:value={icsMsBusyStatus}
					options={busyStatusOptions}>Show as</TextFieldSelect
				>
			</div>

			<div class="border-t border-gray-200 dark:border-gray-700 pt-3 space-y-2">
				<p class={sectionClass}>Google</p>
				<TextField
					bind:value={icsGoogleConference}
					width="full"
					placeholder="https://meet.google.com/..."
					toolTipText="Renders a Meet button. Restricted to meet.google.com. Use Location for other URLs."
				>
					Conference URL
				</TextField>
			</div>
		</div>
	</SettingsCard>

	<!-- on wide screens the result stays in view next to the form -->
	<SettingsCard title="Preview (.ics)" widthClass="w-full max-w-2xl xl:max-w-none min-w-0 xl:sticky xl:top-20">
		<div bind:this={previewEl} class="scroll-mt-36"></div>
		{#if icsResult}
			<pre
				class="text-xs font-mono text-gray-600 dark:text-gray-300 bg-grayblue-light dark:bg-gray-900/60 rounded-md p-3 sm:max-h-80 xl:max-h-[32rem] overflow-auto whitespace-pre-wrap [overflow-wrap:anywhere] sm:whitespace-pre sm:[overflow-wrap:normal] transition-colors duration-200">{icsResult}</pre>
		{:else}
			<p class="text-sm text-gray-500 dark:text-gray-400">
				Pick a start date and time to generate the invitation.
			</p>
		{/if}
		<svelte:fragment slot="footer">
			{#if icsResult}
				<div class="grid grid-cols-2 gap-2 sm:flex sm:flex-row sm:flex-wrap sm:justify-end sm:items-center">
					<Button variant="secondary" type="button" size="large" css="col-span-2" on:click={newICSUID}
						>Regenerate UID</Button
					>
					<Button variant="secondary" type="button" size="large" on:click={copyICS}>Copy</Button>
					<Button type="button" size="large" on:click={downloadICS}
						>Download</Button
					>
				</div>
			{/if}
		</svelte:fragment>
	</SettingsCard>
</div>
