package main

import (
    //"github.com/golang-commonmark/markdown"
    //"github.com/rhinoman/go-commonmark"
    "github.com/russross/blackfriday"
    //"github.com/shurcooL/github_flavored_markdown"
    "io/ioutil"
    "log"
    //"io"
    "os"
)

const (
	EXTENSION_NO_INTRA_EMPHASIS          = 1 << iota // ignore emphasis markers inside words
	EXTENSION_TABLES                                 // render tables
	EXTENSION_FENCED_CODE                            // render fenced code blocks
	EXTENSION_AUTOLINK                               // detect embedded URLs that are not explicitly marked
	EXTENSION_STRIKETHROUGH                          // strikethrough text using ~~test~~
	EXTENSION_LAX_HTML_BLOCKS                        // loosen up HTML block parsing rules
	EXTENSION_SPACE_HEADERS                          // be strict about prefix header rules
	EXTENSION_HARD_LINE_BREAK                        // translate newlines into line breaks
	EXTENSION_TAB_SIZE_EIGHT                         // expand tabs to eight spaces instead of four
	EXTENSION_FOOTNOTES                              // Pandoc-style footnotes
	EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK             // No need to insert an empty line to start a (code, quote, ordered list, unordered list) block
	EXTENSION_HEADER_IDS                             // specify header IDs  with {#id}
	EXTENSION_TITLEBLOCK                             // Titleblock ala pandoc
	EXTENSION_AUTO_HEADER_IDS                        // Create the header ID from the text
	EXTENSION_BACKSLASH_LINE_BREAK                   // translate trailing backslashes into line breaks
	EXTENSION_DEFINITION_LISTS                       // render definition lists
        
    commonHtmlFlags = 0 |
		blackfriday.HTML_USE_XHTML |
		blackfriday.HTML_USE_SMARTYPANTS |
		blackfriday.HTML_SMARTYPANTS_FRACTIONS |
		blackfriday.HTML_SMARTYPANTS_DASHES |
		blackfriday.HTML_SMARTYPANTS_LATEX_DASHES
        
    commonExtensions = 0 |
		EXTENSION_NO_INTRA_EMPHASIS |
		EXTENSION_TABLES |
		EXTENSION_FENCED_CODE |
		EXTENSION_AUTOLINK |
		EXTENSION_STRIKETHROUGH |
		EXTENSION_SPACE_HEADERS |
		EXTENSION_HEADER_IDS |
		EXTENSION_BACKSLASH_LINE_BREAK |
		EXTENSION_DEFINITION_LISTS |
        EXTENSION_NO_EMPTY_LINE_BEFORE_BLOCK |
        EXTENSION_FOOTNOTES  
)

func markdownCommon(input []byte) []byte {
	renderer := blackfriday.HtmlRenderer(commonHtmlFlags, "", "")
	return blackfriday.MarkdownOptions(input, renderer, blackfriday.Options{
		Extensions: commonExtensions})    
}

func main() {
    filename := "../tests/test.md"
    body, err := ioutil.ReadFile(filename)
    if err != nil {
		log.Println(err)
    }
    html, err := os.Create("../tests/test.html")
    if err != nil {
        log.Println(err)
    }
    defer html.Close()

    // With golang-commonmark/markdown:
	//md := markdown.New(markdown.HTML(true), markdown.Nofollow(true), markdown.Breaks(false))
	//mdr := md.RenderToString(body)
    
    // With rhinoman/go-commonmark:
    //md := commonmark.Md2Html(string(body), 4)
    //mdr := string(md)
    
    // With blackfriday:
    //md := blackfriday.MarkdownCommon(body)
    md := markdownCommon(body)
    
    //mdrr, err := io.WriteString(html, mdr)
    _, err = html.Write(md)
    if err != nil {
        log.Println(err)
        //log.Println(mdrr)
    }
    html.Close()    
}