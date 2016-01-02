package main

import (
    "testing"
    "io/ioutil"
)

func TestMarkdownRender(t *testing.T) {
    // Read raw Markdown
    rawmdf := "./tests/test.md"
    rawmd, err := ioutil.ReadFile(rawmdf)	
    if err != nil {
		t.Error("Unable to access test.md")
    }
    // Read what rendered Markdown HTML should look like
    rendermdf := "./tests/test.html"
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

// Tests a corner case where stuff without markdown wasn't being rendered
func TestMarkdownRender2(t *testing.T) {
    // Read raw Markdown
    rawmdf := "./tests/test2.md"
    rawmd, err := ioutil.ReadFile(rawmdf)	
    if err != nil {
		t.Error("Unable to access test2.md")
    }
    // Read what rendered Markdown HTML should look like
    rendermdf := "./tests/test2.html"
    rendermd, err := ioutil.ReadFile(rendermdf)
    if err != nil {
		t.Error("Unable to access test2.html")
    }
    // []byte to string
    rendermds := string(rendermd)
    
	rawmds := markdownRender(rawmd)
    if rawmds != rendermds {
        t.Error("Converted Markdown does not equal readymade test")
    }
}