// +build !dev

package templates

import (
	"github.com/shurcooL/httpfs/html/vfstemplate"
	"github.com/shurcooL/httpfs/path/vfspath"
	"html/template"
	"jba.io/go/httputils"
	"log"
	"path/filepath"
)

func TmplInit() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	layouts, err := vfspath.Glob(Templates, "layouts/*.tmpl")
	if err != nil {
		log.Fatalln(err)
	}
	includes, err := vfspath.Glob(Templates, "includes/*.tmpl")
	if err != nil {
		log.Fatalln(err)
	}

	funcMap := template.FuncMap{"svg": svg, "typeIcon": typeIcon, "prettyDate": httputils.PrettyDate, "safeHTML": httputils.SafeHTML, "imgClass": httputils.ImgClass, "isLoggedIn": isLoggedIn, "jsTags": jsTags}

	for _, layout := range layouts {
		files := append(includes, layout)
		//DEBUG TEMPLATE LOADING
		//httputils.Debugln(files)
		tmpl, err := vfstemplate.ParseFiles(Templates, template.New("templates").Funcs(funcMap), files...)
		if err != nil {
			log.Fatalln(err)
		}
		templates[filepath.Base(layout)] = tmpl
	}
	return templates
}
