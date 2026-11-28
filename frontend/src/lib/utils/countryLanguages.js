// The language most people of a country read, for countries where that is
// clear. Used only to suggest a language for a Country value in Language by,
// the operator still sees and can change every row. Countries with more than
// one main language (Switzerland, Belgium, Canada, India and others) are left
// out. Each country is listed by its English name, its local name and its
// ISO 3166-1 alpha 2 code. Every language is in the fixed list of languages.js.
import { normalizeLanguageKey } from './languages.js';

const COUNTRIES = [
	{ code: 'bg', names: ['Bulgaria', 'България', 'BG'] },
	{ code: 'hr', names: ['Croatia', 'Hrvatska', 'HR'] },
	{ code: 'cs', names: ['Czechia', 'Czech Republic', 'Česko', 'Česká republika', 'CZ'] },
	{ code: 'da', names: ['Denmark', 'Danmark', 'DK'] },
	{ code: 'nl', names: ['Netherlands', 'The Netherlands', 'Nederland', 'Holland', 'NL'] },
	{
		code: 'en',
		names: [
			'United Kingdom',
			'UK',
			'Great Britain',
			'England',
			'GB',
			'United States',
			'United States of America',
			'USA',
			'US',
			'Australia',
			'AU',
			'New Zealand',
			'NZ',
			'Ireland',
			'IE'
		]
	},
	{ code: 'et', names: ['Estonia', 'Eesti', 'EE'] },
	{ code: 'fi', names: ['Finland', 'Suomi', 'FI'] },
	{ code: 'fr', names: ['France', 'FR'] },
	{
		code: 'de',
		names: ['Germany', 'Deutschland', 'DE', 'Austria', 'Österreich', 'AT', 'Liechtenstein', 'LI']
	},
	{ code: 'el', names: ['Greece', 'Ελλάδα', 'Hellas', 'GR'] },
	{ code: 'hu', names: ['Hungary', 'Magyarország', 'HU'] },
	{ code: 'it', names: ['Italy', 'Italia', 'IT', 'San Marino', 'SM'] },
	{ code: 'lv', names: ['Latvia', 'Latvija', 'LV'] },
	{ code: 'lt', names: ['Lithuania', 'Lietuva', 'LT'] },
	{ code: 'pl', names: ['Poland', 'Polska', 'PL'] },
	{ code: 'pt', names: ['Portugal', 'PT', 'Brazil', 'Brasil', 'BR'] },
	{ code: 'ro', names: ['Romania', 'România', 'RO', 'Moldova', 'MD'] },
	{ code: 'sk', names: ['Slovakia', 'Slovensko', 'SK'] },
	{ code: 'sl', names: ['Slovenia', 'Slovenija', 'SI'] },
	{
		code: 'es',
		names: [
			'Spain',
			'España',
			'ES',
			'Mexico',
			'México',
			'MX',
			'Argentina',
			'AR',
			'Colombia',
			'CO',
			'Chile',
			'CL',
			'Peru',
			'Perú',
			'PE'
		]
	},
	{ code: 'sv', names: ['Sweden', 'Sverige', 'SE'] },
	{ code: 'no', names: ['Norway', 'Norge', 'Noreg', 'NO'] },
	{ code: 'is', names: ['Iceland', 'Ísland', 'IS'] },
	{ code: 'sr', names: ['Serbia', 'Srbija', 'Србија', 'RS'] },
	{ code: 'mk', names: ['North Macedonia', 'Macedonia', 'Северна Македонија', 'MK'] },
	{ code: 'sq', names: ['Albania', 'Shqipëria', 'AL'] },
	{ code: 'uk', names: ['Ukraine', 'Україна', 'UA'] },
	{ code: 'ru', names: ['Russia', 'Россия', 'RU'] },
	{ code: 'tr', names: ['Turkey', 'Türkiye', 'TR'] },
	{ code: 'ar', names: ['Saudi Arabia', 'السعودية', 'SA', 'Egypt', 'مصر', 'EG'] },
	{ code: 'he', names: ['Israel', 'ישראל', 'IL'] },
	{ code: 'zh', names: ['China', '中国', 'CN', 'Taiwan', '台灣', '台湾', 'TW'] },
	{ code: 'ja', names: ['Japan', '日本', 'JP'] },
	{ code: 'ko', names: ['South Korea', 'Korea', '대한민국', '한국', 'KR'] },
	{ code: 'th', names: ['Thailand', 'ประเทศไทย', 'TH'] },
	{ code: 'vi', names: ['Vietnam', 'Viet Nam', 'Việt Nam', 'VN'] },
	{ code: 'id', names: ['Indonesia', 'ID'] },
	{ code: 'ms', names: ['Malaysia', 'MY'] }
];

const byKey = new Map(
	COUNTRIES.flatMap(({ code, names }) => names.map((name) => [normalizeLanguageKey(name), code]))
);

/**
 * The language suggested for a Country value, '' when the country has no
 * clear main language or is not known.
 * @param {string} value a raw or normalized country value
 * @returns {string}
 */
export const suggestedCountryLanguage = (value) => byKey.get(normalizeLanguageKey(value)) ?? '';
