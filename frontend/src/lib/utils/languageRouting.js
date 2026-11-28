// Language by checks shared by the campaign and program wizards. A template
// info is { name, language, languages } from a template list row.
import { LANGUAGES, languageName } from './languages.js';

/**
 * A map keyed by recipient values. It has no prototype, so a value such as
 * "constructor" or "__proto__" is a key like any other.
 * @param {Object} [from] the entries to copy
 * @returns {Object}
 */
/**
 * The Language by field that gives each recipient their own Language field.
 * It takes no routes: a recipient gets their language when every template has
 * it, else the default language.
 */
export const LANGUAGE_FIELD = 'language';

export const valueMap = (from) => Object.assign(Object.create(null), from ?? {});

/**
 * The entry of a map keyed by recipient values, only when the map holds it
 * itself.
 * @param {Object} map
 * @param {string} key
 * @returns {*}
 */
export const valueOf = (map, key) =>
	map && Object.prototype.hasOwnProperty.call(map, key) ? map[key] : undefined;

/**
 * Whether a template has language rows. A template with a code and no rows
 * is a single language template: the code stays stored, as sent recipients
 * may carry it, but it is not shown and the template can not use Language by.
 * @param {Object} info
 * @returns {boolean}
 */
export const hasLanguageRows = (info) =>
	Array.isArray(info?.languages) && info.languages.length > 0;

/**
 * The codes a template can send with Language by: its main language and
 * every usable language row. Empty for a single language template.
 * @param {Object} info
 * @returns {string[]}
 */
export const sendableCodes = (info) =>
	info?.language && hasLanguageRows(info)
		? [info.language, ...info.languages.filter((l) => l.isUsable).map((l) => l.language)]
		: [];

/**
 * The codes every template can send, in the order of the fixed list.
 * @param {Object[]} infos
 * @returns {string[]}
 */
export const commonLanguages = (infos) =>
	infos.length
		? LANGUAGES.map((l) => l.code).filter((code) =>
				infos.every((info) => sendableCodes(info).includes(code))
			)
		: [];

/**
 * The main language all templates share, '' when they differ or one of them
 * is a single language template.
 * @param {Object[]} infos
 * @returns {string}
 */
export const sharedMainLanguage = (infos) => {
	const codes = new Set(infos.map((info) => (hasLanguageRows(info) ? info.language || '' : '')));
	return codes.size === 1 ? [...codes][0] : '';
};

/**
 * The codes Language by picks: the default and every mapped value.
 * @param {string} defaultLanguage
 * @param {Object} routes
 * @returns {string[]}
 */
export const routedCodes = (defaultLanguage, routes) => [
	...new Set([defaultLanguage, ...Object.values(routes ?? {})].filter(Boolean))
];

/**
 * The text for a template that has one language.
 * @param {string} name
 * @returns {string}
 */
export const oneLanguageText = (name) =>
	`Template '${name}' has one language. Language by needs templates with Multiple languages, or set Language by to None.`;

/**
 * The text for an anonymous self managed campaign whose Language by picks more
 * than one language.
 */
export const oneLanguageOnlyText =
	'An anonymous self managed campaign can use only one language, as each email is delivered by hand. Set Language by to None or give every value the default language.';

/**
 * Why a template can not send a language: it has no such language, or the
 * language is there but incomplete.
 * @param {Object} info
 * @param {string} code
 * @returns {string}
 */
export const missingLanguageText = (info, code) =>
	(info?.languages ?? []).some((l) => l.language === code)
		? `The ${languageName(code)} of template '${info.name}' is incomplete.`
		: `Template '${info.name}' has no ${languageName(code)}.`;

/**
 * The Language by errors by field (routeBy, default, routes), the same checks
 * the server runs on save. Empty when it is fine.
 * @param {Object} v
 * @param {string} v.routeBy
 * @param {string} v.defaultLanguage
 * @param {Object} v.routes
 * @param {Object[]} v.templates
 * @param {boolean} [v.requireDefault] false leaves out the missing default
 * @param {boolean} [v.oneLanguageOnly] true refuses routing to more than one
 * language, for an anonymous self managed campaign
 * @returns {{routeBy?: string, default?: string, routes?: string}}
 */
export const languageRoutingErrors = ({
	routeBy,
	defaultLanguage,
	routes,
	templates,
	requireDefault = true,
	oneLanguageOnly = false
}) => {
	if (!routeBy) return {};
	const single = templates.find((info) => !sendableCodes(info).length);
	if (single) {
		return { routeBy: oneLanguageText(single.name) };
	}
	if (
		oneLanguageOnly &&
		(routeBy === LANGUAGE_FIELD || routedCodes(defaultLanguage, routes).length > 1)
	) {
		return { routeBy: oneLanguageOnlyText };
	}
	const errors = {};
	if (!defaultLanguage && requireDefault) {
		errors.default = 'Choose the default language.';
	}
	const mapped = routedCodes('', routes);
	for (const info of templates) {
		const codes = sendableCodes(info);
		if (defaultLanguage && !errors.default && !codes.includes(defaultLanguage)) {
			errors.default = missingLanguageText(info, defaultLanguage);
		}
		const missing = mapped.find((code) => !codes.includes(code));
		if (missing && !errors.routes) {
			errors.routes = missingLanguageText(info, missing);
		}
	}
	return errors;
};

/**
 * How many recipients of the picked groups get each language, null when it
 * is not known (values not loaded, or cut and the rest can not be told
 * apart).
 * @param {Object|null} fieldValues the field values response
 * @param {string} defaultLanguage
 * @param {Object} routes
 * @param {string[]|null} [direct] with Language by Language, the codes every
 * template can send: a value is its own code when it is one of them, else
 * the default
 * @returns {Object|null} code to count
 */
export const languageCountsOf = (fieldValues, defaultLanguage, routes, direct = null) => {
	if (!fieldValues || !defaultLanguage) return null;
	const values = fieldValues.values ?? [];
	const listed = new Set(values.map((v) => v.key));
	// the recipients of values not listed get the default only when no route
	// names one of those values
	const known =
		typeof fieldValues.unlistedCount === 'number' &&
		(fieldValues.unlistedCount === 0 || Object.keys(routes ?? {}).every((k) => listed.has(k)));
	if (fieldValues.truncated && !known) return null;
	const unlisted = known ? fieldValues.unlistedCount : 0;
	const counts = Object.create(null);
	const add = (code, n) => {
		if (n > 0) counts[code] = (counts[code] ?? 0) + n;
	};
	for (const v of values) {
		const code = direct
			? direct.includes(v.key)
				? v.key
				: defaultLanguage
			: valueOf(routes, v.key) || defaultLanguage;
		add(code, v.count ?? 0);
	}
	add(defaultLanguage, (fieldValues.emptyCount ?? 0) + unlisted);
	return counts;
};

/**
 * The languages Language by sends, the default first and the others by name,
 * each with how many recipients get it when counts are known. Languages no
 * recipient gets are left out, unless none would be left.
 * @param {string} defaultLanguage
 * @param {Object} routes
 * @param {Object|null} counts code to count, see languageCountsOf
 * @param {string[]|null} [direct] with Language by Language, the codes every
 * template can send, which recipients get by their own Language field
 * @returns {{code: string, count: number|null}[]}
 */
export const sentLanguages = (defaultLanguage, routes, counts, direct = null) => {
	const codes = direct
		? [...new Set([defaultLanguage, ...direct].filter(Boolean))]
		: routedCodes(defaultLanguage, routes);
	const ordered = [
		...codes.filter((code) => code === defaultLanguage),
		...codes
			.filter((code) => code !== defaultLanguage)
			.sort((a, b) => languageName(a).localeCompare(languageName(b)))
	];
	const sent = counts ? ordered.filter((code) => counts[code] > 0) : ordered;
	return (sent.length ? sent : ordered).map((code) => ({
		code,
		count: counts ? (counts[code] ?? 0) : null
	}));
};

/**
 * The text of a sentLanguages list, such as "Danish (4), German (6)".
 * @param {{code: string, count: number|null}[]} list
 * @returns {string}
 */
export const sentLanguagesText = (list) =>
	list
		.map((l) => (l.count === null ? languageName(l.code) : `${languageName(l.code)} (${l.count})`))
		.join(', ');
