package main

import (
    "testing"
    "io/ioutil"
    "bytes"
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
        t.Error("Converted Markdown does not equal test")
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
        t.Error("Converted Markdown does not equal test2")
    }
}

// Below is for testing the difference between just writing the Tags string directly as fed in from the wiki form, or using a []string as the source, but having to write them using a for loop
// Results, seems string is the best bet for now: 
//    BenchmarkBufferString-4    10000            996527 ns/op
//    BenchmarkBufferArray-4      1000           1651610 ns/op


var title string = "YEAH BENCHMARK OMG"
var name string = "WOOHOO"
var tagsarray = []string{"OMG","YEAH","WHAT","ZZZZ","FFFF","EEEE","RRRTRT","GRHTH","GBHFT","QPFLG","MGJHIB","LRIGJB","DJCUDK","WIFJV","GKBIBK","XKSDFM","RUFJS","SLDKF","ZKDJF","WIFKFG","EIFLG","DKFIBJ","WWRKG","SLFIBK","PRIVATE"}
var tagsstring string = "OMG, YEAH, WHAT, ZZZZ, FFFF, EEEE, RRRTRT, GRHTH, GBHFT, QPFLG, MGJHIB, LRIGJB, DJCUDK, WIFJV, GKBIBK, XKSDFM, RUFJS, SLDKF ,ZKDJF, WIFKFG, EIFLG, DKFIBJ, WWRKG, SLFIBK, PRIVATE"
var body string = "WOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO\n# OMG \n # YEAH"

func BenchmarkBufferString(b *testing.B) {
    for n := 0; n < b.N; n++ {
        var buffer bytes.Buffer
        buffer.WriteString("---\n")
        if title == "" {
            title = name
        }
        buffer.WriteString("title: "+ title)
        buffer.WriteString("\n")
        if tagsstring != "" {
            buffer.WriteString("tags: "+ tagsstring)
            buffer.WriteString("\n")
        }
        buffer.WriteString("---\n")
        buffer.WriteString(body)
        body = buffer.String()
    }
}

func BenchmarkBufferArray(b *testing.B) {
    for n := 0; n < b.N; n++ {
        var buffer bytes.Buffer
        buffer.WriteString("---\n")
        if title == "" {
            title = name
        }
        buffer.WriteString("title: "+ title)
        buffer.WriteString("\n")
        if tagsarray != nil {
            buffer.WriteString("tags: [")
            for _, v := range tagsarray {
                buffer.WriteString(v + " ")
            }
            buffer.WriteString("]")
            buffer.WriteString("\n")
        }
        buffer.WriteString("---\n")
        buffer.WriteString(body)
        body = buffer.String()
    }
}


// Below is for testing the difference between sorting through a []string and creating a []string using SplitString, then sorting through it

func benchmarkIsPrivate(size int, b *testing.B) {
    list := "testing"
    n := size
    for i := 0; i < n; i++ {
        list = " " + list
    }
    //tags := strings.Split(list, " ")
    for n := 0; n < b.N; n++ {
        isPrivate(list)
    }
}

func benchmarkIsPrivateArray(size int, b *testing.B) {
    list := []string{"testing"}
    n := size
    for i := 0; i < n; i++ {
        list = append(list, "testing")
    }
    //tags := strings.Split(list, " ")
    for n := 0; n < b.N; n++ {
        isPrivateA(list)
    }
}

//func BenchmarkIsPrivate2(b *testing.B) { benchmarkIsPrivate(2, b) }
//func BenchmarkIsPrivateArray2(b *testing.B) { benchmarkIsPrivateArray(2, b) }

//func BenchmarkIsPrivate10(b *testing.B) { benchmarkIsPrivate(10, b) }
//func BenchmarkIsPrivateArray10(b *testing.B) { benchmarkIsPrivateArray(10, b) }

//func BenchmarkIsPrivate100(b *testing.B) { benchmarkIsPrivate(100, b) }
//func BenchmarkIsPrivateArray100(b *testing.B) { benchmarkIsPrivateArray(100, b) }

//func BenchmarkIsPrivate1000(b *testing.B) { benchmarkIsPrivate(1000, b) }
//func BenchmarkIsPrivateArray1000(b *testing.B) { benchmarkIsPrivateArray(1000, b) }

func BenchmarkIsPrivate10000(b *testing.B) { benchmarkIsPrivate(10000, b) }
func BenchmarkIsPrivateArray10000(b *testing.B) { benchmarkIsPrivateArray(10000, b) }

//func BenchmarkIsPrivate100000(b *testing.B) { benchmarkIsPrivate(100000, b) }
//func BenchmarkIsPrivateArray100000(b *testing.B) { benchmarkIsPrivateArray(100000, b) }


