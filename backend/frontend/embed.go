package frontend

import (
	"embed"
	"html/template"
	"io/fs"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

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
