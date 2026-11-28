package service

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// directTemplateLoads are the places that load a campaign template without
// the language loader, repository.CampaignTemplate.GetByIDForLanguage, by file
// and function with the number of loads. A load is a GetByID or GetByIDs call
// on a campaign template repository or service, or a campaign read with
// WithCampaignTemplate set. A template loaded this way always has the content of
// the template language. Every load in a send, serve, preview, lure, deny or
// program usability path must use the loader so a recipient gets the content
// of their stamped language. Add a new direct load here only when it reads
// what every language shares: page stage presence, names, identifiers, lure
// settings or the sender.
var directTemplateLoads = map[string]int{
	// flow progression and page visits read which stages exist and the
	// identifiers, the same in every language
	"proxy/proxy.go:ProxyHandler.buildCampaignFlowRedirectURL":  1,
	"proxy/proxy.go:ProxyHandler.handleCampaignFlowProgression": 1,
	"proxy/proxy.go:ProxyHandler.registerPageVisitEvent":        1,
	"proxy/proxy.go:ProxyHandler.getCampaignInfo":               1,

	// lure settings, closing and the lure URL read shared settings
	"service/campaign.go:Campaign.snapshotLureSettings":         1,
	"service/campaign.go:Campaign.HandleCloseCampaigns":         1,
	"service/campaign.go:Campaign.lureURLByCampaignRecipientID": 1,
	// report screenshots show the template language in this version
	"service/campaign.go:Campaign.attachReportScreenshots": 1,

	// the template service and readers of names, scope and settings
	"service/campaignTemplate.go:CampaignTemplate.GetByID":          1,
	"service/campaignTemplate.go:CampaignTemplate.UpdateByID":       1,
	"service/campaignTemplatePool.go:Campaign.validateTemplatePool": 1,
	"service/program.go:Program.validateScope":                      1,
	"service/programClone.go:Program.loadCloneSource":               1,
	"service/allowListing.go:AllowListing.readTemplates":            1,
	// the content registry reads the template it installed
	"service/contentregistry_dropped.go:ContentRegistry.campaignTemplateName": 1,
	"service/contentregistry_spec.go:campaignTemplateSpec":                    1,

	// the campaign lists and the template editor of the admin area
	"controller/campaign.go:Campaign.GetAll":                  1,
	"controller/campaign.go:Campaign.GetAllActive":            1,
	"controller/campaign.go:Campaign.GetAllUpcoming":          1,
	"controller/campaign.go:Campaign.GetAllFinished":          1,
	"controller/campaign.go:Campaign.GetAllWithinDates":       1,
	"controller/campaignTemplate.go:CampaignTemplate.GetByID": 1,
}

// templateLoadDirs are the packages searched, seen from this package
var templateLoadDirs = []string{"../app", "../proxy", ".", "../controller"}

// TestNoNewDirectTemplateLoads fails when a campaign template is loaded
// without the language loader in a place that is not listed, or when a listed
// place no longer loads one, so the list stays exact
//
// LIST_TEMPLATE_LOADS=1 prints every place found.
func TestNoNewDirectTemplateLoads(t *testing.T) {
	found := map[string]int{}
	for _, dir := range templateLoadDirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			for site, n := range directTemplateLoadsIn(t, path) {
				found[site] += n
			}
		}
	}
	sites := map[string]bool{}
	for site := range found {
		sites[site] = true
	}
	for site := range directTemplateLoads {
		sites[site] = true
	}
	ordered := make([]string, 0, len(sites))
	for site := range sites {
		ordered = append(ordered, site)
	}
	sort.Strings(ordered)
	for _, site := range ordered {
		want, listed := directTemplateLoads[site]
		got := found[site]
		switch {
		case !listed:
			t.Errorf("%s loads a campaign template %d times without GetByIDForLanguage; use the loader, or list it when it reads only what every language shares", site, got)
		case got != want:
			t.Errorf("%s: %d direct template loads, %d listed; update the list", site, got, want)
		}
	}
}

// directTemplateLoadsIn counts the direct template loads in one file by
// function
func directTemplateLoadsIn(t *testing.T, path string) map[string]int {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(absPath(t, path))), filepath.Base(path)))
	out := map[string]int{}
	count := func(site string, body ast.Node) {
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "GetByID" && sel.Sel.Name != "GetByIDs") {
					return true
				}
				if strings.Contains(exprText(fset, sel.X), "CampaignTemplate") {
					out[site]++
				}
			case *ast.KeyValueExpr:
				if key, ok := x.Key.(*ast.Ident); ok && key.Name == "WithCampaignTemplate" {
					if v, ok := x.Value.(*ast.Ident); !ok || v.Name != "false" {
						out[site]++
					}
				}
			}
			return true
		})
	}
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Body == nil {
				continue
			}
			name := decl.Name.Name
			if decl.Recv != nil {
				name = receiverType(decl) + "." + name
			}
			count(rel+":"+name, decl.Body)
		case *ast.GenDecl:
			// a package level value such as a table of functions
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 {
					continue
				}
				for _, v := range vs.Values {
					count(rel+":"+vs.Names[0].Name, v)
				}
			}
		}
	}
	if os.Getenv("LIST_TEMPLATE_LOADS") != "" {
		for site, n := range out {
			fmt.Printf("%q: %d,\n", site, n)
		}
	}
	return out
}

func absPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func exprText(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return fmt.Sprint(e)
	}
	return buf.String()
}
