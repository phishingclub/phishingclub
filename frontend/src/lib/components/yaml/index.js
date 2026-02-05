// ES module wrapper for js-yaml UMD bundle
import './js-yaml.js';

// the UMD bundle assigns to globalThis.jsyaml
const jsyaml = globalThis.jsyaml;

export const load = jsyaml.load;
export const dump = jsyaml.dump;
export const loadAll = jsyaml.loadAll;
export const Schema = jsyaml.Schema;
export const Type = jsyaml.Type;
export const YAMLException = jsyaml.YAMLException;
export const CORE_SCHEMA = jsyaml.CORE_SCHEMA;
export const DEFAULT_SCHEMA = jsyaml.DEFAULT_SCHEMA;
export const FAILSAFE_SCHEMA = jsyaml.FAILSAFE_SCHEMA;
export const JSON_SCHEMA = jsyaml.JSON_SCHEMA;

// helper to dump yaml with literal block style for multiline strings in specific keys
export function dumpWithLiteralStrings(obj, literalKeys = ['replace', 'body'], options = {}) {
	// store multiline strings and replace with markers
	const multilineStore = new Map();
	let markerIndex = 0;

	function createMarker() {
		return `___LITERAL_${markerIndex++}___`;
	}

	// recursively process object, replacing multiline strings with markers
	function extractMultilineStrings(data, currentKey = null) {
		if (Array.isArray(data)) {
			return data.map((item) => extractMultilineStrings(item, null));
		}
		if (data && typeof data === 'object') {
			const result = {};
			for (const [key, value] of Object.entries(data)) {
				result[key] = extractMultilineStrings(value, key);
			}
			return result;
		}
		// if this is a multiline string in a literal key, replace with marker
		if (typeof data === 'string' && literalKeys.includes(currentKey) && data.includes('\n')) {
			const marker = createMarker();
			// normalize line endings
			const normalized = data.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
			multilineStore.set(marker, normalized);
			return marker;
		}
		return data;
	}

	// process object to extract multiline strings
	const processed = extractMultilineStrings(obj);

	// dump to yaml - markers will be simple strings
	let yaml = jsyaml.dump(processed, {
		indent: 2,
		lineWidth: -1,
		noRefs: true,
		...options
	});

	// replace each marker with literal block using simple string search
	for (const [marker, value] of multilineStore) {
		// find the marker in the yaml (it may be quoted or unquoted)
		const patterns = [`'${marker}'`, `"${marker}"`, marker];

		for (const pattern of patterns) {
			const idx = yaml.indexOf(pattern);
			if (idx === -1) continue;

			// find the start of this line to get indentation
			let lineStart = idx;
			while (lineStart > 0 && yaml[lineStart - 1] !== '\n') {
				lineStart--;
			}

			// extract indentation
			const lineContent = yaml.substring(lineStart, idx);
			const indentMatch = lineContent.match(/^(\s*)/);
			const baseIndent = indentMatch ? indentMatch[1] : '';

			// find end of line
			let lineEnd = idx + pattern.length;
			while (lineEnd < yaml.length && yaml[lineEnd] !== '\n') {
				lineEnd++;
			}

			// get the key from the line (everything before the colon)
			const fullLine = yaml.substring(lineStart, lineEnd);
			const colonIdx = fullLine.indexOf(':');
			const key = fullLine.substring(baseIndent.length, colonIdx);

			// build literal block
			const blockIndent = baseIndent + '  ';
			const literalLines = value.split('\n').map((line) => blockIndent + line);
			const replacement = `${baseIndent}${key}: |\n${literalLines.join('\n')}`;

			// replace the entire line
			yaml = yaml.substring(0, lineStart) + replacement + yaml.substring(lineEnd);
			break;
		}
	}

	return yaml;
}

export default jsyaml;
