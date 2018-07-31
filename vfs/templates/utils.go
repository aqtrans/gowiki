package templates

import (
	"html/template"
	"io/ioutil"
	"log"
	"strings"
)

func typeIcon(gitType string) template.HTML {
	var html template.HTML
	if gitType == "blob" {
		html = svg("file-text")
	}
	if gitType == "tree" {
		html = svg("folder-open")
	}
	return html
}

func svg(iconName string) template.HTML {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := ioutil.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return template.HTML("")
	}
	return template.HTML(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}

func svgByte(iconName string) []byte {
	// MAJOR TODO:
	// Check for file existence before trying to read the file; if non-existent return ""
	iconFile, err := ioutil.ReadFile("assets/icons/" + iconName + ".svg")
	if err != nil {
		log.Println("Error loading assets/icons/", iconName, err)
		return []byte("")
	}
	return []byte(`<div class="svg-icon">` + string(iconFile) + `</div>`)
}

func isLoggedIn(s string) bool {
	if s == "" {
		return false
	}
	return true
}

func jsTags(tagS []string) string {
	var tags string
	for _, v := range tagS {
		tags = tags + ", " + v
	}
	tags = strings.TrimPrefix(tags, ", ")
	tags = strings.TrimSuffix(tags, ", ")
	return tags
}
