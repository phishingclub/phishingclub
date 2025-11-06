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
					triggerCharacters: ['-'],
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

		// don't show autocomplete if user just typed a value after colon and space
		// (likely wants to continue to next line)
		const trimmedPrefix = linePrefix.trim();
		if (trimmedPrefix.includes(':')) {
			const afterColon = trimmedPrefix.split(':').pop().trim();
			// if there's already a value after the colon, don't autocomplete
			if (afterColon.length > 0 && !afterColon.endsWith('-')) {
				return { suggestions: [] };
			}
		}

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
			// determine context for mode - could be access or tls
			const context = this.findParentSection(linesAbove, currentIndent);
			if (context === 'tls') {
				return this.getTLSModeSuggestions(range);
			}
			return this.getAccessModeSuggestions(range);
		}
		if (linePrefix.match(/\bfrom:\s*["']?/)) {
			return this.getFromSuggestions(range);
		}
		if (linePrefix.match(/\s*method:\s*$/)) {
			return this.getMethodSuggestions(range);
		}
		if (linePrefix.match(/\s*on_deny:\s*$/)) {
			return this.getOnDenySuggestions(range);
		}

		// Handle array items
		if (linePrefix.match(/^\s*-\s*$/)) {
			const context = this.findParentSection(linesAbove, currentIndent);
			if (context === 'capture') {
				return this.getNewCaptureSuggestions(range);
			}
			if (context === 'rewrite') {
				return this.getNewRewriteSuggestions(range);
			}
			if (context === 'response') {
				return this.getNewResponseSuggestions(range);
			}
			if (context === 'rewrite_urls') {
				return this.getNewRewriteUrlsSuggestions(range);
			}
			if (
				context === 'query' &&
				this.findParentSection(linesAbove, currentIndent - 2) === 'rewrite_urls'
			) {
				return this.getNewQueryMappingSuggestions(range);
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
			case 'tls':
				return this.getTLSSuggestions(range);
			case 'access':
				return this.getAccessSuggestions(range);
			case 'impersonate':
				return this.getImpersonateSuggestions(range);
			case 'capture':
				return this.getCaptureSuggestions(range);
			case 'rewrite':
				return this.getRewriteSuggestions(range);
			case 'response':
				return this.getResponseSuggestions(range);
			case 'rewrite_urls':
				return this.getRewriteUrlsSuggestions(range);
			case 'query':
				// Check if we're in a rewrite_urls context
				if (this.findParentSection(linesAbove, currentIndent - 2) === 'rewrite_urls') {
					return this.getQueryMappingSuggestions(range);
				}
				return [];
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
				if (key === 'tls') return 'tls';
				if (key === 'access') return 'access';
				if (key === 'impersonate') return 'impersonate';
				if (key === 'capture') return 'capture';
				if (key === 'rewrite') return 'rewrite';
				if (key === 'rewrite_urls') return 'rewrite_urls';
				if (key === 'response') return 'response';
				if (key === 'on_deny') return 'on_deny';
				if (key === 'paths') return 'paths';
				if (key === 'query') return 'query';
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
				label: 'tls',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'tls:',
				documentation: 'Global TLS configuration (applies to all hosts unless overridden)',
				range
			},
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
			},
			{
				label: 'rewrite_urls',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'rewrite_urls:',
				documentation: 'Rules for rewriting URLs in responses',
				range
			},
			{
				label: 'impersonate',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'impersonate:',
				documentation: 'Client browser impersonation configuration using surf library',
				range
			}
		];
	}

	getDomainSuggestions(range) {
		return [
			{
				label: 'to',
				kind: this.monaco.languages.CompletionItemKind.Field,
				insertText: 'to: ',
				documentation: 'Phishing domain (where victims will visit)',
				range
			},
			{
				label: 'tls',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'tls:',
				documentation: 'TLS configuration for this domain (overrides global setting)',
				range
			},
			{
				label: 'access',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'access:',
				documentation: 'Access control configuration',
				range
			},
			{
				label: 'capture',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'capture:',
				documentation: 'Capture rules for this domain',
				range
			},
			{
				label: 'rewrite',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'rewrite:',
				documentation: 'Rewrite rules for this domain',
				range
			},
			{
				label: 'response',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'response:',
				documentation: 'Response rules for this domain',
				range
			},
			{
				label: 'rewrite_urls',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'rewrite_urls:',
				documentation: 'URL rewrite rules for anti-detection',
				range
			}
		];
	}

	getTLSSuggestions(range) {
		return [
			{
				label: 'mode',
				kind: this.monaco.languages.CompletionItemKind.Field,
				insertText: 'mode: ',
				documentation: 'TLS mode: "managed" (Let\'s Encrypt) or "self-signed"',
				range
			}
		];
	}

	getTLSModeSuggestions(range) {
		return [
			{
				label: '"managed"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"managed"',
				documentation: "Managed TLS via Let's Encrypt (DEFAULT)",
				range
			},
			{
				label: '"self-signed"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"self-signed"',
				documentation: 'Automatically generated self-signed certificates',
				range
			}
		];
	}

	getImpersonateSuggestions(range) {
		return [
			{
				label: 'enabled',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'enabled: true',
				documentation:
					'Enable surf browser impersonation based on JA4 fingerprint. Replicates client TLS fingerprint, HTTP/2 settings, header ordering, and platform',
				range
			},
			{
				label: 'retain_ua',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'retain_ua: false',
				documentation:
					"Retain client's original User-Agent header instead of using surf's impersonated one. Useful when you want fingerprint matching but original UA",
				range
			}
		];
	}

	getAccessSuggestions(range) {
		return [
			{
				label: 'mode',
				kind: this.monaco.languages.CompletionItemKind.Field,
				insertText: 'mode: ',
				documentation:
					'Access mode: "public" (allow all) or "private" (IP whitelist after lure access)',
				range
			},
			{
				label: 'on_deny',
				kind: this.monaco.languages.CompletionItemKind.Field,
				insertText: 'on_deny: ',
				documentation:
					'Action when access denied in private mode: "404", status code, or "redirect:URL"',
				range
			}
		];
	}

	getOnDenySuggestions(range) {
		return [
			{
				label: '"404"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"404"',
				documentation: 'Return 404 Not Found status',
				range
			},
			{
				label: '"403"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"403"',
				documentation: 'Return 403 Forbidden status',
				range
			},
			{
				label: '"https://example.com"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"https://example.com"',
				documentation: 'Redirect to specified URL (auto-detected)',
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

	getRewriteUrlsSuggestions(range) {
		return [
			{
				label: 'find',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'find: "^/path/to/match$"',
				documentation: 'Regex pattern to match URL path',
				range
			},
			{
				label: 'replace',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'replace: "/new/path"',
				documentation: 'Replacement path for matched URLs',
				range
			},
			{
				label: 'query',
				kind: this.monaco.languages.CompletionItemKind.Module,
				insertText: 'query:',
				documentation: 'Query parameter mappings',
				range
			},
			{
				label: 'filter',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'filter: ["client_id", "state"]',
				documentation: 'Query parameters to keep (if empty, keep all)',
				range
			}
		];
	}

	getNewRewriteUrlsSuggestions(range) {
		return [
			{
				label: 'url rewrite rule',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText:
					'find: "/common/oauth2/v2\\.0/authorize"\n  replace: "/signin"\n  query:\n    - find: "response_type"\n      replace: "type"\n  filter: ["client_id", "state"]',
				documentation: 'New URL rewrite rule for anti-detection',
				range
			},
			{
				label: 'simple path rewrite',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText: 'find: "/original/path"\n  replace: "/new/path"',
				documentation: 'Simple path rewrite without query changes',
				range
			}
		];
	}

	getQueryMappingSuggestions(range) {
		return [
			{
				label: 'find',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'find: "client_id"',
				documentation: 'Original query parameter name to find',
				range
			},
			{
				label: 'replace',
				kind: this.monaco.languages.CompletionItemKind.Property,
				insertText: 'replace: "app_id"',
				documentation: 'New query parameter name to replace with',
				range
			}
		];
	}

	getNewQueryMappingSuggestions(range) {
		return [
			{
				label: 'query mapping',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText: 'find: "client_id"\n  replace: "app_id"',
				documentation: 'Map query parameter names for anti-detection',
				range
			},
			{
				label: 'oauth parameter mapping',
				kind: this.monaco.languages.CompletionItemKind.Snippet,
				insertText: 'find: "response_type"\n  replace: "type"',
				documentation: 'Common OAuth parameter mapping',
				range
			}
		];
	}

	getAccessModeSuggestions(range) {
		return [
			{
				label: '"private"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"private"',
				documentation: 'Private mode - IP-based whitelist after lure access (DEFAULT, secure)',
				range
			},
			{
				label: '"public"',
				kind: this.monaco.languages.CompletionItemKind.Value,
				insertText: '"public"',
				documentation: 'Public mode - allow all traffic (traditional proxy behavior)',
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
			impersonate:
				'Client browser impersonation configuration. When enabled, uses surf library to replicate the exact TLS fingerprint, HTTP/2 settings, header ordering, and platform of the original client browser',
			enabled:
				'Enable surf browser impersonation based on client JA4 fingerprint. Detects browser (Chrome, Firefox, Safari, Edge) and platform (Windows, macOS, Linux, Android, iOS). Default: false',
			retain_ua:
				"Retain client's original User-Agent header instead of using surf's impersonated one. Useful when you want TLS/HTTP fingerprint matching but need to preserve the exact original User-Agent. Default: false",
			tls: 'TLS certificate configuration for proxy domains',
			access: 'Access control configuration (optional - defaults to private mode for security)',
			mode: 'Access control mode: "public" (allow all traffic) or "private" (IP whitelist after lure access, DEFAULT), OR TLS mode: "managed" (Let\'s Encrypt) or "self-signed"',
			on_deny:
				'Response when access is denied in private mode (e.g., "404", "https://example.com")',
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
			to: 'Target phishing domain for this original domain',
			rewrite_urls: 'URL rewrite rules for anti-detection - changes paths and query parameters',
			query: 'Query parameter mappings for URL rewriting',
			filter: 'Query parameters to keep (if empty, keep all)'
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
