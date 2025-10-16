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
					triggerCharacters: [' ', ':', '-', '"', "'"],
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
		if (linePrefix.match(/\s*engine:\s*$/)) {
			return this.getEngineSuggestions(range);
		}
		if (linePrefix.match(/\s*action:\s*$/)) {
			return this.getDomActionSuggestions(range);
		}
		if (linePrefix.match(/\s*target:\s*$/)) {
			return this.getTargetSuggestions(range);
		}
		if (linePrefix.match(/\s*mode:\s*$/)) {
			return this.getModeSuggestions(range);
		}
		if (linePrefix.match(/\bfrom:\s*["']?/)) {
			return this.getFromSuggestions(range);
		}
		if (linePrefix.match(/\s*method:\s*$/)) {
			return this.getMethodSuggestions(range);
		}
		if (linePrefix.match(/\s*(with_session|without_session):\s*$/)) {
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
			if (context === 'response') {
				return this.getNewResponseSuggestions(range);
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
			case 'response':
				return this.getResponseSuggestions(range);
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
				if (key === 'response') return 'response';
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
			},
			{
				label: 'response',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'response:',
				documentation: 'Global response rules',
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
			},
			{
				label: 'response',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'response:',
				documentation: 'Domain response rules',
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

	getResponseSuggestions(range) {
		return [
			{
				label: '- Response Rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText: [
					'- path: "^/path/pattern$"',
					'  status: 200',
					'  headers:',
					'    Content-Type: "application/json"',
					'  body: \'{"message": "Hello"}\'',
					'  forward: false'
				].join('\n  '),
				documentation: 'Complete response rule template',
				range
			},
			{
				label: 'path',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'path: "^/api/health$"',
				documentation: 'Regex pattern for request path',
				range
			},
			{
				label: 'status',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'status: 200',
				documentation: 'HTTP status code (default: 200)',
				range
			},
			{
				label: 'headers',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'headers:',
				documentation: 'Response headers',
				range
			},
			{
				label: 'body',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'body: "Response content"',
				documentation: 'Response body content (plain text/HTML/JSON/etc.)',
				range
			},
			{
				label: 'forward',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'forward: false',
				documentation: 'Whether to also forward request to target (default: false)',
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

	getRewriteSuggestions(range, linesAbove, current) {
		return [
			{
				label: 'name',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'name: "rewrite_name"',
				documentation: 'Optional rewrite rule name',
				range
			},
			{
				label: 'engine',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'engine: "regex"',
				documentation: 'Rewrite engine: "regex" (default) or "dom"',
				range
			},
			{
				label: 'find',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'find: "pattern"',
				documentation:
					'Pattern/selector to find (regex pattern for regex engine, CSS selector for dom engine)',
				range
			},
			{
				label: 'replace',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'replace: "replacement"',
				documentation: 'Replacement value (replacement text for regex, value for dom actions)',
				range
			},
			{
				label: 'action',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'action: "setText"',
				documentation:
					'DOM action: setText, setHtml, setAttr, removeAttr, addClass, removeClass, remove',
				range
			},
			{
				label: 'target',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'target: "all"',
				documentation: 'Target matching: "first", "last", "all" (default), "1,3,5", "2-4"',
				range
			},
			{
				label: 'from',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'from: "response_body"',
				documentation:
					'Where to apply replacement (regex engine only - dom engine always uses response_body)',
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
				label: 'regex rewrite rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "regex_rule"\n  find: "pattern"\n  replace: "replacement"\n  from: "response_body"',
				documentation: 'New regex-based rewrite rule template',
				range
			},
			{
				label: 'dom rewrite rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "dom_rule"\n  engine: "dom"\n  find: "css-selector"\n  action: "setText"\n  replace: "new-value"\n  target: "all"',
				documentation: 'New DOM-based rewrite rule template',
				range
			},
			{
				label: 'dom change title',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "change_title"\n  engine: "dom"\n  find: "title"\n  action: "setText"\n  replace: "Secure Login Portal"\n  target: "first"',
				documentation: 'Change page title using DOM',
				range
			},
			{
				label: 'dom modify form action',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "modify_form"\n  engine: "dom"\n  find: "form[action]"\n  action: "setAttr"\n  replace: "action:/evil/submit"\n  target: "all"',
				documentation: 'Modify form action attribute using DOM',
				range
			},
			{
				label: 'dom add CSS class',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "add_class"\n  engine: "dom"\n  find: ".login-form"\n  action: "addClass"\n  replace: "enhanced-security"\n  target: "all"',
				documentation: 'Add CSS class to elements using DOM',
				range
			},
			{
				label: 'dom remove elements',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "remove_warnings"\n  engine: "dom"\n  find: ".security-warning"\n  action: "remove"\n  target: "all"',
				documentation: 'Remove security warnings using DOM',
				range
			},
			{
				label: 'dom remove attribute',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'name: "remove_csrf"\n  engine: "dom"\n  find: "input[name=\'_token\']"\n  action: "removeAttr"\n  replace: "name"\n  target: "all"',
				documentation: 'Remove attributes using DOM',
				range
			}
		];
	}

	getNewResponseSuggestions(range) {
		return [
			{
				label: 'response rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'path: "^/api/health$"\n  status: 200\n  headers:\n    Content-Type: "application/json"\n  body: \'{"status": "ok"}\'\n  forward: false',
				documentation: 'New response rule template',
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

	getEngineSuggestions(range) {
		return [
			{
				label: '"regex"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"regex"',
				documentation: 'Regex-based replacement engine (default)',
				range
			},
			{
				label: '"dom"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"dom"',
				documentation: 'DOM manipulation engine for HTML elements',
				range
			}
		];
	}

	getTargetSuggestions(range) {
		return [
			{
				label: '"all"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"all"',
				documentation: 'Target all matching elements (default)',
				range
			},
			{
				label: '"first"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"first"',
				documentation: 'Target only the first matching element',
				range
			},
			{
				label: '"last"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"last"',
				documentation: 'Target only the last matching element',
				range
			},
			{
				label: '"1,3,5"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"1,3,5"',
				documentation: 'Target specific elements by index (comma-separated)',
				range
			},
			{
				label: '"2-4"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"2-4"',
				documentation: 'Target a range of elements (start-end)',
				range
			}
		];
	}

	getDomActionSuggestions(range) {
		return [
			{
				label: '"setText"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"setText"',
				documentation: 'Set the text content of selected elements (preserves HTML structure)',
				range
			},
			{
				label: '"setHtml"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"setHtml"',
				documentation: 'Set the HTML content of selected elements (replaces inner HTML)',
				range
			},
			{
				label: '"setAttr"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"setAttr"',
				documentation: 'Set attribute value (use value format: "attribute:value")',
				range
			},
			{
				label: '"removeAttr"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"removeAttr"',
				documentation: 'Remove attribute from selected elements',
				range
			},
			{
				label: '"addClass"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"addClass"',
				documentation: 'Add CSS class to selected elements',
				range
			},
			{
				label: '"removeClass"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"removeClass"',
				documentation: 'Remove CSS class from selected elements',
				range
			},
			{
				label: '"remove"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"remove"',
				documentation: 'Remove selected elements from DOM',
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
			find: 'Pattern to find: regex pattern (regex engine) or CSS selector (dom engine)',
			from: 'Location to search (regex engine only): request_body, request_header, response_body, response_header, cookie, any',
			required: 'Whether this capture is required for page and capture completion',
			response: 'Rules for custom responses to specific paths',
			status: 'HTTP status code for response (default: 200)',
			headers: 'HTTP headers to include in response',
			body: 'Response body content (plain text/HTML/JSON/etc.)',
			forward: 'Whether to also forward request to target server (default: false)',
			rewrite: 'Rules for modifying request/response content using regex or dom engines',
			replace: 'Replacement value: replacement text (regex engine) or value for dom actions',
			engine:
				'Rewrite engine: "regex" (default) for pattern replacement or "dom" for HTML manipulation',
			action: 'DOM action: setText, setHtml, setAttr, removeAttr, addClass, removeClass, remove',
			target:
				'Target matching: "first", "last", "all" (default), "1,3,5" (specific), "2-4" (range)',
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
