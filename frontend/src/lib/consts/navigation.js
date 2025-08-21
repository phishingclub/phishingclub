export const route = {
	profile: {
		label: 'Profile',
		route: '/profile/'
	},
	settings: {
		label: 'Settings',
		route: '/settings/'
	},
	sessions: {
		label: 'Sessions',
		route: '/sessions/'
	},
	logout: {
		label: 'Logout',
		route: '/logout/'
	},
	dashboard: {
		label: 'Dashboard',
		route: '/dashboard/'
	},
	companies: {
		label: 'Companies',
		route: '/company/'
	},
	smtpConfigurations: {
		label: 'SMTP Configurations',
		singleLabel: 'Configurations',
		route: '/smtp-configuration/'
	},
	domain: {
		label: 'Domains',
		route: '/domain/'
	},
	assets: {
		label: 'Assets',
		route: '/asset/'
	},
	attachments: {
		label: 'Attachments',
		route: '/attachment/'
	},
	recipients: {
		label: 'Recipients',
		route: '/recipient/'
	},
	recipientGroups: {
		label: 'Groups',
		route: '/recipient/group/'
	},
	emails: {
		label: 'Emails',
		route: '/email/'
	},
	pages: {
		label: 'Pages',
		route: '/page/'
	},
	campaignTemplates: {
		label: 'Campaign Templates',
		singleLabel: 'Templates',
		route: '/campaign-template/'
	},
	campaigns: {
		label: 'Campaigns',
		route: '/campaign/'
	},
	users: {
		label: 'Users',
		route: '/user/'
	},
	apiSenders: {
		label: 'API Senders',
		route: '/api-sender/'
	},
	allowDeny: {
		label: 'IP filters',
		route: '/ip-filter/'
	},
	webhook: {
		label: 'Webhooks',
		route: '/webhook/'
	},
	userGuide: {
		label: 'User Guide',
		route: 'https://phishing.club/guide/introduction/',
		external: true
	}
};

export const menu = [
	{
		label: 'Dashboard',
		type: 'submenu',
		items: [route.dashboard]
	},

	{
		label: 'Campaigns',
		type: 'submenu',
		items: [route.campaigns, route.campaignTemplates, route.allowDeny, route.webhook]
	},

	{
		label: 'Recipients',
		type: 'submenu',
		items: [route.recipients, route.recipientGroups]
	},
	{
		label: 'Domains',
		type: 'submenu',
		items: [route.domain, route.pages, route.assets]
	},
	{
		label: 'Emails',
		type: 'submenu',
		items: [route.emails, route.attachments, route.smtpConfigurations, route.apiSenders]
	}
];

export const topMenu = [
	route.profile,
	route.sessions,
	route.users,
	route.companies,
	route.settings,
	route.userGuide
];

export const mobileTopMenu = [
	route.profile,
	route.sessions,
	route.users,
	route.companies,
	route.settings,
	route.userGuide
];
