/**
 * Monaco Editor YAML completion provider for proxy configurations
 */

export class ProxyYamlCompletionProvider {
	constructor(monaco) {
		this.monaco = monaco;
		this.completionProvider = null;
		this.hoverProvider = null;
		this.setupLanguageFeatures();
	}

	setupLanguageFeatures() {
		try {
			if (!this.completionProvider) {
				this.completionProvider = this.monaco.languages.registerCompletionItemProvider('yaml', {
					provideCompletionItems: (model, position) => {
						try {
							return this.provideCompletionItems(model, position);
						} catch (error) {
							console.warn('Completion error:', error);
							return { suggestions: [] };
						}
					}
				});
			}

			if (!this.hoverProvider) {
				this.hoverProvider = this.monaco.languages.registerHoverProvider('yaml', {
					provideHover: (model, position) => {
						try {
							return this.provideHover(model, position);
						} catch (error) {
							console.warn('Hover error:', error);
							return null;
						}
					}
				});
			}
		} catch (error) {
			console.warn('Failed to setup language features:', error);
		}
	}

	dispose() {
		if (this.completionProvider) {
			this.completionProvider.dispose();
			this.completionProvider = null;
		}
		if (this.hoverProvider) {
			this.hoverProvider.dispose();
			this.hoverProvider = null;
		}
	}

	provideCompletionItems(model, position) {
		const word = model.getWordUntilPosition(position);
		const range = {
			startLineNumber: position.lineNumber,
			endLineNumber: position.lineNumber,
			startColumn: word.startColumn,
			endColumn: word.endColumn
		};

		const lineContent = model.getLineContent(position.lineNumber);
		const linePrefix = lineContent.substring(0, position.column - 1);
		const fullContent = model.getValue();
		const linesAbove = model.getLinesContent().slice(0, position.lineNumber - 1);

		return {
			suggestions: this.getSuggestions(linePrefix, range, linesAbove, fullContent)
		};
	}

	getSuggestions(linePrefix, range, linesAbove, fullContent) {
		const suggestions = [];
		const currentIndent = this.getIndent(linePrefix);

		// Handle specific field value completions
		if (linePrefix.match(/mode:\s*$/)) {
			return this.getModeSuggestions(range);
		}
		if (linePrefix.match(/from:\s*$/)) {
			return this.getFromSuggestions(range);
		}
		if (linePrefix.match(/method:\s*$/)) {
			return this.getMethodSuggestions(range);
		}
		if (linePrefix.match(/(with_session|without_session):\s*$/)) {
			return this.getActionSuggestions(range);
		}

		// Handle array items
		if (linePrefix.match(/^\s*-\s*$/)) {
			const context = this.findParentSection(linesAbove, currentIndent);
			if (context === 'paths') {
				return this.getPathPatternSuggestions(range);
			}
			if (context === 'capture') {
				return this.getNewCaptureSuggestions(range);
			}
			if (context === 'rewrite') {
				return this.getNewRewriteSuggestions(range);
			}
		}

		// Handle field completions based on context
		const context = this.findParentSection(linesAbove, currentIndent);

		if (currentIndent === 0) {
			return this.getTopLevelSuggestions(range);
		}

		switch (context) {
			case 'global':
				return this.getGlobalSuggestions(range);
			case 'domain':
				return this.getDomainSuggestions(range);
			case 'access':
				return this.getAccessSuggestions(range);
			case 'on_deny':
				return this.getOnDenySuggestions(range);
			case 'capture':
				return this.getCaptureSuggestions(range);
			case 'rewrite':
				return this.getRewriteSuggestions(range);
			default:
				return [];
		}
	}

	getIndent(line) {
		const match = line.match(/^\s*/);
		return match ? match[0].length : 0;
	}

	findParentSection(linesAbove, currentIndent) {
		// Look backwards to find the parent section
		for (let i = linesAbove.length - 1; i >= 0; i--) {
			const line = linesAbove[i];
			const lineIndent = this.getIndent(line);
			const trimmed = line.trim();

			if (!trimmed || trimmed.startsWith('#')) continue;

			// If we find a line with less indentation that has a colon, it's a parent
			if (lineIndent < currentIndent && trimmed.includes(':')) {
				const key = trimmed.split(':')[0].trim();

				// Top level sections
				if (lineIndent === 0) {
					if (key === 'global') return 'global';
					if (key.match(/^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/)) return 'domain';
				}

				// Nested sections
				if (key === 'access') return 'access';
				if (key === 'capture') return 'capture';
				if (key === 'rewrite') return 'rewrite';
				if (key === 'on_deny') return 'on_deny';
				if (key === 'paths') return 'paths';
			}
		}

		return null;
	}

	getTopLevelSuggestions(range) {
		return [
			{
				label: 'version',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'version: "0.0"',
				documentation: 'Configuration version',
				range
			},
			{
				label: 'proxy',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'proxy: "proxy-name"',
				documentation: 'Optional proxy name',
				range
			},
			{
				label: 'global',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'global:',
				documentation: 'Global rules for all domains',
				range
			}
		];
	}

	getGlobalSuggestions(range) {
		return [
			{
				label: 'access',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'access:',
				documentation: 'Global access control',
				range
			},
			{
				label: 'capture',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'capture:',
				documentation: 'Global capture rules',
				range
			},
			{
				label: 'rewrite',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'rewrite:',
				documentation: 'Global rewrite rules',
				range
			}
		];
	}

	getDomainSuggestions(range) {
		return [
			{
				label: 'to',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'to: "phishing-domain.com"',
				documentation: 'Target phishing domain (required)',
				range
			},
			{
				label: 'access',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'access:',
				documentation: 'Domain access control',
				range
			},
			{
				label: 'capture',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'capture:',
				documentation: 'Domain capture rules',
				range
			},
			{
				label: 'rewrite',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'rewrite:',
				documentation: 'Domain rewrite rules',
				range
			}
		];
	}

	getAccessSuggestions(range) {
		return [
			{
				label: 'mode',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'mode: "allow"',
				documentation: 'Access control mode: allow or deny',
				range
			},
			{
				label: 'paths',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'paths:',
				documentation: 'Array of path patterns',
				range
			},
			{
				label: 'on_deny',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'on_deny:',
				documentation: 'Response when access denied',
				range
			}
		];
	}

	getOnDenySuggestions(range) {
		return [
			{
				label: 'with_session',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'with_session: 403',
				documentation: 'Response for users with sessions',
				range
			},
			{
				label: 'without_session',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'without_session: 404',
				documentation: 'Response for users without sessions',
				range
			}
		];
	}

	getCaptureSuggestions(range) {
		return [
			{
				label: 'name',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'name: "capture_name"',
				documentation: 'Unique capture rule name (required)',
				range
			},
			{
				label: 'method',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'method: "POST"',
				documentation: 'HTTP method to match',
				range
			},
			{
				label: 'path',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'path: "/login"',
				documentation: 'URL path pattern to match (required)',
				range
			},
			{
				label: 'find',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'find: "pattern"',
				documentation: 'Regex pattern to capture',
				range
			},
			{
				label: 'from',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'from: "request_body"',
				documentation: 'Where to search for pattern',
				range
			},
			{
				label: 'required',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'required: true',
				documentation: 'Whether capture is required',
				range
			}
		];
	}

	getRewriteSuggestions(range) {
		return [
			{
				label: 'name',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'name: "rewrite_name"',
				documentation: 'Optional rewrite rule name',
				range
			},
			{
				label: 'find',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'find: "pattern"',
				documentation: 'Regex pattern to find (required)',
				range
			},
			{
				label: 'replace',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'replace: "replacement"',
				documentation: 'Replacement text (required)',
				range
			},
			{
				label: 'from',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'from: "response_body"',
				documentation: 'Where to apply replacement',
				range
			}
		];
	}

	getNewCaptureSuggestions(range) {
		return [
			{
				label: 'capture rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "capture_name"\n  method: "POST"\n  path: "/path"\n  find: "pattern"\n  from: "request_body"',
				documentation: 'New capture rule template',
				range
			}
		];
	}

	getNewRewriteSuggestions(range) {
		return [
			{
				label: 'rewrite rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "rewrite_name"\n  find: "pattern"\n  replace: "replacement"\n  from: "response_body"',
				documentation: 'New rewrite rule template',
				range
			}
		];
	}

	getModeSuggestions(range) {
		return [
			{
				label: '"allow"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"allow"',
				documentation: 'Allowlist mode - only specified paths allowed',
				range
			},
			{
				label: '"deny"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"deny"',
				documentation: 'Denylist mode - specified paths blocked',
				range
			}
		];
	}

	getFromSuggestions(range) {
		return [
			{
				label: '"request_body"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"request_body"',
				documentation: 'Search in request body',
				range
			},
			{
				label: '"request_header"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"request_header"',
				documentation: 'Search in request headers',
				range
			},
			{
				label: '"response_body"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"response_body"',
				documentation: 'Search in response body',
				range
			},
			{
				label: '"response_header"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"response_header"',
				documentation: 'Search in response headers',
				range
			},
			{
				label: '"cookie"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"cookie"',
				documentation: 'Capture cookie data',
				range
			},
			{
				label: '"any"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"any"',
				documentation: 'Search anywhere',
				range
			}
		];
	}

	getMethodSuggestions(range) {
		return [
			{
				label: '"GET"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"GET"',
				documentation: 'HTTP GET method',
				range
			},
			{
				label: '"POST"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"POST"',
				documentation: 'HTTP POST method',
				range
			},
			{
				label: '"PUT"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"PUT"',
				documentation: 'HTTP PUT method',
				range
			},
			{
				label: '"DELETE"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"DELETE"',
				documentation: 'HTTP DELETE method',
				range
			}
		];
	}

	getActionSuggestions(range) {
		return [
			{
				label: '"allow"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"allow"',
				documentation: 'Allow access (override deny)',
				range
			},
			{
				label: '"redirect:https://example.com"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"redirect:https://example.com"',
				documentation: 'Redirect to URL',
				range
			},
			{
				label: '404',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '404',
				documentation: 'Return 404 Not Found',
				range
			},
			{
				label: '403',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '403',
				documentation: 'Return 403 Forbidden',
				range
			},
			{
				label: '503',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '503',
				documentation: 'Return 503 Service Unavailable',
				range
			}
		];
	}

	getPathPatternSuggestions(range) {
		return [
			{
				label: '"^/admin/"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"^/admin/"',
				documentation: 'Admin panel paths',
				range
			},
			{
				label: '"^/login"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"^/login"',
				documentation: 'Login page',
				range
			},
			{
				label: '"^/api/"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"^/api/"',
				documentation: 'API endpoints',
				range
			},
			{
				label: '"^/assets/"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"^/assets/"',
				documentation: 'Static assets',
				range
			},
			{
				label: '"^/\\.git/"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"^/\\.git/"',
				documentation: 'Git repository',
				range
			}
		];
	}

	provideHover(model, position) {
		const word = model.getWordAtPosition(position);
		if (!word) return null;

		const hoverInfo = this.getHoverInfo(word.word);
		if (!hoverInfo) return null;

		return {
			range: new this.monaco.Range(
				position.lineNumber,
				word.startColumn,
				position.lineNumber,
				word.endColumn
			),
			contents: [{ value: `**${word.word}**` }, { value: hoverInfo }]
		};
	}

	getHoverInfo(word) {
		const hoverData = {
			version: 'Configuration version. Currently supports "0.0"',
			global: 'Rules that apply to all domain mappings',
			access: 'Access control configuration - restricts which paths are accessible',
			mode: 'Access control mode: "allow" (allowlist) or "deny" (denylist)',
			paths: 'Array of regex patterns for path matching',
			on_deny: 'Response configuration when access is denied',
			with_session: 'Response for users with active proxy sessions (request with mitm cookie)',
			without_session: 'Response for requests without sessions',
			capture: 'Rules for capturing data from requests/responses',
			name: 'Unique identifier for the rule',
			method: 'HTTP method to match (GET, POST, PUT, DELETE, etc.)',
			path: 'URL path pattern to match (regex)',
			find: 'Regex pattern to capture data, or cookie name if from=cookie',
			from: 'Location to search: request_body, request_header, response_body, response_header, cookie, any',
			required: 'Whether this capture is required for page and capture completion',
			rewrite: 'Rules for modifying request/response content',
			replace: 'Replacement text for the find pattern',
			to: 'Target phishing domain for this original domain'
		};

		return hoverData[word] || null;
	}
}

// Global provider instance to prevent duplicates
let globalProvider = null;

// Initialize the completion provider
export function setupProxyYamlCompletion(monaco) {
	// Dispose existing provider if it exists
	if (globalProvider) {
		globalProvider.dispose();
	}

	// Create new provider
	globalProvider = new ProxyYamlCompletionProvider(monaco);
	return globalProvider;
}
