<script>
	// Shows the values to allow list the mail of one template, of every template
	// in a campaign, or of every template a program uses, in Microsoft Defender
	// Advanced Delivery. Call open(id, name) to load and show it.
	import { api } from '$lib/api/apiProxy.js';
	import Note from '$lib/components/Note.svelte';
	import { AppStateService } from '$lib/service/appState';
	import { addToast } from '$lib/store/toast';
	import Modal from '$lib/components/Modal.svelte';
	import Button from '$lib/components/Button.svelte';
	import CopyCell from '$lib/components/table/CopyCell.svelte';
	import CheckboxField from '$lib/components/CheckboxField.svelte';
	import { languageName } from '$lib/utils/languages.js';

	/** @type {'template'|'campaign'|'program'} */
	export let kind = 'template';
	// when set, a template note calls this with the template id instead of linking
	// to the templates page, for the templates page itself
	/** @type {((id: string) => void)|null} */
	export let onEditTemplate = null;

	const DEFENDER_URL = 'https://security.microsoft.com/advanceddelivery';
	const DOCS_URL =
		'https://learn.microsoft.com/en-us/defender-office-365/advanced-delivery-policy-configure?view=o365-worldwide';
	const STEPS = [
		'Go to Microsoft Defender Advanced Delivery',
		'Select the Phishing simulation tab',
		'Click Add or Edit to configure a third party phishing simulation',
		'Enter the values above in the Domain, Sending IP and Simulation URLs to allow fields'
	];

	const loaders = {
		template: (id, companyID) => api.campaignTemplate.getAllowListing(id, companyID),
		campaign: (id, companyID) => api.campaign.getAllowListing(id, companyID),
		program: (id, companyID) => api.program.getAllowListing(id, companyID)
	};
	const nouns = { template: 'template', campaign: 'campaign', program: 'program' };
	// Microsoft limits on one phishing simulation policy
	const LIMITS = { domains: 20, sendingIPs: 10, simulationURLs: 30, urlLength: 250 };
	const URL_WHY =
		'Listed so Microsoft does not open the links itself, which would show up as clicks.';
	const WHOLE_DOMAIN_RISK =
		'These entries allow links to every address on these domains, so dedicated simulation domains are best.';

	let visible = false;
	let isLoading = false;
	let error = '';
	let name = '';
	let data = null;
	let showBreakdown = false;
	let showExact = false;
	// ignores a slow answer for an earlier open
	let loadToken = 0;

	export const open = async (id, title = '') => {
		const token = ++loadToken;
		name = title;
		data = null;
		error = '';
		showBreakdown = false;
		showExact = false;
		visible = true;
		isLoading = true;
		try {
			const context = AppStateService.instance.getContext();
			const res = await loaders[kind](id, context?.companyID || null);
			if (token !== loadToken) return;
			if (!res.success) {
				error =
					res.statusCode === 404
						? `The ${nouns[kind]} was not found in this view.`
						: res.error || 'Failed to load the allow listing information';
				return;
			}
			data = res.data;
		} catch (e) {
			if (token !== loadToken) return;
			error = 'Failed to load the allow listing information';
			console.error('failed to load allow listing', e);
		} finally {
			if (token === loadToken) isLoading = false;
		}
	};

	const close = () => {
		visible = false;
		loadToken++;
		data = null;
		error = '';
	};

	const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

	// notes name each missing value once, with a link to the setting that fixes it
	const buildNotes = (d) => {
		if (!d) return [];
		const notes = [];
		const seenSenders = new Set();
		if (!d.templates.length && !d.deletedTemplates) {
			notes.push({ text: `This ${nouns[kind]} has no templates.` });
		}
		for (const t of d.templates) {
			const editTemplate = { label: 'Update template', templateID: t.id };
			if (t.missing.includes('domain')) {
				notes.push({ text: `Template ${t.name} has no domain.`, link: editTemplate });
			}
			if (t.missing.includes('email')) {
				notes.push({ text: `Template ${t.name} has no email.`, link: editTemplate });
			}
			if (t.missing.includes('mailFrom')) {
				notes.push({
					text: `The email of template ${t.name} has no envelope sender domain.`,
					link: { label: 'Update email', href: `/email?edit=${t.emailID}` }
				});
			}
			// each other language sends its own email, with its own envelope sender
			for (const l of t.languages ?? []) {
				const lang = languageName(l.language);
				if (l.missing?.includes('email')) {
					notes.push({
						text: `The ${lang} email of template ${t.name} is missing.`,
						link: editTemplate
					});
				} else if (l.missing?.includes('mailFrom')) {
					notes.push({
						text: `The ${lang} email of template ${t.name} has no envelope sender domain.`,
						link: { label: 'Update email', href: `/email?edit=${l.emailID}` }
					});
				}
			}
			if (t.missing.includes('sender')) {
				notes.push({
					text: `Template ${t.name} has no SMTP configuration or API sender.`,
					link: editTemplate
				});
			}
			if (t.senderID && !seenSenders.has(t.senderID)) {
				if (t.missing.includes('sendingIP')) {
					seenSenders.add(t.senderID);
					notes.push({
						text: `SMTP configuration ${t.senderName} has no public sending IP set. Add it under Advanced in the SMTP configuration.`,
						link: {
							label: 'Update SMTP configuration',
							href: `/smtp-configuration?edit=${t.senderID}`
						}
					});
				} else if (t.senderType === 'apiSender') {
					seenSenders.add(t.senderID);
					notes.push({
						text: `API sender ${t.senderName} has no sending IP field. Get the sending IPs from the provider. The provider usually sends with its own bounce domain, so the MAIL FROM domain shown here needs a check with the provider too.`
					});
				}
			}
		}
		if (d.deletedTemplates) {
			notes.push({
				text: `${plural(d.deletedTemplates, 'template')} of this ${nouns[kind]} ${d.deletedTemplates === 1 ? 'was' : 'were'} deleted and ${d.deletedTemplates === 1 ? 'is' : 'are'} not included.`
			});
		}
		return notes;
	};

	const overLimit = (count, limit, what) =>
		`${count} ${what} is more than the ${limit} Microsoft allows in one policy. Split the templates over more policies.`;

	const buildSections = (d, exact) => {
		if (!d) return [];
		const urls = exact ? d.simulationURLs : d.simulationDomainURLs || [];
		const domainNotes = [];
		if (d.overLimit?.mailFromDomains) {
			domainNotes.push(overLimit(d.mailFromDomains.length, LIMITS.domains, 'domains'));
		}
		const ipNotes = [];
		if (d.overLimit?.sendingIPs) {
			ipNotes.push(overLimit(d.sendingIPs.length, LIMITS.sendingIPs, 'sending IPs'));
		}
		if (d.hasIPv6SendingIP) {
			ipNotes.push('IPv6 addresses can be added only with PowerShell, not in the portal.');
		}
		const urlNotes = [];
		const urlOver = exact ? d.overLimit?.simulationURLs : d.overLimit?.simulationDomainURLs;
		if (urlOver) {
			urlNotes.push(
				overLimit(urls.length, LIMITS.simulationURLs, 'entries') +
					(exact ? '' : ' Each domain needs two entries.')
			);
		}
		if (d.tooLongURLs?.length) {
			urlNotes.push(
				`Left out as longer than ${LIMITS.urlLength} characters, the most Microsoft allows: ${d.tooLongURLs.join(', ')}`
			);
		}
		return [
			{
				label: 'Domain (MAIL FROM / 5321.MailFrom)',
				copyLabel: 'Domain (MAIL FROM / 5321.MailFrom)',
				values: d.mailFromDomains,
				empty: 'No envelope sender domain found. See the notes below.',
				notes: domainNotes
			},
			{
				label: 'Sending IP',
				copyLabel: 'Sending IP',
				values: d.sendingIPs,
				empty: 'No sending IP set. See the notes below.',
				notes: ipNotes
			},
			{
				label: 'Simulation URLs to allow',
				copyLabel: `Simulation URLs to allow (${exact ? 'exact entries' : 'whole domain entries'})`,
				values: urls,
				empty: 'No domain found. See the notes below.',
				hints: exact ? [URL_WHY] : [URL_WHY, WHOLE_DOMAIN_RISK],
				notes: urlNotes,
				isURLs: true
			}
		];
	};

	$: notes = buildNotes(data);
	$: sections = buildSections(data, showExact);

	const breakdownRows = (t) => [
		{ label: 'Domain', value: [t.domain, ...t.proxyDomains].filter(Boolean).join(', ') },
		{ label: 'MAIL FROM', value: breakdownMailFrom(t) },
		{ label: 'Sender', value: breakdownSender(t) },
		{ label: 'Sending IP', value: breakdownIPs(t) }
	];
	// an API sender provider usually sets its own bounce domain, so the
	// envelope sender of the email is shown with a check
	const breakdownMailFrom = (t) => {
		const domains = mailFromDomainsOf(t);
		if (domains.length && t.senderType === 'apiSender') {
			return `${domains.join(', ')} (check with the provider)`;
		}
		return domains.join(', ');
	};
	// the envelope sender domain of the template email and of each other
	// language email, each domain once. With languages the domain is followed
	// by the languages that use it
	const mailFromDomainsOf = (t) => {
		const languages = t.languages ?? [];
		if (!languages.length) return t.mailFromDomain ? [t.mailFromDomain] : [];
		const byDomain = new Map();
		const add = (domain, code) => {
			if (!domain) return;
			if (!byDomain.has(domain)) byDomain.set(domain, []);
			if (code) byDomain.get(domain).push(languageName(code));
		};
		add(t.mailFromDomain, t.language);
		for (const l of languages) add(l.mailFromDomain, l.language);
		return [...byDomain].map(([domain, names]) =>
			names.length ? `${domain} (${names.join(', ')})` : domain
		);
	};
	// whether any template sends more than one email
	$: manyEmails =
		kind !== 'template' || !!data?.templates?.some((t) => (t.languages ?? []).length > 0);
	const breakdownSender = (t) => {
		if (t.senderType === 'smtp') return `SMTP: ${t.senderName}`;
		if (t.senderType === 'apiSender') return `API sender: ${t.senderName}`;
		return '';
	};
	const breakdownIPs = (t) => {
		if (t.sendingIPs.length) return t.sendingIPs.join(', ');
		if (t.senderType === 'apiSender') return 'Ask the provider';
		return '';
	};

	const copyAll = async () => {
		if (!data) return;
		const lines = ['Microsoft Defender Advanced Delivery, phishing simulation'];
		if (name) lines.push(name);
		lines.push('');
		for (const s of sections) {
			lines.push(`${s.copyLabel}:`);
			if (s.values.length) {
				lines.push(...s.values);
			} else {
				lines.push(`Note: ${s.empty}`);
			}
			for (const n of [...(s.hints || []), ...s.notes]) lines.push(`Note: ${n}`);
			lines.push('');
		}
		if (notes.length) {
			lines.push('Notes:');
			for (const n of notes) lines.push(`- ${n.text}`);
			lines.push('');
		}
		lines.push('Where to add these:');
		STEPS.forEach((step, i) => {
			lines.push(`${i + 1}. ${step}${i === 0 ? `: ${DEFENDER_URL}` : ''}`);
		});
		lines.push('');
		lines.push(`Microsoft documentation: ${DOCS_URL}`);
		try {
			await navigator.clipboard.writeText(lines.join('\n'));
			addToast(
				`Copied allow listing with ${showExact ? 'exact' : 'whole domain'} simulation URL entries`,
				'Info'
			);
		} catch (e) {
			addToast('Failed to copy', 'Error');
		}
	};

	const linkCls =
		'pc-focus rounded-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline';
	const labelCls = 'text-sm font-semibold text-pc-darkblue dark:text-white';
</script>

<Modal
	headerText="Allow listing"
	description={name}
	{visible}
	onClose={close}
	width="lg"
	docSlug="campaigns"
	docAnchor="allow-listing"
>
	<div class="py-6 space-y-6 text-gray-700 dark:text-gray-300 transition-colors duration-200">
		{#if isLoading}
			<p class="text-sm">Loading allow listing information...</p>
		{:else if error}
			<p class="text-sm text-red-600 dark:text-red-400">{error}</p>
		{:else if data}
			<p class="text-sm">
				Add these values to the <b>Advanced Delivery</b> policy in Microsoft Defender for Office 365
				as a third party phishing simulation, so the
				{manyEmails ? 'emails are' : 'template email is'} delivered.
				{#if kind !== 'template'}
					They cover {plural(data.templates.length, 'template')}.
				{/if}
			</p>

			{#each sections as section}
				<div>
					<p class={labelCls}>{section.label}</p>
					{#if section.isURLs}
						{#each section.hints as hint}
							<p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{hint}</p>
						{/each}
						<CheckboxField inline plain bind:value={showExact}>Show exact entries</CheckboxField>
					{/if}
					{#if section.values.length}
						<ul class="mt-1">
							{#each section.values as value}
								<li><CopyCell text={value} /></li>
							{/each}
						</ul>
					{:else}
						<Note boxed class="mt-1">{section.empty}</Note>
					{/if}
					{#if section.notes.length}
						<ul class="mt-2 space-y-2">
							{#each section.notes as note}
								<li><Note boxed>{note}</Note></li>
							{/each}
						</ul>
					{/if}
				</div>
			{/each}

			{#if notes.length}
				<div>
					<p class={labelCls}>Notes</p>
					<ul class="mt-1 space-y-2">
						{#each notes as note}
							<li>
								<Note boxed>
									{note.text}
									{#if note.link}
										{#if note.link.templateID && onEditTemplate}
											<button
												type="button"
												class="ml-1 {linkCls}"
												on:click={() => {
													const id = note.link.templateID;
													close();
													onEditTemplate(id);
												}}>{note.link.label}</button
											>
										{:else}
											<a
												class="ml-1 {linkCls}"
												href={note.link.href || `/campaign-template?edit=${note.link.templateID}`}
												on:click={close}>{note.link.label}</a
											>
										{/if}
									{/if}
								</Note>
							</li>
						{/each}
					</ul>
				</div>
			{/if}

			{#if data.templates.length}
				<div>
					<button
						type="button"
						class="pc-focus inline-flex items-center gap-1 rounded-sm text-sm font-medium text-cta-blue dark:text-highlight-blue hover:underline"
						aria-expanded={showBreakdown}
						on:click={() => (showBreakdown = !showBreakdown)}
					>
						{showBreakdown ? 'Hide details per template' : 'Show details per template'}
						<svg
							class="w-4 h-4 transition-transform duration-200"
							class:rotate-180={showBreakdown}
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
							aria-hidden="true"
							><path
								stroke-linecap="round"
								stroke-linejoin="round"
								stroke-width="2"
								d="M19 9l-7 7-7-7"
							/></svg
						>
					</button>
					{#if showBreakdown}
						<ul class="mt-3 space-y-3">
							{#each data.templates as t (t.id)}
								<li
									class="rounded-md border border-gray-200 dark:border-gray-700 px-4 py-3 text-sm space-y-1 transition-colors duration-200"
								>
									<p class="font-semibold text-pc-darkblue dark:text-white break-words">{t.name}</p>
									{#each breakdownRows(t) as row}
										<div class="flex justify-between gap-4">
											<span class="shrink-0 text-gray-600 dark:text-gray-400">{row.label}:</span>
											<span
												class="min-w-0 text-right break-words {row.value
													? 'text-pc-darkblue dark:text-white'
													: 'text-gray-400 dark:text-gray-500'}">{row.value || 'Not set'}</span
											>
										</div>
									{/each}
								</li>
							{/each}
						</ul>
					{/if}
				</div>
			{/if}

			<div class="text-sm">
				<p class={labelCls}>Where to add these</p>
				<ol class="mt-1 list-decimal ml-6 space-y-1">
					<li>
						Go to <a href={DEFENDER_URL} target="_blank" rel="noopener noreferrer" class={linkCls}
							>Microsoft Defender Advanced Delivery</a
						>
					</li>
					<li>Select the <b>Phishing simulation</b> tab</li>
					<li>Click <b>Add</b> or <b>Edit</b> to configure a third party phishing simulation</li>
					<li>
						Enter the values above in the <b>Domain</b>, <b>Sending IP</b> and
						<b>Simulation URLs to allow</b> fields
					</li>
				</ol>
				<p class="mt-2">
					See the <a href={DOCS_URL} target="_blank" rel="noopener noreferrer" class={linkCls}
						>Microsoft documentation</a
					> for details.
				</p>
			</div>
		{/if}
	</div>
	<div
		class="sticky bottom-0 -mx-4 sm:-mx-8 px-4 sm:px-8 py-4 border-t-2 border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 flex flex-wrap justify-end gap-2 transition-colors duration-200"
	>
		<Button variant="secondary" size="small" on:click={close}>Close</Button>
		<Button size="medium" disabled={!data} on:click={copyAll}>Copy All</Button>
	</div>
</Modal>
