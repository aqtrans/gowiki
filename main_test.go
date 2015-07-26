package main

import (
    "testing"
    "io/ioutil"
)

func TestMarkdownRender(t *testing.T) {
    // Read raw Markdown
    rawmdf := "./test.md"
    rawmd, err := ioutil.ReadFile(rawmdf)	
    if err != nil {
		t.Error("Unable to access test.md")
    }
    // Read what rendered Markdown HTML should look like
    rendermdf := "./test.html"
    rendermd, err := ioutil.ReadFile(rendermdf)
    if err != nil {
		t.Error("Unable to access test.html")
    }
    // []byte to string
    rendermds := string(rendermd)
    
	rawmds := markdownRender(rawmd)
    if rawmds != rendermds {
        t.Error("Converted Markdown does not equal readymade test")
    }
}