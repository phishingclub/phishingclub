package frontend

import (
	"embed"
	"html"
	"html/template"
	"io/fs"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// cspMetaRe matches the content security policy meta tag SvelteKit bakes into
// index.html during a hash mode build.
var cspMetaRe = regexp.MustCompile(`(?is)<meta[^>]+http-equiv=["']content-security-policy["'][^>]*>`)

// cspContentRe pulls the policy out of the meta tag's content attribute. A csp
// policy contains single quotes but never double quotes, so the content
// attribute is double quoted and its value runs to the next double quote.
var cspContentRe = regexp.MustCompile(`(?is)content="([^"]*)"`)

// ExtractCSPFromIndex reads build/index.html from the embedded file system,
// pulls the policy out of the SvelteKit content security policy meta tag, and
// returns that policy, the html with the meta tag removed, and whether a tag was
// found. When nothing is found it returns the original bytes and false so the
// caller can fall back to serving the document unchanged.
func ExtractCSPFromIndex(embedFS embed.FS) (policy string, indexHTML []byte, ok bool) {
	data, err := embedFS.ReadFile("build/index.html")
	if err != nil {
		return "", nil, false
	}
	tag := cspMetaRe.Find(data)
	if tag == nil {
		return "", data, false
	}
	m := cspContentRe.FindSubmatch(tag)
	if m == nil {
		return "", data, false
	}
	policy = strings.TrimSpace(html.UnescapeString(string(m[1])))
	if policy == "" {
		return "", data, false
	}
	stripped := cspMetaRe.ReplaceAll(data, []byte(""))
	return policy, stripped, true
}

// The version of gin I used when writting this, did not support using embeded files as html
// so I found this solution on good old https://stackoverflow.com/questions/26537299/golang-gin-framework-status-code-without-message-body

// LoadHTMLFromEmbedFS loads all files from the embeded file system that match the pattern
func LoadHTMLFromEmbedFS(engine *gin.Engine, embedFS embed.FS, pattern string) {
	root := template.New("")
	tmpl := template.Must(root, LoadAndAddToRoot(engine.FuncMap, root, embedFS, pattern))
	engine.SetHTMLTemplate(tmpl)
}

// LoadAndAddToRoot loads all files from the embeded file system that match the pattern and adds them to the root template
//
// Usage:
//
//	func (engine *gin.Engine) LoadHTMLFromFS(embedFS embed.FS, pattern string) {
//		root := template.New("")
//		tmpl := template.Must(root, LoadAndAddToRoot(engine.FuncMap, root, embedFS, pattern))
//		engine.SetHTMLTemplate(tmpl)
//	}
func LoadAndAddToRoot(funcMap template.FuncMap, rootTemplate *template.Template, embedFS embed.FS, pattern string) error {
	pattern = strings.ReplaceAll(pattern, ".", "\\.")
	pattern = strings.ReplaceAll(pattern, "*", ".*")

	err := fs.WalkDir(embedFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if matched, _ := regexp.MatchString(pattern, path); !d.IsDir() && matched {
			data, readErr := embedFS.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			t := rootTemplate.New(path).Funcs(funcMap)
			if _, parseErr := t.Parse(string(data)); parseErr != nil {
				return parseErr
			}
		}
		return nil
	})
	return err
}
