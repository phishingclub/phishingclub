// The fixed list of languages a template can use, stored as lowercase ISO 639-1
// codes. The backend keeps the same list in backend/data/language.go, a test
// there checks the two lists match. Names are English on purpose, so they do
// not change with the browser language.
export const LANGUAGES = [
	// official languages of the EU
	{ code: 'bg', name: 'Bulgarian' },
	{ code: 'hr', name: 'Croatian' },
	{ code: 'cs', name: 'Czech' },
	{ code: 'da', name: 'Danish' },
	{ code: 'nl', name: 'Dutch' },
	{ code: 'en', name: 'English' },
	{ code: 'et', name: 'Estonian' },
	{ code: 'fi', name: 'Finnish' },
	{ code: 'fr', name: 'French' },
	{ code: 'de', name: 'German' },
	{ code: 'el', name: 'Greek' },
	{ code: 'hu', name: 'Hungarian' },
	{ code: 'ga', name: 'Irish' },
	{ code: 'it', name: 'Italian' },
	{ code: 'lv', name: 'Latvian' },
	{ code: 'lt', name: 'Lithuanian' },
	{ code: 'mt', name: 'Maltese' },
	{ code: 'pl', name: 'Polish' },
	{ code: 'pt', name: 'Portuguese' },
	{ code: 'ro', name: 'Romanian' },
	{ code: 'sk', name: 'Slovak' },
	{ code: 'sl', name: 'Slovenian' },
	{ code: 'es', name: 'Spanish' },
	{ code: 'sv', name: 'Swedish' },
	// other languages of Europe
	{ code: 'no', name: 'Norwegian' },
	{ code: 'is', name: 'Icelandic' },
	{ code: 'sr', name: 'Serbian' },
	{ code: 'bs', name: 'Bosnian' },
	{ code: 'mk', name: 'Macedonian' },
	{ code: 'sq', name: 'Albanian' },
	{ code: 'uk', name: 'Ukrainian' },
	{ code: 'ru', name: 'Russian' },
	{ code: 'tr', name: 'Turkish' },
	// other languages common in companies
	{ code: 'ar', name: 'Arabic' },
	{ code: 'he', name: 'Hebrew' },
	{ code: 'hi', name: 'Hindi' },
	{ code: 'zh', name: 'Chinese' },
	{ code: 'ja', name: 'Japanese' },
	{ code: 'ko', name: 'Korean' },
	{ code: 'th', name: 'Thai' },
	{ code: 'vi', name: 'Vietnamese' },
	{ code: 'id', name: 'Indonesian' },
	{ code: 'ms', name: 'Malay' }
];

const names = new Map(LANGUAGES.map((l) => [l.code, l.name]));

/**
 * The English name of a language code, or the code itself when it is not in
 * the list.
 * @param {string} code
 * @returns {string}
 */
export function languageName(code) {
	return names.get(code) ?? code;
}

/**
 * Whether code is in the fixed language list.
 * @param {string} code
 * @returns {boolean}
 */
export function isValidLanguage(code) {
	return names.has(code);
}

/**
 * The key a recipient field value is matched on when it picks a language:
 * trimmed, Unicode NFC and lower case. The server uses the same rule, see
 * NormalizeLanguageKey in backend/model/languageKey.go.
 * @param {string} value
 * @returns {string}
 */
export function normalizeLanguageKey(value) {
	return String(value ?? '')
		.trim()
		.normalize('NFC')
		.toLocaleLowerCase('und');
}
