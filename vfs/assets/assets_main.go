// +build !dev

package assets

import (
	"html/template"
	"log"

	"github.com/shurcooL/httpfs/vfsutil"
)

func Svg(iconName string) template.HTML {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := vfsutil.ReadFile(Assets, "icons/"+iconName+".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return template.HTML("")
	}
	return template.HTML(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}

func SvgByte(iconName string) []byte {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := vfsutil.ReadFile(Assets, "icons/"+iconName+".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return []byte("")
	}
	return []byte(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}
