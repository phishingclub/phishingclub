// Tag chip colors, matching the Bait Box registry palette so a tag shows the same
// color here that the registry operator chose. The key is an allowlisted token the
// registry sends; the class strings are written out in full so Tailwind generates
// them. The token is only ever used as a lookup key, never interpolated into a
// class, so a registry cannot inject arbitrary styling. An empty or unknown key
// falls back to a neutral gray chip.
const TAG_CHIP = {
	slate: 'bg-slate-100 text-slate-700 dark:bg-slate-400/15 dark:text-slate-200',
	red: 'bg-red-100 text-red-700 dark:bg-red-400/15 dark:text-red-200',
	orange: 'bg-orange-100 text-orange-700 dark:bg-orange-400/15 dark:text-orange-200',
	amber: 'bg-amber-100 text-amber-800 dark:bg-amber-400/15 dark:text-amber-200',
	green: 'bg-green-100 text-green-700 dark:bg-green-400/15 dark:text-green-200',
	teal: 'bg-teal-100 text-teal-700 dark:bg-teal-400/15 dark:text-teal-200',
	sky: 'bg-sky-100 text-sky-700 dark:bg-sky-400/15 dark:text-sky-200',
	blue: 'bg-blue-100 text-blue-700 dark:bg-blue-400/15 dark:text-blue-200',
	violet: 'bg-violet-100 text-violet-700 dark:bg-violet-400/15 dark:text-violet-200',
	pink: 'bg-pink-100 text-pink-700 dark:bg-pink-400/15 dark:text-pink-200'
};
const TAG_CHIP_NEUTRAL =
	'bg-grayblue-light text-slate-700 dark:bg-gray-700 dark:text-gray-200';

// the chip classes for a tag color token, or a neutral default for none/unknown.
// uses hasOwnProperty so a token like "constructor" or "toString" does not resolve
// an inherited prototype member and leak junk into the class attribute.
export function tagChipClass(key) {
	return Object.prototype.hasOwnProperty.call(TAG_CHIP, key) ? TAG_CHIP[key] : TAG_CHIP_NEUTRAL;
}
