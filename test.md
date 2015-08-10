---
title: CommonMark Spec
author:
- John MacFarlane
version: 1
date: 2014-09-06
...

# Introduction

## What is Markdown?

Markdown is a plain text format for writing structured documents,
based on conventions used for indicating formatting in email and
usenet posts.  It was developed in 2004 by John Gruber, who wrote
the first Markdown-to-HTML converter in perl, and it soon became
widely used in websites.  By 2014 there were dozens of
implementations in many languages.  Some of them extended basic
Markdown syntax with conventions for footnotes, definition lists,
tables, and other constructs, and some allowed output not just in
HTML but in LaTeX and many other formats.

## Why is a spec needed?

John Gruber's [canonical description of Markdown's
syntax](http://daringfireball.net/projects/markdown/syntax)
does not specify the syntax unambiguously.  Here are some examples of
questions it does not answer:

1.  How much indentation is needed for a sublist?  The spec says that
    continuation paragraphs need to be indented four spaces, but is
    not fully explicit about sublists.  It is natural to think that
    they, too, must be indented four spaces, but `Markdown.pl` does
    not require that.  This is hardly a "corner case," and divergences
    between implementations on this issue often lead to surprises for
    users in real documents. (See [this comment by John
    Gruber](http://article.gmane.org/gmane.text.markdown.general/1997).)

2.  Is a blank line needed before a block quote or header?
    Most implementations do not require the blank line.  However,
    this can lead to unexpected results in hard-wrapped text, and
    also to ambiguities in parsing (note that some implementations
    put the header inside the blockquote, while others do not).
    (John Gruber has also spoken [in favor of requiring the blank
    lines](http://article.gmane.org/gmane.text.markdown.general/2146).)

3.  Is a blank line needed before an indented code block?
    (`Markdown.pl` requires it, but this is not mentioned in the
    documentation, and some implementations do not require it.)

    ``` markdown
    paragraph
        code?
    ```

4.  What is the exact rule for determining when list items get
    wrapped in `<p>` tags?  Can a list be partially "loose" and partially
    "tight"?  What should we do with a list like this?

    ``` markdown
    1. one

    2. two
    3. three
    ```

    Or this?

    ``` markdown
    1.  one
        - a

        - b
    2.  two
    ```

    (There are some relevant comments by John Gruber
    [here](http://article.gmane.org/gmane.text.markdown.general/2554).)

5.  Can list markers be indented?  Can ordered list markers be right-aligned?

    ``` markdown
     8. item 1
     9. item 2
    10. item 2a
    ```

6.  Is this one list with a horizontal rule in its second item,
    or two lists separated by a horizontal rule?

    ``` markdown
    * a
    * * * * *
    * b
    ```

7.  When list markers change from numbers to bullets, do we have
    two lists or one?  (The Markdown syntax description suggests two,
    but the perl scripts and many other implementations produce one.)

    ``` markdown
    1. fee
    2. fie
    -  foe
    -  fum
    ```

8.  What are the precedence rules for the markers of inline structure?
    For example, is the following a valid link, or does the code span
    take precedence ?

    ``` markdown
    [a backtick (`)](/url) and [another backtick (`)](/url).
    ```

9.  What are the precedence rules for markers of emphasis and strong
    emphasis?  For example, how should the following be parsed?

    ``` markdown
    *foo *bar* baz*
    ```

10. What are the precedence rules between block-level and inline-level
    structure?  For example, how should the following be parsed?

    ``` markdown
    - `a long code span can contain a hyphen like this
      - and it can screw things up`
    ```

11. Can list items include headers?  (`Markdown.pl` does not allow this,
    but headers can occur in blockquotes.)

    ``` markdown
    - # Heading
    ```

12. Can link references be defined inside block quotes or list items?

    ``` markdown
    > Blockquote [foo].
    >
    > [foo]: /url
    ```

13. If there are multiple definitions for the same reference, which takes
    precedence?

    ``` markdown
    [foo]: /url1
    [foo]: /url2

    [foo][]
    ```

In the absence of a spec, early implementers consulted `Markdown.pl`
to resolve these ambiguities.  But `Markdown.pl` was quite buggy, and
gave manifestly bad results in many cases, so it was not a
satisfactory replacement for a spec.

Because there is no unambiguous spec, implementations have diverged
considerably.  As a result, users are often surprised to find that
a document that renders one way on one system (say, a github wiki)
renders differently on another (say, converting to docbook using
pandoc).  To make matters worse, because nothing in Markdown counts
as a "syntax error," the divergence often isn't discovered right away.

## About this document

This document attempts to specify Markdown syntax unambiguously.
It contains many examples with side-by-side Markdown and
HTML.  These are intended to double as conformance tests.  An
accompanying script `runtests.pl` can be used to run the tests
against any Markdown program:

    perl runtests.pl spec.txt PROGRAM

Since this document describes how Markdown is to be parsed into
an abstract syntax tree, it would have made sense to use an abstract
representation of the syntax tree instead of HTML.  But HTML is capable
of representing the structural distinctions we need to make, and the
choice of HTML for the tests makes it possible to run the tests against
an implementation without writing an abstract syntax tree renderer.

This document is generated from a text file, `spec.txt`, written
in Markdown with a small extension for the side-by-side tests.
The script `spec2md.pl` can be used to turn `spec.txt` into pandoc
Markdown, which can then be converted into other formats.

In the examples, the `→` character is used to represent tabs.

# Preprocessing

A [line](#line) <a id="line"></a>
is a sequence of zero or more characters followed by a line
ending (CR, LF, or CRLF) or by the end of
file.

This spec does not specify an encoding; it thinks of lines as composed
of characters rather than bytes.  A conforming parser may be limited
to a certain encoding.

Tabs in lines are expanded to spaces, with a tab stop of 4 characters:

.
→foo→baz→→bim
.
<pre><code>foo baz     bim
</code></pre>
.

.
    a→a
    ὐ→a
.
<pre><code>a   a
ὐ   a
</code></pre>
.

Line endings are replaced by newline characters (LF).

A line containing no characters, or a line containing only spaces (after
tab expansion), is called a [blank line](#blank-line).
<a id="blank-line"></a>

# Blocks and inlines

We can think of a document as a sequence of [blocks](#block)<a
id="block"></a>---structural elements like paragraphs, block quotations,
lists, headers, rules, and code blocks.  Blocks can contain other
blocks, or they can contain [inline](#inline)<a id="inline"></a> content:
words, spaces, links, emphasized text, images, and inline code.

## Precedence

Indicators of block structure always take precedence over indicators
of inline structure.  So, for example, the following is a list with
two items, not a list with one item containing a code span:

.
- `one
- two`
.
<ul>
<li>`one</li>
<li>two`</li>
</ul>
.

This means that parsing can proceed in two steps:  first, the block
structure of the document can be discerned; second, text lines inside
paragraphs, headers, and other block constructs can be parsed for inline
structure.  The second step requires information about link reference
definitions that will be available only at the end of the first
step.  Note that the first step requires processing lines in sequence,
but the second can be parallelized, since the inline parsing of
one block element does not affect the inline parsing of any other.

## Container blocks and leaf blocks

We can divide blocks into two types:
[container blocks](#container-block), <a id="container-block"></a>
which can contain other blocks, and [leaf blocks](#leaf-block),
<a id="leaf-block"></a> which cannot.

# Leaf blocks

This section describes the different kinds of leaf block that make up a
Markdown document.

## Horizontal rules

A line consisting of 0-3 spaces of indentation, followed by a sequence
of three or more matching `-`, `_`, or `*` characters, each followed
optionally by any number of spaces, forms a [horizontal
rule](#horizontal-rule). <a id="horizontal-rule"></a>

.
***
---
___
.
<hr />
<hr />
<hr />
.

Wrong characters:

.
+++
.
<p>+++</p>
.

.
===
.
<p>===</p>
.

Not enough characters:

.
--
**
__
.
<p>--
**
__</p>
.

One to three spaces indent are allowed:

.
 ***
  ***
   ***
.
<hr />
<hr />
<hr />
.

Four spaces is too many:

.
    ***
.
<pre><code>***
</code></pre>
.

.
Foo
    ***
.
<p>Foo
***</p>
.

More than three characters may be used:

.
_____________________________________
.
<hr />
.

Spaces are allowed between the characters:

.
 - - -
.
<hr />
.

.
 **  * ** * ** * **
.
<hr />
.

.
-     -      -      -
.
<hr />
.

Spaces are allowed at the end:

.
- - - -    
.
<hr />
.

However, no other characters may occur at the end or the
beginning:

.
_ _ _ _ a

a------
.
<p>_ _ _ _ a</p>
<p>a------</p>
.

It is required that all of the non-space characters be the same.
So, this is not a horizontal rule:

.
 *-*
.
<p><em>-</em></p>
.

Horizontal rules do not need blank lines before or after:

.
- foo
***
- bar
.
<ul>
<li>foo</li>
</ul>
<hr />
<ul>
<li>bar</li>
</ul>
.

Horizontal rules can interrupt a paragraph:

.
Foo
***
bar
.
<p>Foo</p>
<hr />
<p>bar</p>
.

Note, however, that this is a setext header, not a paragraph followed
by a horizontal rule:

.
Foo
---
bar
.
<h2>Foo</h2>
<p>bar</p>
.

When both a horizontal rule and a list item are possible
interpretations of a line, the horizontal rule is preferred:

.
* Foo
* * *
* Bar
.
<ul>
<li>Foo</li>
</ul>
<hr />
<ul>
<li>Bar</li>
</ul>
.

If you want a horizontal rule in a list item, use a different bullet:

.
- Foo
- * * *
.
<ul>
<li>Foo</li>
<li><hr /></li>
</ul>
.

## ATX headers

An [ATX header](#atx-header) <a id="atx-header"></a>
consists of a string of characters, parsed as inline content, between an
opening sequence of 1--6 unescaped `#` characters and an optional
closing sequence of any number of `#` characters.  The opening sequence
of `#` characters cannot be followed directly by a nonspace character.
The closing `#` characters may be followed by spaces only.  The opening
`#` character may be indented 0-3 spaces.  The raw contents of the
header are stripped of leading and trailing spaces before being parsed
as inline content.  The header level is equal to the number of `#`
characters in the opening sequence.

Simple headers:

.
# foo
## foo
### foo
#### foo
##### foo
###### foo
.
<h1>foo</h1>
<h2>foo</h2>
<h3>foo</h3>
<h4>foo</h4>
<h5>foo</h5>
<h6>foo</h6>
.

More than six `#` characters is not a header:

.
####### foo
.
<p>####### foo</p>
.

A space is required between the `#` characters and the header's
contents.  Note that many implementations currently do not require
the space.  However, the space was required by the [original ATX
implementation](http://www.aaronsw.com/2002/atx/atx.py), and it helps
prevent things like the following from being parsed as headers:

.
#5 bolt
.
<p>#5 bolt</p>
.

This is not a header, because the first `#` is escaped:

.
\## foo
.
<p>## foo</p>
.

Contents are parsed as inlines:

.
# foo *bar* \*baz\*
.
<h1>foo <em>bar</em> *baz*</h1>
.

Leading and trailing blanks are ignored in parsing inline content:

.
#                  foo                     
.
<h1>foo</h1>
.

One to three spaces indentation are allowed:

.
 ### foo
  ## foo
   # foo
.
<h3>foo</h3>
<h2>foo</h2>
<h1>foo</h1>
.

Four spaces are too much:

.
    # foo
.
<pre><code># foo
</code></pre>
.

.
foo
    # bar
.
<p>foo
# bar</p>
.

A closing sequence of `#` characters is optional:

.
## foo ##
  ###   bar    ###
.
<h2>foo</h2>
<h3>bar</h3>
.

It need not be the same length as the opening sequence:

.
# foo ##################################
##### foo ##
.
<h1>foo</h1>
<h5>foo</h5>
.

Spaces are allowed after the closing sequence:

.
### foo ###     
.
<h3>foo</h3>
.

A sequence of `#` characters with a nonspace character following it
is not a closing sequence, but counts as part of the contents of the
header:

.
### foo ### b
.
<h3>foo ### b</h3>
.

Backslash-escaped `#` characters do not count as part
of the closing sequence:

.
### foo \###
## foo \#\##
# foo \#
.
<h3>foo #</h3>
<h2>foo ##</h2>
<h1>foo #</h1>
.

ATX headers need not be separated from surrounding content by blank
lines, and they can interrupt paragraphs:

.
****
## foo
****
.
<hr />
<h2>foo</h2>
<hr />
.

.
Foo bar
# baz
Bar foo
.
<p>Foo bar</p>
<h1>baz</h1>
<p>Bar foo</p>
.

ATX headers can be empty:

.
## 
#
### ###
.
<h2></h2>
<h1></h1>
<h3></h3>
.

## Setext headers

A [setext header](#setext-header) <a id="setext-header"></a>
consists of a line of text, containing at least one nonspace character,
with no more than 3 spaces indentation, followed by a [setext header
underline](#setext-header-underline).  A [setext header
underline](#setext-header-underline) <a id="setext-header-underline"></a>
is a sequence of `=` characters or a sequence of `-` characters, with no
more than 3 spaces indentation and any number of trailing
spaces.  The header is a level 1 header if `=` characters are used, and
a level 2 header if `-` characters are used.  The contents of the header
are the result of parsing the first line as Markdown inline content.

In general, a setext header need not be preceded or followed by a
blank line.  However, it cannot interrupt a paragraph, so when a
setext header comes after a paragraph, a blank line is needed between
them.

Simple examples:

.
Foo *bar*
=========

Foo *bar*
---------
.
<h1>Foo <em>bar</em></h1>
<h2>Foo <em>bar</em></h2>
.

The underlining can be any length:

.
Foo
-------------------------

Foo
=
.
<h2>Foo</h2>
<h1>Foo</h1>
.

The header content can be indented up to three spaces, and need
not line up with the underlining:

.
   Foo
---

  Foo
-----

  Foo
  ===
.
<h2>Foo</h2>
<h2>Foo</h2>
<h1>Foo</h1>
.

Four spaces indent is too much:

.
    Foo
    ---

    Foo
---
.
<pre><code>Foo
---

Foo
</code></pre>
<hr />
.

The setext header underline can be indented up to three spaces, and
may have trailing spaces:

.
Foo
   ----      
.
<h2>Foo</h2>
.

Four spaces is too much:

.
Foo
     ---
.
<p>Foo
---</p>
.

The setext header underline cannot contain internal spaces:

.
Foo
= =

Foo
--- -
.
<p>Foo
= =</p>
<p>Foo</p>
<hr />
.

Trailing spaces in the content line do not cause a line break:

.
Foo  
-----
.
<h2>Foo</h2>
.

Nor does a backslash at the end:

.
Foo\
----
.
<h2>Foo\</h2>
.

Since indicators of block structure take precedence over
indicators of inline structure, the following are setext headers:

.
`Foo
----
`

<a title="a lot
---
of dashes"/>
.
<h2>`Foo</h2>
<p>`</p>
<h2>&lt;a title=&quot;a lot</h2>
<p>of dashes&quot;/&gt;</p>
.

The setext header underline cannot be a lazy line:

.
> Foo
---
.
<blockquote>
<p>Foo</p>
</blockquote>
<hr />
.

A setext header cannot interrupt a paragraph:

.
Foo
Bar
---

Foo
Bar
===
.
<p>Foo
Bar</p>
<hr />
<p>Foo
Bar
===</p>
.

But in general a blank line is not required before or after:

.
---
Foo
---
Bar
---
Baz
.
<hr />
<h2>Foo</h2>
<h2>Bar</h2>
<p>Baz</p>
.

Setext headers cannot be empty:

.

====
.
<p>====</p>
.


## Indented code blocks

An [indented code block](#indented-code-block)
<a id="indented-code-block"></a> is composed of one or more
[indented chunks](#indented-chunk) separated by blank lines.
An [indented chunk](#indented-chunk) <a id="indented-chunk"></a>
is a sequence of non-blank lines, each indented four or more
spaces.  An indented code block cannot interrupt a paragraph, so
if it occurs before or after a paragraph, there must be an
intervening blank line.  The contents of the code block are
the literal contents of the lines, including trailing newlines,
minus four spaces of indentation. An indented code block has no
attributes.

.
    a simple
      indented code block
.
<pre><code>a simple
  indented code block
</code></pre>
.

The contents are literal text, and do not get parsed as Markdown:

.
    <a/>
    *hi*

    - one
.
<pre><code>&lt;a/&gt;
*hi*

- one
</code></pre>
.

Here we have three chunks separated by blank lines:

.
    chunk1

    chunk2
  
 
 
    chunk3
.
<pre><code>chunk1

chunk2



chunk3
</code></pre>
.

Any initial spaces beyond four will be included in the content, even
in interior blank lines:

.
    chunk1
      
      chunk2
.
<pre><code>chunk1
  
  chunk2
</code></pre>
.

An indented code block cannot interrupt a paragraph.  (This
allows hanging indents and the like.)

.
Foo
    bar

.
<p>Foo
bar</p>
.

However, any non-blank line with fewer than four leading spaces ends
the code block immediately.  So a paragraph may occur immediately
after indented code:

.
    foo
bar
.
<pre><code>foo
</code></pre>
<p>bar</p>
.

And indented code can occur immediately before and after other kinds of
blocks:

.
# Header
    foo
Header
------
    foo
----
.
<h1>Header</h1>
<pre><code>foo
</code></pre>
<h2>Header</h2>
<pre><code>foo
</code></pre>
<hr />
.

The first line can be indented more than four spaces:

.
        foo
    bar
.
<pre><code>    foo
bar
</code></pre>
.

Blank lines preceding or following an indented code block
are not included in it:

.

    
    foo
    

.
<pre><code>foo
</code></pre>
.

Trailing spaces are included in the code block's content:

.
    foo  
.
<pre><code>foo  
</code></pre>
.


## Fenced code blocks

A [code fence](#code-fence) <a id="code-fence"></a> is a sequence
of at least three consecutive backtick characters (`` ` ``) or
tildes (`~`).  (Tildes and backticks cannot be mixed.)
A [fenced code block](#fenced-code-block) <a id="fenced-code-block"></a>
begins with a code fence, indented no more than three spaces.

The line with the opening code fence may optionally contain some text
following the code fence; this is trimmed of leading and trailing
spaces and called the [info string](#info-string).
<a id="info-string"></a> The info string may not contain any backtick
characters.  (The reason for this restriction is that otherwise
some inline code would be incorrectly interpreted as the
beginning of a fenced code block.)

The content of the code block consists of all subsequent lines, until
a closing [code fence](#code-fence) of the same type as the code block
began with (backticks or tildes), and with at least as many backticks
or tildes as the opening code fence.  If the leading code fence is
indented N spaces, then up to N spaces of indentation are removed from
each line of the content (if present).  (If a content line is not
indented, it is preserved unchanged.  If it is indented less than N
spaces, all of the indentation is removed.)

The closing code fence may be indented up to three spaces, and may be
followed only by spaces, which are ignored.  If the end of the
containing block (or document) is reached and no closing code fence
has been found, the code block contains all of the lines after the
opening code fence until the end of the containing block (or
document).  (An alternative spec would require backtracking in the
event that a closing code fence is not found.  But this makes parsing
much less efficient, and there seems to be no real down side to the
behavior described here.)

A fenced code block may interrupt a paragraph, and does not require
a blank line either before or after.

The content of a code fence is treated as literal text, not parsed
as inlines.  The first word of the info string is typically used to
specify the language of the code sample, and rendered in the `class`
attribute of the `code` tag.  However, this spec does not mandate any
particular treatment of the info string.

Here is a simple example with backticks:

.
```
<
 >
```
.
<pre><code>&lt;
 &gt;
</code></pre>
.

With tildes:

.
~~~
<
 >
~~~
.
<pre><code>&lt;
 &gt;
</code></pre>
.

The closing code fence must use the same character as the opening
fence:

.
```
aaa
~~~
```
.
<pre><code>aaa
~~~
</code></pre>
.

.
~~~
aaa
```
~~~
.
<pre><code>aaa
```
</code></pre>
.

The closing code fence must be at least as long as the opening fence:

.
````
aaa
```
``````
.
<pre><code>aaa
```
</code></pre>
.

.
~~~~
aaa
~~~
~~~~
.
<pre><code>aaa
~~~
</code></pre>
.

Unclosed code blocks are closed by the end of the document:

.
```
.
<pre><code></code></pre>
.

.
`````

```
aaa
.
<pre><code>
```
aaa
</code></pre>
.

A code block can have all empty lines as its content:

.
```

  
```
.
<pre><code>
  
</code></pre>
.

A code block can be empty:

.
```
```
.
<pre><code></code></pre>
.

Fences can be indented.  If the opening fence is indented,
content lines will have equivalent opening indentation removed,
if present:

.
 ```
 aaa
aaa
```
.
<pre><code>aaa
aaa
</code></pre>
.

.
  ```
aaa
  aaa
aaa
  ```
.
<pre><code>aaa
aaa
aaa
</code></pre>
.

.
   ```
   aaa
    aaa
  aaa
   ```
.
<pre><code>aaa
 aaa
aaa
</code></pre>
.

Four spaces indentation produces an indented code block:

.
    ```
    aaa
    ```
.
<pre><code>```
aaa
```
</code></pre>
.

Code fences (opening and closing) cannot contain internal spaces:

.
``` ```
aaa
.
<p><code></code>
aaa</p>
.

.
~~~~~~
aaa
~~~ ~~
.
<pre><code>aaa
~~~ ~~
</code></pre>
.

Fenced code blocks can interrupt paragraphs, and can be followed
directly by paragraphs, without a blank line between:

.
foo
```
bar
```
baz
.
<p>foo</p>
<pre><code>bar
</code></pre>
<p>baz</p>
.

Other blocks can also occur before and after fenced code blocks
without an intervening blank line:

.
foo
---
~~~
bar
~~~
# baz
.
<h2>foo</h2>
<pre><code>bar
</code></pre>
<h1>baz</h1>
.

An [info string](#info-string) can be provided after the opening code fence.
Opening and closing spaces will be stripped, and the first word, prefixed
with `language-`, is used as the value for the `class` attribute of the
`code` element within the enclosing `pre` element.

.
```ruby
def foo(x)
  return 3
end
```
.
<pre><code class="language-ruby">def foo(x)
  return 3
end
</code></pre>
.

.
~~~~    ruby startline=3 $%@#$
def foo(x)
  return 3
end
~~~~~~~
.
<pre><code class="language-ruby">def foo(x)
  return 3
end
</code></pre>
.

.
````;
````
.
<pre><code class="language-;"></code></pre>
.

Info strings for backtick code blocks cannot contain backticks:

.
``` aa ```
foo
.
<p><code>aa</code>
foo</p>
.

Closing code fences cannot have info strings:

.
```
``` aaa
```
.
<pre><code>``` aaa
</code></pre>
.


## HTML blocks

An [HTML block tag](#html-block-tag) <a id="html-block-tag"></a> is
an [open tag](#open-tag) or [closing tag](#closing-tag) whose tag
name is one of the following (case-insensitive):
`article`, `header`, `aside`, `hgroup`, `blockquote`, `hr`, `iframe`,
`body`, `li`, `map`, `button`, `object`, `canvas`, `ol`, `caption`,
`output`, `col`, `p`, `colgroup`, `pre`, `dd`, `progress`, `div`,
`section`, `dl`, `table`, `td`, `dt`, `tbody`, `embed`, `textarea`,
`fieldset`, `tfoot`, `figcaption`, `th`, `figure`, `thead`, `footer`,
`footer`, `tr`, `form`, `ul`, `h1`, `h2`, `h3`, `h4`, `h5`, `h6`,
`video`, `script`, `style`.

An [HTML block](#html-block) <a id="html-block"></a> begins with an
[HTML block tag](#html-block-tag), [HTML comment](#html-comment),
[processing instruction](#processing-instruction),
[declaration](#declaration), or [CDATA section](#cdata-section).
It ends when a [blank line](#blank-line) or the end of the
input is encountered.  The initial line may be indented up to three
spaces, and subsequent lines may have any indentation.  The contents
of the HTML block are interpreted as raw HTML, and will not be escaped
in HTML output.

Some simple examples:

.
<table>
  <tr>
    <td>
           hi
    </td>
  </tr>
</table>

okay.
.
<table>
  <tr>
    <td>
           hi
    </td>
  </tr>
</table>
<p>okay.</p>
.

.
 <div>
  *hello*
         <foo><a>
.
 <div>
  *hello*
         <foo><a>
.

Here we have two code blocks with a Markdown paragraph between them:

.
<DIV CLASS="foo">

*Markdown*

</DIV>
.
<DIV CLASS="foo">
<p><em>Markdown</em></p>
</DIV>
.

In the following example, what looks like a Markdown code block
is actually part of the HTML block, which continues until a blank
line or the end of the document is reached:

.
<div></div>
``` c
int x = 33;
```
.
<div></div>
``` c
int x = 33;
```
.

A comment:

.
<!-- Foo
bar
   baz -->
.
<!-- Foo
bar
   baz -->
.

A processing instruction:

.
<?php
  echo 'foo'
?>
.
<?php
  echo 'foo'
?>
.

CDATA:

.
<![CDATA[
function matchwo(a,b)
{
if (a < b && a < 0) then
  {
  return 1;
  }
else
  {
  return 0;
  }
}
]]>
.
<![CDATA[
function matchwo(a,b)
{
if (a < b && a < 0) then
  {
  return 1;
  }
else
  {
  return 0;
  }
}
]]>
.

The opening tag can be indented 1-3 spaces, but not 4:

.
  <!-- foo -->

    <!-- foo -->
.
  <!-- foo -->
<pre><code>&lt;!-- foo --&gt;
</code></pre>
.

An HTML block can interrupt a paragraph, and need not be preceded
by a blank line.

.
Foo
<div>
bar
</div>
.
<p>Foo</p>
<div>
bar
</div>
.

However, a following blank line is always needed, except at the end of
a document:

.
<div>
bar
</div>
*foo*
.
<div>
bar
</div>
*foo*
.

An incomplete HTML block tag may also start an HTML block:

.
<div class
foo
.
<div class
foo
.

This rule differs from John Gruber's original Markdown syntax
specification, which says:

> The only restrictions are that block-level HTML elements —
> e.g. `<div>`, `<table>`, `<pre>`, `<p>`, etc. — must be separated from
> surrounding content by blank lines, and the start and end tags of the
> block should not be indented with tabs or spaces.

In some ways Gruber's rule is more restrictive than the one given
here:

- It requires that an HTML block be preceded by a blank line.
- It does not allow the start tag to be indented.
- It requires a matching end tag, which it also does not allow to
  be indented.

Indeed, most Markdown implementations, including some of Gruber's
own perl implementations, do not impose these restrictions.

There is one respect, however, in which Gruber's rule is more liberal
than the one given here, since it allows blank lines to occur inside
an HTML block.  There are two reasons for disallowing them here.
First, it removes the need to parse balanced tags, which is
expensive and can require backtracking from the end of the document
if no matching end tag is found. Second, it provides a very simple
and flexible way of including Markdown content inside HTML tags:
simply separate the Markdown from the HTML using blank lines:

.
<div>

*Emphasized* text.

</div>
.
<div>
<p><em>Emphasized</em> text.</p>
</div>
.

Compare:

.
<div>
*Emphasized* text.
</div>
.
<div>
*Emphasized* text.
</div>
.

Some Markdown implementations have adopted a convention of
interpreting content inside tags as text if the open tag has
the attribute `markdown=1`.  The rule given above seems a simpler and
more elegant way of achieving the same expressive power, which is also
much simpler to parse.

The main potential drawback is that one can no longer paste HTML
blocks into Markdown documents with 100% reliability.  However,
*in most cases* this will work fine, because the blank lines in
HTML are usually followed by HTML block tags.  For example:

.
<table>

<tr>

<td>
Hi
</td>

</tr>

</table>
.
<table>
<tr>
<td>
Hi
</td>
</tr>
</table>
.

Moreover, blank lines are usually not necessary and can be
deleted.  The exception is inside `<pre>` tags; here, one can
replace the blank lines with `&#10;` entities.

So there is no important loss of expressive power with the new rule.

## Link reference definitions

A [link reference definition](#link-reference-definition)
<a id="link-reference-definition"></a> consists of a [link
label](#link-label), indented up to three spaces, followed
by a colon (`:`), optional blank space (including up to one
newline), a [link destination](#link-destination), optional
blank space (including up to one newline), and an optional [link
title](#link-title), which if it is present must be separated
from the [link destination](#link-destination) by whitespace.
No further non-space characters may occur on the line.

A [link reference-definition](#link-reference-definition)
does not correspond to a structural element of a document.  Instead, it
defines a label which can be used in [reference links](#reference-link)
and reference-style [images](#image) elsewhere in the document.  [Link
reference definitions] can come either before or after the links that use
them.

.
[foo]: /url "title"

[foo]
.
<p><a href="/url" title="title">foo</a></p>
.

.
   [foo]: 
      /url  
           'the title'  

[foo]
.
<p><a href="/url" title="the title">foo</a></p>
.

.
[Foo*bar\]]:my_(url) 'title (with parens)'

[Foo*bar\]]
.
<p><a href="my_(url)" title="title (with parens)">Foo*bar]</a></p>
.

.
[Foo bar]:
<my url>
'title'

[Foo bar]
.
<p><a href="my%20url" title="title">Foo bar</a></p>
.

The title may be omitted:

.
[foo]:
/url

[foo]
.
<p><a href="/url">foo</a></p>
.

The link destination may not be omitted:

.
[foo]:

[foo]
.
<p>[foo]:</p>
<p>[foo]</p>
.

A link can come before its corresponding definition:

.
[foo]

[foo]: url
.
<p><a href="url">foo</a></p>
.

If there are several matching definitions, the first one takes
precedence:

.
[foo]

[foo]: first
[foo]: second
.
<p><a href="first">foo</a></p>
.

As noted in the section on [Links], matching of labels is
case-insensitive (see [matches](#matches)).

.
[FOO]: /url

[Foo]
.
<p><a href="/url">Foo</a></p>
.

.
[ΑΓΩ]: /φου

[αγω]
.
<p><a href="/%CF%86%CE%BF%CF%85">αγω</a></p>
.

Here is a link reference definition with no corresponding link.
It contributes nothing to the document.

.
[foo]: /url
.
.

This is not a link reference definition, because there are
non-space characters after the title:

.
[foo]: /url "title" ok
.
<p>[foo]: /url &quot;title&quot; ok</p>
.

This is not a link reference definition, because it is indented
four spaces:

.
    [foo]: /url "title"

[foo]
.
<pre><code>[foo]: /url &quot;title&quot;
</code></pre>
<p>[foo]</p>
.

This is not a link reference definition, because it occurs inside
a code block:

.
```
[foo]: /url
```

[foo]
.
<pre><code>[foo]: /url
</code></pre>
<p>[foo]</p>
.

A [link reference definition](#link-reference-definition) cannot
interrupt a paragraph.

.
Foo
[bar]: /baz

[bar]
.
<p>Foo
[bar]: /baz</p>
<p>[bar]</p>
.

However, it can directly follow other block elements, such as headers
and horizontal rules, and it need not be followed by a blank line.

.
# [Foo]
[foo]: /url
> bar
.
<h1><a href="/url">Foo</a></h1>
<blockquote>
<p>bar</p>
</blockquote>
.

Several [link references](#link-reference) can occur one after another,
without intervening blank lines.

.
[foo]: /foo-url "foo"
[bar]: /bar-url
  "bar"
[baz]: /baz-url

[foo],
[bar],
[baz]
.
<p><a href="/foo-url" title="foo">foo</a>,
<a href="/bar-url" title="bar">bar</a>,
<a href="/baz-url">baz</a></p>
.

[Link reference definitions](#link-reference-definition) can occur
inside block containers, like lists and block quotations.  They
affect the entire document, not just the container in which they
are defined:

.
[foo]

> [foo]: /url
.
<p><a href="/url">foo</a></p>
<blockquote>
</blockquote>
.


## Paragraphs

A sequence of non-blank lines that cannot be interpreted as other
kinds of blocks forms a [paragraph](#paragraph).<a id="paragraph"></a>
The contents of the paragraph are the result of parsing the
paragraph's raw content as inlines.  The paragraph's raw content
is formed by concatenating the lines and removing initial and final
spaces.

A simple example with two paragraphs:

.
aaa

bbb
.
<p>aaa</p>
<p>bbb</p>
.

Paragraphs can contain multiple lines, but no blank lines:

.
aaa
bbb

ccc
ddd
.
<p>aaa
bbb</p>
<p>ccc
ddd</p>
.

Multiple blank lines between paragraph have no effect:

.
aaa


bbb
.
<p>aaa</p>
<p>bbb</p>
.

Leading spaces are skipped:

.
  aaa
 bbb
.
<p>aaa
bbb</p>
.

Lines after the first may be indented any amount, since indented
code blocks cannot interrupt paragraphs.

.
aaa
             bbb
                                       ccc
.
<p>aaa
bbb
ccc</p>
.

However, the first line may be indented at most three spaces,
or an indented code block will be triggered:

.
   aaa
bbb
.
<p>aaa
bbb</p>
.

.
    aaa
bbb
.
<pre><code>aaa
</code></pre>
<p>bbb</p>
.

Final spaces are stripped before inline parsing, so a paragraph
that ends with two or more spaces will not end with a hard line
break:

.
aaa     
bbb     
.
<p>aaa<br />
bbb</p>
.

## Blank lines

[Blank lines](#blank-line) between block-level elements are ignored,
except for the role they play in determining whether a [list](#list)
is [tight](#tight) or [loose](#loose).

Blank lines at the beginning and end of the document are also ignored.

.
  

aaa
  

# aaa

  
.
<p>aaa</p>
<h1>aaa</h1>
.


# Container blocks

A [container block](#container-block) is a block that has other
blocks as its contents.  There are two basic kinds of container blocks:
[block quotes](#block-quote) and [list items](#list-item).
[Lists](#list) are meta-containers for [list items](#list-item).

We define the syntax for container blocks recursively.  The general
form of the definition is:

> If X is a sequence of blocks, then the result of
> transforming X in such-and-such a way is a container of type Y
> with these blocks as its content.

So, we explain what counts as a block quote or list item by explaining
how these can be *generated* from their contents. This should suffice
to define the syntax, although it does not give a recipe for *parsing*
these constructions.  (A recipe is provided below in the section entitled
[A parsing strategy](#appendix-a-a-parsing-strategy).)

## Block quotes

A [block quote marker](#block-quote-marker) <a id="block-quote-marker"></a>
consists of 0-3 spaces of initial indent, plus (a) the character `>` together
with a following space, or (b) a single character `>` not followed by a space.

The following rules define [block quotes](#block-quote):
<a id="block-quote"></a>

1.  **Basic case.**  If a string of lines *Ls* constitute a sequence
    of blocks *Bs*, then the result of appending a [block quote
    marker](#block-quote-marker) to the beginning of each line in *Ls*
    is a [block quote](#block-quote) containing *Bs*.

2.  **Laziness.**  If a string of lines *Ls* constitute a [block
    quote](#block-quote) with contents *Bs*, then the result of deleting
    the initial [block quote marker](#block-quote-marker) from one or
    more lines in which the next non-space character after the [block
    quote marker](#block-quote-marker) is [paragraph continuation
    text](#paragraph-continuation-text) is a block quote with *Bs* as
    its content.  <a id="paragraph-continuation-text"></a>
    [Paragraph continuation text](#paragraph-continuation-text) is text
    that will be parsed as part of the content of a paragraph, but does
    not occur at the beginning of the paragraph.

3.  **Consecutiveness.**  A document cannot contain two [block
    quotes](#block-quote) in a row unless there is a [blank
    line](#blank-line) between them.

Nothing else counts as a [block quote](#block-quote).

Here is a simple example:

.
> # Foo
> bar
> baz
.
<blockquote>
<h1>Foo</h1>
<p>bar
baz</p>
</blockquote>
.

The spaces after the `>` characters can be omitted:

.
># Foo
>bar
> baz
.
<blockquote>
<h1>Foo</h1>
<p>bar
baz</p>
</blockquote>
.

The `>` characters can be indented 1-3 spaces:

.
   > # Foo
   > bar
 > baz
.
<blockquote>
<h1>Foo</h1>
<p>bar
baz</p>
</blockquote>
.

Four spaces gives us a code block:

.
    > # Foo
    > bar
    > baz
.
<pre><code>&gt; # Foo
&gt; bar
&gt; baz
</code></pre>
.

The Laziness clause allows us to omit the `>` before a
paragraph continuation line:

.
> # Foo
> bar
baz
.
<blockquote>
<h1>Foo</h1>
<p>bar
baz</p>
</blockquote>
.

A block quote can contain some lazy and some non-lazy
continuation lines:

.
> bar
baz
> foo
.
<blockquote>
<p>bar
baz
foo</p>
</blockquote>
.

Laziness only applies to lines that are continuations of
paragraphs. Lines containing characters or indentation that indicate
block structure cannot be lazy.

.
> foo
---
.
<blockquote>
<p>foo</p>
</blockquote>
<hr />
.

.
> - foo
- bar
.
<blockquote>
<ul>
<li>foo</li>
</ul>
</blockquote>
<ul>
<li>bar</li>
</ul>
.

.
>     foo
    bar
.
<blockquote>
<pre><code>foo
</code></pre>
</blockquote>
<pre><code>bar
</code></pre>
.

.
> ```
foo
```
.
<blockquote>
<pre><code></code></pre>
</blockquote>
<p>foo</p>
<pre><code></code></pre>
.

A block quote can be empty:

.
>
.
<blockquote>
</blockquote>
.

.
>
>  
> 
.
<blockquote>
</blockquote>
.

A block quote can have initial or final blank lines:

.
>
> foo
>  
.
<blockquote>
<p>foo</p>
</blockquote>
.

A blank line always separates block quotes:

.
> foo

> bar
.
<blockquote>
<p>foo</p>
</blockquote>
<blockquote>
<p>bar</p>
</blockquote>
.

(Most current Markdown implementations, including John Gruber's
original `Markdown.pl`, will parse this example as a single block quote
with two paragraphs.  But it seems better to allow the author to decide
whether two block quotes or one are wanted.)

Consecutiveness means that if we put these block quotes together,
we get a single block quote:

.
> foo
> bar
.
<blockquote>
<p>foo
bar</p>
</blockquote>
.

To get a block quote with two paragraphs, use:

.
> foo
>
> bar
.
<blockquote>
<p>foo</p>
<p>bar</p>
</blockquote>
.

Block quotes can interrupt paragraphs:

.
foo
> bar
.
<p>foo</p>
<blockquote>
<p>bar</p>
</blockquote>
.

In general, blank lines are not needed before or after block
quotes:

.
> aaa
***
> bbb
.
<blockquote>
<p>aaa</p>
</blockquote>
<hr />
<blockquote>
<p>bbb</p>
</blockquote>
.

However, because of laziness, a blank line is needed between
a block quote and a following paragraph:

.
> bar
baz
.
<blockquote>
<p>bar
baz</p>
</blockquote>
.

.
> bar

baz
.
<blockquote>
<p>bar</p>
</blockquote>
<p>baz</p>
.

.
> bar
>
baz
.
<blockquote>
<p>bar</p>
</blockquote>
<p>baz</p>
.

It is a consequence of the Laziness rule that any number
of initial `>`s may be omitted on a continuation line of a
nested block quote:

.
> > > foo
bar
.
<blockquote>
<blockquote>
<blockquote>
<p>foo
bar</p>
</blockquote>
</blockquote>
</blockquote>
.

.
>>> foo
> bar
>>baz
.
<blockquote>
<blockquote>
<blockquote>
<p>foo
bar
baz</p>
</blockquote>
</blockquote>
</blockquote>
.

When including an indented code block in a block quote,
remember that the [block quote marker](#block-quote-marker) includes
both the `>` and a following space.  So *five spaces* are needed after
the `>`:

.
>     code

>    not code
.
<blockquote>
<pre><code>code
</code></pre>
</blockquote>
<blockquote>
<p>not code</p>
</blockquote>
.


## List items

A [list marker](#list-marker) <a id="list-marker"></a> is a
[bullet list marker](#bullet-list-marker) or an [ordered list
marker](#ordered-list-marker).

A [bullet list marker](#bullet-list-marker) <a id="bullet-list-marker"></a>
is a `-`, `+`, or `*` character.

An [ordered list marker](#ordered-list-marker) <a id="ordered-list-marker"></a>
is a sequence of one of more digits (`0-9`), followed by either a
`.` character or a `)` character.

The following rules define [list items](#list-item):

1.  **Basic case.**  If a sequence of lines *Ls* constitute a sequence of
    blocks *Bs* starting with a non-space character and not separated
    from each other by more than one blank line, and *M* is a list
    marker *M* of width *W* followed by 0 < *N* < 5 spaces, then the result
    of prepending *M* and the following spaces to the first line of
    *Ls*, and indenting subsequent lines of *Ls* by *W + N* spaces, is a
    list item with *Bs* as its contents.  The type of the list item
    (bullet or ordered) is determined by the type of its list marker.
    If the list item is ordered, then it is also assigned a start
    number, based on the ordered list marker.

For example, let *Ls* be the lines

.
A paragraph
with two lines.

    indented code

> A block quote.
.
<p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote>
.

And let *M* be the marker `1.`, and *N* = 2.  Then rule #1 says
that the following is an ordered list item with start number 1,
and the same contents as *Ls*:

.
1.  A paragraph
    with two lines.

        indented code

    > A block quote.
.
<ol>
<li><p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote></li>
</ol>
.

The most important thing to notice is that the position of
the text after the list marker determines how much indentation
is needed in subsequent blocks in the list item.  If the list
marker takes up two spaces, and there are three spaces between
the list marker and the next nonspace character, then blocks
must be indented five spaces in order to fall under the list
item.

Here are some examples showing how far content must be indented to be
put under the list item:

.
- one

 two
.
<ul>
<li>one</li>
</ul>
<p>two</p>
.

.
- one

  two
.
<ul>
<li><p>one</p>
<p>two</p></li>
</ul>
.

.
 -    one

     two
.
<ul>
<li>one</li>
</ul>
<pre><code> two
</code></pre>
.

.
 -    one

      two
.
<ul>
<li><p>one</p>
<p>two</p></li>
</ul>
.

It is tempting to think of this in terms of columns:  the continuation
blocks must be indented at least to the column of the first nonspace
character after the list marker.  However, that is not quite right.
The spaces after the list marker determine how much relative indentation
is needed.  Which column this indentation reaches will depend on
how the list item is embedded in other constructions, as shown by
this example:

.
   > > 1.  one
>>
>>     two
.
<blockquote>
<blockquote>
<ol>
<li><p>one</p>
<p>two</p></li>
</ol>
</blockquote>
</blockquote>
.

Here `two` occurs in the same column as the list marker `1.`,
but is actually contained in the list item, because there is
sufficent indentation after the last containing blockquote marker.

The converse is also possible.  In the following example, the word `two`
occurs far to the right of the initial text of the list item, `one`, but
it is not considered part of the list item, because it is not indented
far enough past the blockquote marker:

.
>>- one
>>
  >  > two
.
<blockquote>
<blockquote>
<ul>
<li>one</li>
</ul>
<p>two</p>
</blockquote>
</blockquote>
.

A list item may not contain blocks that are separated by more than
one blank line.  Thus, two blank lines will end a list, unless the
two blanks are contained in a [fenced code block](#fenced-code-block).

.
- foo

  bar

- foo


  bar

- ```
  foo


  bar
  ```
.
<ul>
<li><p>foo</p>
<p>bar</p></li>
<li><p>foo</p></li>
</ul>
<p>bar</p>
<ul>
<li><pre><code>foo


bar
</code></pre></li>
</ul>
.

A list item may contain any kind of block:

.
1.  foo

    ```
    bar
    ```

    baz

    > bam
.
<ol>
<li><p>foo</p>
<pre><code>bar
</code></pre>
<p>baz</p>
<blockquote>
<p>bam</p>
</blockquote></li>
</ol>
.

2.  **Item starting with indented code.**  If a sequence of lines *Ls*
    constitute a sequence of blocks *Bs* starting with an indented code
    block and not separated from each other by more than one blank line,
    and *M* is a list marker *M* of width *W* followed by
    one space, then the result of prepending *M* and the following
    space to the first line of *Ls*, and indenting subsequent lines of
    *Ls* by *W + 1* spaces, is a list item with *Bs* as its contents.
    If a line is empty, then it need not be indented.  The type of the
    list item (bullet or ordered) is determined by the type of its list
    marker.  If the list item is ordered, then it is also assigned a
    start number, based on the ordered list marker.

An indented code block will have to be indented four spaces beyond
the edge of the region where text will be included in the list item.
In the following case that is 6 spaces:

.
- foo

      bar
.
<ul>
<li><p>foo</p>
<pre><code>bar
</code></pre></li>
</ul>
.

And in this case it is 11 spaces:

.
  10.  foo

           bar
.
<ol start="10">
<li><p>foo</p>
<pre><code>bar
</code></pre></li>
</ol>
.

If the *first* block in the list item is an indented code block,
then by rule #2, the contents must be indented *one* space after the
list marker:

.
    indented code

paragraph

    more code
.
<pre><code>indented code
</code></pre>
<p>paragraph</p>
<pre><code>more code
</code></pre>
.

.
1.     indented code

   paragraph

       more code
.
<ol>
<li><pre><code>indented code
</code></pre>
<p>paragraph</p>
<pre><code>more code
</code></pre></li>
</ol>
.

Note that an additional space indent is interpreted as space
inside the code block:

.
1.      indented code

   paragraph

       more code
.
<ol>
<li><pre><code> indented code
</code></pre>
<p>paragraph</p>
<pre><code>more code
</code></pre></li>
</ol>
.

Note that rules #1 and #2 only apply to two cases:  (a) cases
in which the lines to be included in a list item begin with a nonspace
character, and (b) cases in which they begin with an indented code
block.  In a case like the following, where the first block begins with
a three-space indent, the rules do not allow us to form a list item by
indenting the whole thing and prepending a list marker:

.
   foo

bar
.
<p>foo</p>
<p>bar</p>
.

.
-    foo

  bar
.
<ul>
<li>foo</li>
</ul>
<p>bar</p>
.

This is not a significant restriction, because when a block begins
with 1-3 spaces indent, the indentation can always be removed without
a change in interpretation, allowing rule #1 to be applied.  So, in
the above case:

.
-  foo

   bar
.
<ul>
<li><p>foo</p>
<p>bar</p></li>
</ul>
.


3.  **Indentation.**  If a sequence of lines *Ls* constitutes a list item
    according to rule #1 or #2, then the result of indenting each line
    of *L* by 1-3 spaces (the same for each line) also constitutes a
    list item with the same contents and attributes.  If a line is
    empty, then it need not be indented.

Indented one space:

.
 1.  A paragraph
     with two lines.

         indented code

     > A block quote.
.
<ol>
<li><p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote></li>
</ol>
.

Indented two spaces:

.
  1.  A paragraph
      with two lines.

          indented code

      > A block quote.
.
<ol>
<li><p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote></li>
</ol>
.

Indented three spaces:

.
   1.  A paragraph
       with two lines.

           indented code

       > A block quote.
.
<ol>
<li><p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote></li>
</ol>
.

Four spaces indent gives a code block:

.
    1.  A paragraph
        with two lines.

            indented code

        > A block quote.
.
<pre><code>1.  A paragraph
    with two lines.

        indented code

    &gt; A block quote.
</code></pre>
.


4.  **Laziness.**  If a string of lines *Ls* constitute a [list
    item](#list-item) with contents *Bs*, then the result of deleting
    some or all of the indentation from one or more lines in which the
    next non-space character after the indentation is
    [paragraph continuation text](#paragraph-continuation-text) is a
    list item with the same contents and attributes.

Here is an example with lazy continuation lines:

.
  1.  A paragraph
with two lines.

          indented code

      > A block quote.
.
<ol>
<li><p>A paragraph
with two lines.</p>
<pre><code>indented code
</code></pre>
<blockquote>
<p>A block quote.</p>
</blockquote></li>
</ol>
.

Indentation can be partially deleted:

.
  1.  A paragraph
    with two lines.
.
<ol>
<li>A paragraph
with two lines.</li>
</ol>
.

These examples show how laziness can work in nested structures:

.
> 1. > Blockquote
continued here.
.
<blockquote>
<ol>
<li><blockquote>
<p>Blockquote
continued here.</p>
</blockquote></li>
</ol>
</blockquote>
.

.
> 1. > Blockquote
> continued here.
.
<blockquote>
<ol>
<li><blockquote>
<p>Blockquote
continued here.</p>
</blockquote></li>
</ol>
</blockquote>
.


5.  **That's all.** Nothing that is not counted as a list item by rules
    #1--4 counts as a [list item](#list-item).

The rules for sublists follow from the general rules above.  A sublist
must be indented the same number of spaces a paragraph would need to be
in order to be included in the list item.

So, in this case we need two spaces indent:

.
- foo
  - bar
    - baz
.
<ul>
<li>foo
<ul>
<li>bar
<ul>
<li>baz</li>
</ul></li>
</ul></li>
</ul>
.

One is not enough:

.
- foo
 - bar
  - baz
.
<ul>
<li>foo</li>
<li>bar</li>
<li>baz</li>
</ul>
.

Here we need four, because the list marker is wider:

.
10) foo
    - bar
.
<ol start="10">
<li>foo
<ul>
<li>bar</li>
</ul></li>
</ol>
.

Three is not enough:

.
10) foo
   - bar
.
<ol start="10">
<li>foo</li>
</ol>
<ul>
<li>bar</li>
</ul>
.

A list may be the first block in a list item:

.
- - foo
.
<ul>
<li><ul>
<li>foo</li>
</ul></li>
</ul>
.

.
1. - 2. foo
.
<ol>
<li><ul>
<li><ol start="2">
<li>foo</li>
</ol></li>
</ul></li>
</ol>
.

A list item may be empty:

.
- foo
-
- bar
.
<ul>
<li>foo</li>
<li></li>
<li>bar</li>
</ul>
.

.
-
.
<ul>
<li></li>
</ul>
.

### Motivation

John Gruber's Markdown spec says the following about list items:

1. "List markers typically start at the left margin, but may be indented
   by up to three spaces. List markers must be followed by one or more
   spaces or a tab."

2. "To make lists look nice, you can wrap items with hanging indents....
   But if you don't want to, you don't have to."

3. "List items may consist of multiple paragraphs. Each subsequent
   paragraph in a list item must be indented by either 4 spaces or one
   tab."

4. "It looks nice if you indent every line of the subsequent paragraphs,
   but here again, Markdown will allow you to be lazy."

5. "To put a blockquote within a list item, the blockquote's `>`
   delimiters need to be indented."

6. "To put a code block within a list item, the code block needs to be
   indented twice — 8 spaces or two tabs."

These rules specify that a paragraph under a list item must be indented
four spaces (presumably, from the left margin, rather than the start of
the list marker, but this is not said), and that code under a list item
must be indented eight spaces instead of the usual four.  They also say
that a block quote must be indented, but not by how much; however, the
example given has four spaces indentation.  Although nothing is said
about other kinds of block-level content, it is certainly reasonable to
infer that *all* block elements under a list item, including other
lists, must be indented four spaces.  This principle has been called the
*four-space rule*.

The four-space rule is clear and principled, and if the reference
implementation `Markdown.pl` had followed it, it probably would have
become the standard.  However, `Markdown.pl` allowed paragraphs and
sublists to start with only two spaces indentation, at least on the
outer level.  Worse, its behavior was inconsistent: a sublist of an
outer-level list needed two spaces indentation, but a sublist of this
sublist needed three spaces.  It is not surprising, then, that different
implementations of Markdown have developed very different rules for
determining what comes under a list item.  (Pandoc and python-Markdown,
for example, stuck with Gruber's syntax description and the four-space
rule, while discount, redcarpet, marked, PHP Markdown, and others
followed `Markdown.pl`'s behavior more closely.)

Unfortunately, given the divergences between implementations, there
is no way to give a spec for list items that will be guaranteed not
to break any existing documents.  However, the spec given here should
correctly handle lists formatted with either the four-space rule or
the more forgiving `Markdown.pl` behavior, provided they are laid out
in a way that is natural for a human to read.

The strategy here is to let the width and indentation of the list marker
determine the indentation necessary for blocks to fall under the list
item, rather than having a fixed and arbitrary number.  The writer can
think of the body of the list item as a unit which gets indented to the
right enough to fit the list marker (and any indentation on the list
marker).  (The laziness rule, #4, then allows continuation lines to be
unindented if needed.)

This rule is superior, we claim, to any rule requiring a fixed level of
indentation from the margin.  The four-space rule is clear but
unnatural. It is quite unintuitive that

``` markdown
- foo

  bar

  - baz
```

should be parsed as two lists with an intervening paragraph,

``` html
<ul>
<li>foo</li>
</ul>
<p>bar</p>
<ul>
<li>baz</li>
</ul>
```

as the four-space rule demands, rather than a single list,

``` html
<ul>
<li><p>foo</p>
<p>bar</p>
<ul>
<li>baz</li>
</ul></li>
</ul>
```

The choice of four spaces is arbitrary.  It can be learned, but it is
not likely to be guessed, and it trips up beginners regularly.

Would it help to adopt a two-space rule?  The problem is that such
a rule, together with the rule allowing 1--3 spaces indentation of the
initial list marker, allows text that is indented *less than* the
original list marker to be included in the list item. For example,
`Markdown.pl` parses

``` markdown
   - one

  two
```

as a single list item, with `two` a continuation paragraph:

``` html
<ul>
<li><p>one</p>
<p>two</p></li>
</ul>
```

and similarly

``` markdown
>   - one
>
>  two
```

as

``` html
<blockquote>
<ul>
<li><p>one</p>
<p>two</p></li>
</ul>
</blockquote>
```

This is extremely unintuitive.

Rather than requiring a fixed indent from the margin, we could require
a fixed indent (say, two spaces, or even one space) from the list marker (which
may itself be indented).  This proposal would remove the last anomaly
discussed.  Unlike the spec presented above, it would count the following
as a list item with a subparagraph, even though the paragraph `bar`
is not indented as far as the first paragraph `foo`:

``` markdown
 10. foo

   bar  
```

Arguably this text does read like a list item with `bar` as a subparagraph,
which may count in favor of the proposal.  However, on this proposal indented
code would have to be indented six spaces after the list marker.  And this
would break a lot of existing Markdown, which has the pattern:

``` markdown
1.  foo

        indented code
```

where the code is indented eight spaces.  The spec above, by contrast, will
parse this text as expected, since the code block's indentation is measured
from the beginning of `foo`.

The one case that needs special treatment is a list item that *starts*
with indented code.  How much indentation is required in that case, since
we don't have a "first paragraph" to measure from?  Rule #2 simply stipulates
that in such cases, we require one space indentation from the list marker
(and then the normal four spaces for the indented code).  This will match the
four-space rule in cases where the list marker plus its initial indentation
takes four spaces (a common case), but diverge in other cases.

## Lists

A [list](#list) <a id="list"></a> is a sequence of one or more
list items [of the same type](#of-the-same-type).  The list items
may be separated by single [blank lines](#blank-line), but two
blank lines end all containing lists.

Two list items are [of the same type](#of-the-same-type)
<a id="of-the-same-type"></a> if they begin with a [list
marker](#list-marker) of the same type.  Two list markers are of the
same type if (a) they are bullet list markers using the same character
(`-`, `+`, or `*`) or (b) they are ordered list numbers with the same
delimiter (either `.` or `)`).

A list is an [ordered list](#ordered-list) <a id="ordered-list"></a>
if its constituent list items begin with
[ordered list markers](#ordered-list-marker), and a [bullet
list](#bullet-list) <a id="bullet-list"></a> if its constituent list
items begin with [bullet list markers](#bullet-list-marker).

The [start number](#start-number) <a id="start-number"></a>
of an [ordered list](#ordered-list) is determined by the list number of
its initial list item.  The numbers of subsequent list items are
disregarded.

A list is [loose](#loose) if it any of its constituent list items are
separated by blank lines, or if any of its constituent list items
directly contain two block-level elements with a blank line between
them.  Otherwise a list is [tight](#tight).  (The difference in HTML output
is that paragraphs in a loose with are wrapped in `<p>` tags, while
paragraphs in a tight list are not.)

Changing the bullet or ordered list delimiter starts a new list:

.
- foo
- bar
+ baz
.
<ul>
<li>foo</li>
<li>bar</li>
</ul>
<ul>
<li>baz</li>
</ul>
.

.
1. foo
2. bar
3) baz
.
<ol>
<li>foo</li>
<li>bar</li>
</ol>
<ol start="3">
<li>baz</li>
</ol>
.

There can be blank lines between items, but two blank lines end
a list:

.
- foo

- bar


- baz
.
<ul>
<li><p>foo</p></li>
<li><p>bar</p></li>
</ul>
<ul>
<li>baz</li>
</ul>
.

As illustrated above in the section on [list items](#list-item),
two blank lines between blocks *within* a list item will also end a
list:

.
- foo


  bar
- baz
.
<ul>
<li>foo</li>
</ul>
<p>bar</p>
<ul>
<li>baz</li>
</ul>
.

Indeed, two blank lines will end *all* containing lists:

.
- foo
  - bar
    - baz


      bim
.
<ul>
<li>foo
<ul>
<li>bar
<ul>
<li>baz</li>
</ul></li>
</ul></li>
</ul>
<pre><code>  bim
</code></pre>
.

Thus, two blank lines can be used to separate consecutive lists of
the same type, or to separate a list from an indented code block
that would otherwise be parsed as a subparagraph of the final list
item:

.
- foo
- bar


- baz
- bim
.
<ul>
<li>foo</li>
<li>bar</li>
</ul>
<ul>
<li>baz</li>
<li>bim</li>
</ul>
.

.
-   foo

    notcode

-   foo


    code
.
<ul>
<li><p>foo</p>
<p>notcode</p></li>
<li><p>foo</p></li>
</ul>
<pre><code>code
</code></pre>
.

List items need not be indented to the same level.  The following
list items will be treated as items at the same list level,
since none is indented enough to belong to the previous list
item:

.
- a
 - b
  - c
   - d
  - e
 - f
- g
.
<ul>
<li>a</li>
<li>b</li>
<li>c</li>
<li>d</li>
<li>e</li>
<li>f</li>
<li>g</li>
</ul>
.

This is a loose list, because there is a blank line between
two of the list items:

.
- a
- b

- c
.
<ul>
<li><p>a</p></li>
<li><p>b</p></li>
<li><p>c</p></li>
</ul>
.

So is this, with a empty second item:

.
* a
*

* c
.
<ul>
<li><p>a</p></li>
<li></li>
<li><p>c</p></li>
</ul>
.

These are loose lists, even though there is no space between the items,
because one of the items directly contains two block-level elements
with a blank line between them:

.
- a
- b

  c
- d
.
<ul>
<li><p>a</p></li>
<li><p>b</p>
<p>c</p></li>
<li><p>d</p></li>
</ul>
.

.
- a
- b

  [ref]: /url
- d
.
<ul>
<li><p>a</p></li>
<li><p>b</p></li>
<li><p>d</p></li>
</ul>
.

This is a tight list, because the blank lines are in a code block:

.
- a
- ```
  b


  ```
- c
.
<ul>
<li>a</li>
<li><pre><code>b


</code></pre></li>
<li>c</li>
</ul>
.

This is a tight list, because the blank line is between two
paragraphs of a sublist.  So the inner list is loose while
the other list is tight:

.
- a
  - b

    c
- d
.
<ul>
<li>a
<ul>
<li><p>b</p>
<p>c</p></li>
</ul></li>
<li>d</li>
</ul>
.

This is a tight list, because the blank line is inside the
block quote:

.
* a
  > b
  >
* c
.
<ul>
<li>a
<blockquote>
<p>b</p>
</blockquote></li>
<li>c</li>
</ul>
.

This list is tight, because the consecutive block elements
are not separated by blank lines:

.
- a
  > b
  ```
  c
  ```
- d
.
<ul>
<li>a
<blockquote>
<p>b</p>
</blockquote>
<pre><code>c
</code></pre></li>
<li>d</li>
</ul>
.

A single-paragraph list is tight:

.
- a
.
<ul>
<li>a</li>
</ul>
.

.
- a
  - b
.
<ul>
<li>a
<ul>
<li>b</li>
</ul></li>
</ul>
.

Here the outer list is loose, the inner list tight:

.
* foo
  * bar

  baz
.
<ul>
<li><p>foo</p>
<ul>
<li>bar</li>
</ul>
<p>baz</p></li>
</ul>
.

.
- a
  - b
  - c

- d
  - e
  - f
.
<ul>
<li><p>a</p>
<ul>
<li>b</li>
<li>c</li>
</ul></li>
<li><p>d</p>
<ul>
<li>e</li>
<li>f</li>
</ul></li>
</ul>
.

# Inlines

Inlines are parsed sequentially from the beginning of the character
stream to the end (left to right, in left-to-right languages).
Thus, for example, in

.
`hi`lo`
.
<p><code>hi</code>lo`</p>
.

`hi` is parsed as code, leaving the backtick at the end as a literal
backtick.

## Backslash escapes

Any ASCII punctuation character may be backslash-escaped:

.
\!\"\#\$\%\&\'\(\)\*\+\,\-\.\/\:\;\<\=\>\?\@\[\\\]\^\_\`\{\|\}\~
.
<p>!&quot;#$%&amp;'()*+,-./:;&lt;=&gt;?@[\]^_`{|}~</p>
.

Backslashes before other characters are treated as literal
backslashes:

.
\→\A\a\ \3\φ\«
.
<p>\   \A\a\ \3\φ\«</p>
.

Escaped characters are treated as regular characters and do
not have their usual Markdown meanings:

.
\*not emphasized*
\<br/> not a tag
\[not a link](/foo)
\`not code`
1\. not a list
\* not a list
\# not a header
\[foo]: /url "not a reference"
.
<p>*not emphasized*
&lt;br/&gt; not a tag
[not a link](/foo)
`not code`
1. not a list
* not a list
# not a header
[foo]: /url &quot;not a reference&quot;</p>
.

If a backslash is itself escaped, the following character is not:

.
\\*emphasis*
.
<p>\<em>emphasis</em></p>
.

A backslash at the end of the line is a hard line break:

.
foo\
bar
.
<p>foo<br />
bar</p>
.

Backslash escapes do not work in code blocks, code spans, autolinks, or
raw HTML:

.
`` \[\` ``
.
<p><code>\[\`</code></p>
.

.
    \[\]
.
<pre><code>\[\]
</code></pre>
.

.
~~~
\[\]
~~~
.
<pre><code>\[\]
</code></pre>
.

.
<http://google.com?find=\*>
.
<p><a href="http://google.com?find=%5C*">http://google.com?find=\*</a></p>
.

.
<a href="/bar\/)">
.
<p><a href="/bar\/)"></p>
.

But they work in all other contexts, including URLs and link titles,
link references, and info strings in [fenced code
blocks](#fenced-code-block):

.
[foo](/bar\* "ti\*tle")
.
<p><a href="/bar*" title="ti*tle">foo</a></p>
.

.
[foo]

[foo]: /bar\* "ti\*tle"
.
<p><a href="/bar*" title="ti*tle">foo</a></p>
.

.
``` foo\+bar
foo
```
.
<pre><code class="language-foo+bar">foo
</code></pre>
.


## Entities

With the goal of making this standard as HTML-agnostic as possible, all HTML valid HTML Entities in any
context are recognized as such and converted into their actual values (i.e. the UTF8 characters representing
the entity itself) before they are stored in the AST.

This allows implementations that target HTML output to trivially escape the entities when generating HTML,
and simplifies the job of implementations targetting other languages, as these will only need to handle the
UTF8 chars and need not be HTML-entity aware.

[Named entities](#name-entities) <a id="named-entities"></a> consist of `&`
+ any of the valid HTML5 entity names + `;`. The [following document](http://www.whatwg.org/specs/web-apps/current-work/multipage/entities.json)
is used as an authoritative source of the valid entity names and their corresponding codepoints.

Conforming implementations that target Markdown don't need to generate entities for all the valid
named entities that exist, with the exception of `"` (`&quot;`), `&` (`&amp;`), `<` (`&lt;`) and `>` (`&gt;`),
which always need to be written as entities for security reasons.

.
&nbsp; &amp; &copy; &AElig; &Dcaron; &frac34; &HilbertSpace; &DifferentialD; &ClockwiseContourIntegral;
.
<p>  &amp; © Æ Ď ¾ ℋ ⅆ ∲</p>
.

[Decimal entities](#decimal-entities) <a id="decimal-entities"></a>
consist of `&#` + a string of 1--8 arabic digits + `;`. Again, these entities need to be recognised
and tranformed into their corresponding UTF8 codepoints. Invalid Unicode codepoints will be written
as the "unknown codepoint" character (`0xFFFD`)

.
&#35; &#1234; &#992; &#98765432;
.
<p># Ӓ Ϡ �</p>
.

[Hexadecimal entities](#hexadecimal-entities) <a id="hexadecimal-entities"></a>
consist of `&#` + either `X` or `x` + a string of 1-8 hexadecimal digits
+ `;`. They will also be parsed and turned into their corresponding UTF8 values in the AST.

.
&#X22; &#XD06; &#xcab;
.
<p>&quot; ആ ಫ</p>
.

Here are some nonentities:

.
&nbsp &x; &#; &#x; &ThisIsWayTooLongToBeAnEntityIsntIt; &hi?;
.
<p>&amp;nbsp &amp;x; &amp;#; &amp;#x; &amp;ThisIsWayTooLongToBeAnEntityIsntIt; &amp;hi?;</p>
.

Although HTML5 does accept some entities without a trailing semicolon
(such as `&copy`), these are not recognized as entities here, because it makes the grammar too ambiguous:

.
&copy
.
<p>&amp;copy</p>
.

Strings that are not on the list of HTML5 named entities are not recognized as entities either:

.
&MadeUpEntity;
.
<p>&amp;MadeUpEntity;</p>
.

Entities are recognized in any context besides code spans or
code blocks, including raw HTML, URLs, [link titles](#link-title), and
[fenced code block](#fenced-code-block) info strings:

.
<a href="&ouml;&ouml;.html">
.
<p><a href="&ouml;&ouml;.html"></p>
.

.
[foo](/f&ouml;&ouml; "f&ouml;&ouml;")
.
<p><a href="/f%C3%B6%C3%B6" title="föö">foo</a></p>
.

.
[foo]

[foo]: /f&ouml;&ouml; "f&ouml;&ouml;"
.
<p><a href="/f%C3%B6%C3%B6" title="föö">foo</a></p>
.

.
``` f&ouml;&ouml;
foo
```
.
<pre><code class="language-föö">foo
</code></pre>
.

Entities are treated as literal text in code spans and code blocks:

.
`f&ouml;&ouml;`
.
<p><code>f&amp;ouml;&amp;ouml;</code></p>
.

.
    f&ouml;f&ouml;
.
<pre><code>f&amp;ouml;f&amp;ouml;
</code></pre>
.

## Code span

A [backtick string](#backtick-string) <a id="backtick-string"></a>
is a string of one or more backtick characters (`` ` ``) that is neither
preceded nor followed by a backtick.

A code span begins with a backtick string and ends with a backtick
string of equal length.  The contents of the code span are the
characters between the two backtick strings, with leading and trailing
spaces and newlines removed, and consecutive spaces and newlines
collapsed to single spaces.

This is a simple code span:

.
`foo`
.
<p><code>foo</code></p>
.

Here two backticks are used, because the code contains a backtick.
This example also illustrates stripping of leading and trailing spaces:

.
`` foo ` bar  ``
.
<p><code>foo ` bar</code></p>
.

This example shows the motivation for stripping leading and trailing
spaces:

.
` `` `
.
<p><code>``</code></p>
.

Newlines are treated like spaces:

.
``
foo
``
.
<p><code>foo</code></p>
.

Interior spaces and newlines are collapsed into single spaces, just
as they would be by a browser:

.
`foo   bar
  baz`
.
<p><code>foo bar baz</code></p>
.

Q: Why not just leave the spaces, since browsers will collapse them
anyway?  A:  Because we might be targeting a non-HTML format, and we
shouldn't rely on HTML-specific rendering assumptions.

(Existing implementations differ in their treatment of internal
spaces and newlines.  Some, including `Markdown.pl` and
`showdown`, convert an internal newline into a `<br />` tag.
But this makes things difficult for those who like to hard-wrap
their paragraphs, since a line break in the midst of a code
span will cause an unintended line break in the output.  Others
just leave internal spaces as they are, which is fine if only
HTML is being targeted.)

.
`foo `` bar`
.
<p><code>foo `` bar</code></p>
.

Note that backslash escapes do not work in code spans. All backslashes
are treated literally:

.
`foo\`bar`
.
<p><code>foo\</code>bar`</p>
.

Backslash escapes are never needed, because one can always choose a
string of *n* backtick characters as delimiters, where the code does
not contain any strings of exactly *n* backtick characters.

Code span backticks have higher precedence than any other inline
constructs except HTML tags and autolinks.  Thus, for example, this is
not parsed as emphasized text, since the second `*` is part of a code
span:

.
*foo`*`
.
<p>*foo<code>*</code></p>
.

And this is not parsed as a link:

.
[not a `link](/foo`)
.
<p>[not a <code>link](/foo</code>)</p>
.

But this is a link:

.
<http://foo.bar.`baz>`
.
<p><a href="http://foo.bar.%60baz">http://foo.bar.`baz</a>`</p>
.

And this is an HTML tag:

.
<a href="`">`
.
<p><a href="`">`</p>
.

When a backtick string is not closed by a matching backtick string,
we just have literal backticks:

.
```foo``
.
<p>```foo``</p>
.

.
`foo
.
<p>`foo</p>
.

## Emphasis and strong emphasis

John Gruber's original [Markdown syntax
description](http://daringfireball.net/projects/markdown/syntax#em) says:

> Markdown treats asterisks (`*`) and underscores (`_`) as indicators of
> emphasis. Text wrapped with one `*` or `_` will be wrapped with an HTML
> `<em>` tag; double `*`'s or `_`'s will be wrapped with an HTML `<strong>`
> tag.

This is enough for most users, but these rules leave much undecided,
especially when it comes to nested emphasis.  The original
`Markdown.pl` test suite makes it clear that triple `***` and
`___` delimiters can be used for strong emphasis, and most
implementations have also allowed the following patterns:

``` markdown
***strong emph***
***strong** in emph*
***emph* in strong**
**in strong *emph***
*in emph **strong***
```

The following patterns are less widely supported, but the intent
is clear and they are useful (especially in contexts like bibliography
entries):

``` markdown
*emph *with emph* in it*
**strong **with strong** in it**
```

Many implementations have also restricted intraword emphasis to
the `*` forms, to avoid unwanted emphasis in words containing
internal underscores.  (It is best practice to put these in code
spans, but users often do not.)

``` markdown
internal emphasis: foo*bar*baz
no emphasis: foo_bar_baz
```

The following rules capture all of these patterns, while allowing
for efficient parsing strategies that do not backtrack:

1.  A single `*` character [can open emphasis](#can-open-emphasis)
    <a id="can-open-emphasis"></a> iff

    (a) it is not part of a sequence of four or more unescaped `*`s,
    (b) it is not followed by whitespace, and
    (c) either it is not followed by a `*` character or it is
        followed immediately by strong emphasis.

2.  A single `_` character [can open emphasis](#can-open-emphasis) iff

    (a) it is not part of a sequence of four or more unescaped `_`s,
    (b) it is not followed by whitespace,
    (c) it is not preceded by an ASCII alphanumeric character, and
    (d) either it is not followed by a `_` character or it is
        followed immediately by strong emphasis.

3.  A single `*` character [can close emphasis](#can-close-emphasis)
    <a id="can-close-emphasis"></a> iff

    (a) it is not part of a sequence of four or more unescaped `*`s, and
    (b) it is not preceded by whitespace.

4.  A single `_` character [can close emphasis](#can-close-emphasis) iff

    (a) it is not part of a sequence of four or more unescaped `_`s,
    (b) it is not preceded by whitespace, and
    (c) it is not followed by an ASCII alphanumeric character.

5.  A double `**` [can open strong emphasis](#can-open-strong-emphasis)
    <a id="can-open-strong-emphasis" ></a> iff

    (a) it is not part of a sequence of four or more unescaped `*`s,
    (b) it is not followed by whitespace, and
    (c) either it is not followed by a `*` character or it is
        followed immediately by emphasis.

6.  A double `__` [can open strong emphasis](#can-open-strong-emphasis)
    iff

    (a) it is not part of a sequence of four or more unescaped `_`s,
    (b) it is not followed by whitespace, and
    (c) it is not preceded by an ASCII alphanumeric character, and
    (d) either it is not followed by a `_` character or it is
        followed immediately by emphasis.

7.  A double `**` [can close strong emphasis](#can-close-strong-emphasis)
    <a id="can-close-strong-emphasis" ></a> iff

    (a) it is not part of a sequence of four or more unescaped `*`s, and
    (b) it is not preceded by whitespace.

8.  A double `__` [can close strong emphasis](#can-close-strong-emphasis)
    iff

    (a) it is not part of a sequence of four or more unescaped `_`s,
    (b) it is not preceded by whitespace, and
    (c) it is not followed by an ASCII alphanumeric character.

9.  Emphasis begins with a delimiter that [can open
    emphasis](#can-open-emphasis) and includes inlines parsed
    sequentially until a delimiter that [can close
    emphasis](#can-close-emphasis), and that uses the same
    character (`_` or `*`) as the opening delimiter, is reached.

10. Strong emphasis begins with a delimiter that [can open strong
    emphasis](#can-open-strong-emphasis) and includes inlines parsed
    sequentially until a delimiter that [can close strong
    emphasis](#can-close-strong-emphasis), and that uses the
    same character (`_` or `*`) as the opening delimiter, is reached.

These rules can be illustrated through a series of examples.

Simple emphasis:

.
*foo bar*
.
<p><em>foo bar</em></p>
.

.
_foo bar_
.
<p><em>foo bar</em></p>
.

Simple strong emphasis:

.
**foo bar**
.
<p><strong>foo bar</strong></p>
.

.
__foo bar__
.
<p><strong>foo bar</strong></p>
.

Emphasis can continue over line breaks:

.
*foo
bar*
.
<p><em>foo
bar</em></p>
.

.
_foo
bar_
.
<p><em>foo
bar</em></p>
.

.
**foo
bar**
.
<p><strong>foo
bar</strong></p>
.

.
__foo
bar__
.
<p><strong>foo
bar</strong></p>
.

Emphasis can contain other inline constructs:

.
*foo [bar](/url)*
.
<p><em>foo <a href="/url">bar</a></em></p>
.

.
_foo [bar](/url)_
.
<p><em>foo <a href="/url">bar</a></em></p>
.

.
**foo [bar](/url)**
.
<p><strong>foo <a href="/url">bar</a></strong></p>
.

.
__foo [bar](/url)__
.
<p><strong>foo <a href="/url">bar</a></strong></p>
.

Symbols contained in other inline constructs will not
close emphasis:

.
*foo [bar*](/url)
.
<p>*foo <a href="/url">bar*</a></p>
.

.
_foo [bar_](/url)
.
<p>_foo <a href="/url">bar_</a></p>
.

.
**<a href="**">
.
<p>**<a href="**"></p>
.

.
__<a href="__">
.
<p>__<a href="__"></p>
.

.
*a `*`*
.
<p><em>a <code>*</code></em></p>
.

.
_a `_`_
.
<p><em>a <code>_</code></em></p>
.

.
**a<http://foo.bar?q=**>
.
<p>**a<a href="http://foo.bar?q=**">http://foo.bar?q=**</a></p>
.

.
__a<http://foo.bar?q=__>
.
<p>__a<a href="http://foo.bar?q=__">http://foo.bar?q=__</a></p>
.

This is not emphasis, because the opening delimiter is
followed by white space:

.
and * foo bar*
.
<p>and * foo bar*</p>
.

.
_ foo bar_
.
<p>_ foo bar_</p>
.

.
and ** foo bar**
.
<p>and ** foo bar**</p>
.

.
__ foo bar__
.
<p>__ foo bar__</p>
.

This is not emphasis, because the closing delimiter is
preceded by white space:

.
and *foo bar *
.
<p>and *foo bar *</p>
.

.
and _foo bar _
.
<p>and _foo bar _</p>
.

.
and **foo bar **
.
<p>and **foo bar **</p>
.

.
and __foo bar __
.
<p>and __foo bar __</p>
.

The rules imply that a sequence of four or more unescaped `*` or
`_` characters will always be parsed as a literal string:

.
****hi****
.
<p>****hi****</p>
.

.
_____hi_____
.
<p>_____hi_____</p>
.

.
Sign here: _________
.
<p>Sign here: _________</p>
.

The rules also imply that there can be no empty emphasis or strong
emphasis:

.
** is not an empty emphasis
.
<p>** is not an empty emphasis</p>
.

.
**** is not an empty strong emphasis
.
<p>**** is not an empty strong emphasis</p>
.

To include `*` or `_` in emphasized sections, use backslash escapes
or code spans:

.
*here is a \**
.
<p><em>here is a *</em></p>
.

.
__this is a double underscore (`__`)__
.
<p><strong>this is a double underscore (<code>__</code>)</strong></p>
.

`*` delimiters allow intra-word emphasis; `_` delimiters do not:

.
foo*bar*baz
.
<p>foo<em>bar</em>baz</p>
.

.
foo_bar_baz
.
<p>foo_bar_baz</p>
.

.
foo__bar__baz
.
<p>foo__bar__baz</p>
.

.
_foo_bar_baz_
.
<p><em>foo_bar_baz</em></p>
.

.
11*15*32
.
<p>11<em>15</em>32</p>
.

.
11_15_32
.
<p>11_15_32</p>
.

Internal underscores will be ignored in underscore-delimited
emphasis:

.
_foo_bar_baz_
.
<p><em>foo_bar_baz</em></p>
.

.
__foo__bar__baz__
.
<p><strong>foo__bar__baz</strong></p>
.

The rules are sufficient for the following nesting patterns:

.
***foo bar***
.
<p><strong><em>foo bar</em></strong></p>
.

.
___foo bar___
.
<p><strong><em>foo bar</em></strong></p>
.

.
***foo** bar*
.
<p><em><strong>foo</strong> bar</em></p>
.

.
___foo__ bar_
.
<p><em><strong>foo</strong> bar</em></p>
.

.
***foo* bar**
.
<p><strong><em>foo</em> bar</strong></p>
.

.
___foo_ bar__
.
<p><strong><em>foo</em> bar</strong></p>
.

.
*foo **bar***
.
<p><em>foo <strong>bar</strong></em></p>
.

.
_foo __bar___
.
<p><em>foo <strong>bar</strong></em></p>
.

.
**foo *bar***
.
<p><strong>foo <em>bar</em></strong></p>
.

.
__foo _bar___
.
<p><strong>foo <em>bar</em></strong></p>
.

.
*foo **bar***
.
<p><em>foo <strong>bar</strong></em></p>
.

.
_foo __bar___
.
<p><em>foo <strong>bar</strong></em></p>
.

.
*foo *bar* baz*
.
<p><em>foo <em>bar</em> baz</em></p>
.

.
_foo _bar_ baz_
.
<p><em>foo <em>bar</em> baz</em></p>
.

.
**foo **bar** baz**
.
<p><strong>foo <strong>bar</strong> baz</strong></p>
.

.
__foo __bar__ baz__
.
<p><strong>foo <strong>bar</strong> baz</strong></p>
.

.
*foo **bar** baz*
.
<p><em>foo <strong>bar</strong> baz</em></p>
.

.
_foo __bar__ baz_
.
<p><em>foo <strong>bar</strong> baz</em></p>
.

.
**foo *bar* baz**
.
<p><strong>foo <em>bar</em> baz</strong></p>
.

.
__foo _bar_ baz__
.
<p><strong>foo <em>bar</em> baz</strong></p>
.

Note that you cannot nest emphasis directly inside emphasis
using the same delimeter, or strong emphasis directly inside
strong emphasis:

.
**foo**
.
<p><strong>foo</strong></p>
.

.
****foo****
.
<p>****foo****</p>
.

For these nestings, you need to switch delimiters:

.
*_foo_*
.
<p><em><em>foo</em></em></p>
.

.
**__foo__**
.
<p><strong><strong>foo</strong></strong></p>
.

Note that a `*` followed by a `*` can close emphasis, and
a `**` followed by a `*` can close strong emphasis (and
similarly for `_` and `__`):

.
*foo**
.
<p><em>foo</em>*</p>
.

.
*foo *bar**
.
<p><em>foo <em>bar</em></em></p>
.

.
**foo***
.
<p><strong>foo</strong>*</p>
.

.
***foo* bar***
.
<p><strong><em>foo</em> bar</strong>*</p>
.

.
***foo** bar***
.
<p><em><strong>foo</strong> bar</em>**</p>
.

The following contains no strong emphasis, because the opening
delimiter is closed by the first `*` before `bar`:

.
*foo**bar***
.
<p><em>foo</em><em>bar</em>**</p>
.

However, a string of four or more `****` can never close emphasis:

.
*foo****
.
<p>*foo****</p>
.

Note that there are some asymmetries here:

.
*foo**

**foo*
.
<p><em>foo</em>*</p>
<p>**foo*</p>
.

.
*foo *bar**

**foo* bar*
.
<p><em>foo <em>bar</em></em></p>
<p>**foo* bar*</p>
.

More cases with mismatched delimiters:

.
**foo* bar*
.
<p>**foo* bar*</p>
.

.
*bar***
.
<p><em>bar</em>**</p>
.

.
***foo*
.
<p>***foo*</p>
.

.
**bar***
.
<p><strong>bar</strong>*</p>
.

.
***foo**
.
<p>***foo**</p>
.

.
***foo *bar*
.
<p>***foo <em>bar</em></p>
.

## Links

A link contains a [link label](#link-label) (the visible text),
a [destination](#destination) (the URI that is the link destination),
and optionally a [link title](#link-title).  There are two basic kinds
of links in Markdown.  In [inline links](#inline-links) the destination
and title are given immediately after the label.  In [reference
links](#reference-links) the destination and title are defined elsewhere
in the document.

A [link label](#link-label) <a id="link-label"></a>  consists of

- an opening `[`, followed by
- zero or more backtick code spans, autolinks, HTML tags, link labels,
  backslash-escaped ASCII punctuation characters, or non-`]` characters,
  followed by
- a closing `]`.

These rules are motivated by the following intuitive ideas:

- A link label is a container for inline elements.
- The square brackets bind more tightly than emphasis markers,
  but less tightly than `<>` or `` ` ``.
- Link labels may contain material in matching square brackets.

A [link destination](#link-destination) <a id="link-destination"></a>
consists of either

- a sequence of zero or more characters between an opening `<` and a
  closing `>` that contains no line breaks or unescaped `<` or `>`
  characters, or

- a nonempty sequence of characters that does not include
  ASCII space or control characters, and includes parentheses
  only if (a) they are backslash-escaped or (b) they are part of
  a balanced pair of unescaped parentheses that is not itself
  inside a balanced pair of unescaped paretheses.

A [link title](#link-title) <a id="link-title"></a>  consists of either

- a sequence of zero or more characters between straight double-quote
  characters (`"`), including a `"` character only if it is
  backslash-escaped, or

- a sequence of zero or more characters between straight single-quote
  characters (`'`), including a `'` character only if it is
  backslash-escaped, or

- a sequence of zero or more characters between matching parentheses
  (`(...)`), including a `)` character only if it is backslash-escaped.

An [inline link](#inline-link) <a id="inline-link"></a>
consists of a [link label](#link-label) followed immediately
by a left parenthesis `(`, optional whitespace,
an optional [link destination](#link-destination),
an optional [link title](#link-title) separated from the link
destination by whitespace, optional whitespace, and a right
parenthesis `)`.  The link's text consists of the label (excluding
the enclosing square brackets) parsed as inlines.  The link's
URI consists of the link destination, excluding enclosing `<...>` if
present, with backslash-escapes in effect as described above.  The
link's title consists of the link title, excluding its enclosing
delimiters, with backslash-escapes in effect as described above.

Here is a simple inline link:

.
[link](/uri "title")
.
<p><a href="/uri" title="title">link</a></p>
.

The title may be omitted:

.
[link](/uri)
.
<p><a href="/uri">link</a></p>
.

Both the title and the destination may be omitted:

.
[link]()
.
<p><a href="">link</a></p>
.

.
[link](<>)
.
<p><a href="">link</a></p>
.


If the destination contains spaces, it must be enclosed in pointy
braces:

.
[link](/my uri)
.
<p>[link](/my uri)</p>
.

.
[link](</my uri>)
.
<p><a href="/my%20uri">link</a></p>
.

The destination cannot contain line breaks, even with pointy braces:

.
[link](foo
bar)
.
<p>[link](foo
bar)</p>
.

One level of balanced parentheses is allowed without escaping:

.
[link]((foo)and(bar))
.
<p><a href="(foo)and(bar)">link</a></p>
.

However, if you have parentheses within parentheses, you need to escape
or use the `<...>` form:

.
[link](foo(and(bar)))
.
<p>[link](foo(and(bar)))</p>
.

.
[link](foo(and\(bar\)))
.
<p><a href="foo(and(bar))">link</a></p>
.

.
[link](<foo(and(bar))>)
.
<p><a href="foo(and(bar))">link</a></p>
.

Parentheses and other symbols can also be escaped, as usual
in Markdown:

.
[link](foo\)\:)
.
<p><a href="foo):">link</a></p>
.

URL-escaping and should be left alone inside the destination, as all URL-escaped characters
are also valid URL characters. HTML entities in the destination will be parsed into their UTF8
codepoints, as usual, and optionally URL-escaped when written as HTML.

.
[link](foo%20b&auml;)
.
<p><a href="foo%20b%C3%A4">link</a></p>
.

Note that, because titles can often be parsed as destinations,
if you try to omit the destination and keep the title, you'll
get unexpected results:

.
[link]("title")
.
<p><a href="%22title%22">link</a></p>
.

Titles may be in single quotes, double quotes, or parentheses:

.
[link](/url "title")
[link](/url 'title')
[link](/url (title))
.
<p><a href="/url" title="title">link</a>
<a href="/url" title="title">link</a>
<a href="/url" title="title">link</a></p>
.

Backslash escapes and entities may be used in titles:

.
[link](/url "title \"&quot;")
.
<p><a href="/url" title="title &quot;&quot;">link</a></p>
.

Nested balanced quotes are not allowed without escaping:

.
[link](/url "title "and" title")
.
<p>[link](/url &quot;title &quot;and&quot; title&quot;)</p>
.

But it is easy to work around this by using a different quote type:

.
[link](/url 'title "and" title')
.
<p><a href="/url" title="title &quot;and&quot; title">link</a></p>
.

(Note:  `Markdown.pl` did allow double quotes inside a double-quoted
title, and its test suite included a test demonstrating this.
But it is hard to see a good rationale for the extra complexity this
brings, since there are already many ways---backslash escaping,
entities, or using a different quote type for the enclosing title---to
write titles containing double quotes.  `Markdown.pl`'s handling of
titles has a number of other strange features.  For example, it allows
single-quoted titles in inline links, but not reference links.  And, in
reference links but not inline links, it allows a title to begin with
`"` and end with `)`.  `Markdown.pl` 1.0.1 even allows titles with no closing
quotation mark, though 1.0.2b8 does not.  It seems preferable to adopt
a simple, rational rule that works the same way in inline links and
link reference definitions.)

Whitespace is allowed around the destination and title:

.
[link](   /uri
  "title"  )
.
<p><a href="/uri" title="title">link</a></p>
.

But it is not allowed between the link label and the
following parenthesis:

.
[link] (/uri)
.
<p>[link] (/uri)</p>
.

Note that this is not a link, because the closing `]` occurs in
an HTML tag:

.
[foo <bar attr="](baz)">
.
<p>[foo <bar attr="](baz)"></p>
.


There are three kinds of [reference links](#reference-link):
<a id="reference-link"></a>

A [full reference link](#full-reference-link) <a id="full-reference-link"></a>
consists of a [link label](#link-label), optional whitespace, and
another [link label](#link-label) that [matches](#matches) a
[link reference definition](#link-reference-definition) elsewhere in the
document.

One label [matches](#matches) <a id="matches"></a>
another just in case their normalized forms are equal.  To normalize a
label, perform the *unicode case fold* and collapse consecutive internal
whitespace to a single space.  If there are multiple matching reference
link definitions, the one that comes first in the document is used.  (It
is desirable in such cases to emit a warning.)

The contents of the first link label are parsed as inlines, which are
used as the link's text.  The link's URI and title are provided by the
matching [link reference definition](#link-reference-definition).

Here is a simple example:

.
[foo][bar]

[bar]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

The first label can contain inline content:

.
[*foo\!*][bar]

[bar]: /url "title"
.
<p><a href="/url" title="title"><em>foo!</em></a></p>
.

Matching is case-insensitive:

.
[foo][BaR]

[bar]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

Unicode case fold is used:

.
[Толпой][Толпой] is a Russian word.

[ТОЛПОЙ]: /url
.
<p><a href="/url">Толпой</a> is a Russian word.</p>
.

Consecutive internal whitespace is treated as one space for
purposes of determining matching:

.
[Foo
  bar]: /url

[Baz][Foo bar]
.
<p><a href="/url">Baz</a></p>
.

There can be whitespace between the two labels:

.
[foo] [bar]

[bar]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

.
[foo]
[bar]

[bar]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

When there are multiple matching [link reference
definitions](#link-reference-definition), the first is used:

.
[foo]: /url1

[foo]: /url2

[bar][foo]
.
<p><a href="/url1">bar</a></p>
.

Note that matching is performed on normalized strings, not parsed
inline content.  So the following does not match, even though the
labels define equivalent inline content:

.
[bar][foo\!]

[foo!]: /url
.
<p>[bar][foo!]</p>
.

A [collapsed reference link](#collapsed-reference-link)
<a id="collapsed-reference-link"></a> consists of a [link
label](#link-label) that [matches](#matches) a [link reference
definition](#link-reference-definition) elsewhere in the
document, optional whitespace, and the string `[]`.  The contents of the
first link label are parsed as inlines, which are used as the link's
text.  The link's URI and title are provided by the matching reference
link definition.  Thus, `[foo][]` is equivalent to `[foo][foo]`.

.
[foo][]

[foo]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

.
[*foo* bar][]

[*foo* bar]: /url "title"
.
<p><a href="/url" title="title"><em>foo</em> bar</a></p>
.

The link labels are case-insensitive:

.
[Foo][]

[foo]: /url "title"
.
<p><a href="/url" title="title">Foo</a></p>
.


As with full reference links, whitespace is allowed
between the two sets of brackets:

.
[foo] 
[]

[foo]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

A [shortcut reference link](#shortcut-reference-link)
<a id="shortcut-reference-link"></a> consists of a [link
label](#link-label) that [matches](#matches) a [link reference
definition](#link-reference-definition)  elsewhere in the
document and is not followed by `[]` or a link label.
The contents of the first link label are parsed as inlines,
which are used as the link's text.  the link's URI and title
are provided by the matching link reference definition.
Thus, `[foo]` is equivalent to `[foo][]`.

.
[foo]

[foo]: /url "title"
.
<p><a href="/url" title="title">foo</a></p>
.

.
[*foo* bar]

[*foo* bar]: /url "title"
.
<p><a href="/url" title="title"><em>foo</em> bar</a></p>
.

.
[[*foo* bar]]

[*foo* bar]: /url "title"
.
<p>[<a href="/url" title="title"><em>foo</em> bar</a>]</p>
.

The link labels are case-insensitive:

.
[Foo]

[foo]: /url "title"
.
<p><a href="/url" title="title">Foo</a></p>
.

If you just want bracketed text, you can backslash-escape the
opening bracket to avoid links:

.
\[foo]

[foo]: /url "title"
.
<p>[foo]</p>
.

Note that this is a link, because link labels bind more tightly
than emphasis:

.
[foo*]: /url

*[foo*]
.
<p>*<a href="/url">foo*</a></p>
.

However, this is not, because link labels bind less
tightly than code backticks:

.
[foo`]: /url

[foo`]`
.
<p>[foo<code>]</code></p>
.

Link labels can contain matched square brackets:

.
[[[foo]]]

[[[foo]]]: /url
.
<p><a href="/url">[[foo]]</a></p>
.

.
[[[foo]]]

[[[foo]]]: /url1
[foo]: /url2
.
<p><a href="/url1">[[foo]]</a></p>
.

For non-matching brackets, use backslash escapes:

.
[\[foo]

[\[foo]: /url
.
<p><a href="/url">[foo</a></p>
.

Full references take precedence over shortcut references:

.
[foo][bar]

[foo]: /url1
[bar]: /url2
.
<p><a href="/url2">foo</a></p>
.

In the following case `[bar][baz]` is parsed as a reference,
`[foo]` as normal text:

.
[foo][bar][baz]

[baz]: /url
.
<p>[foo]<a href="/url">bar</a></p>
.

Here, though, `[foo][bar]` is parsed as a reference, since
`[bar]` is defined:

.
[foo][bar][baz]

[baz]: /url1
[bar]: /url2
.
<p><a href="/url2">foo</a><a href="/url1">baz</a></p>
.

Here `[foo]` is not parsed as a shortcut reference, because it
is followed by a link label (even though `[bar]` is not defined):

.
[foo][bar][baz]

[baz]: /url1
[foo]: /url2
.
<p>[foo]<a href="/url1">bar</a></p>
.


## Images

An (unescaped) exclamation mark (`!`) followed by a reference or
inline link will be parsed as an image.  The link label will be
used as the image's alt text, and the link title, if any, will
be used as the image's title.

.
![foo](/url "title")
.
<p><img src="/url" alt="foo" title="title" /></p>
.

.
![foo *bar*]

[foo *bar*]: train.jpg "train & tracks"
.
<p><img src="train.jpg" alt="foo &lt;em&gt;bar&lt;/em&gt;" title="train &amp; tracks" /></p>
.

.
![foo *bar*][]

[foo *bar*]: train.jpg "train & tracks"
.
<p><img src="train.jpg" alt="foo &lt;em&gt;bar&lt;/em&gt;" title="train &amp; tracks" /></p>
.

.
![foo *bar*][foobar]

[FOOBAR]: train.jpg "train & tracks"
.
<p><img src="train.jpg" alt="foo &lt;em&gt;bar&lt;/em&gt;" title="train &amp; tracks" /></p>
.

.
![foo](train.jpg)
.
<p><img src="train.jpg" alt="foo" /></p>
.

.
My ![foo bar](/path/to/train.jpg  "title"   )
.
<p>My <img src="/path/to/train.jpg" alt="foo bar" title="title" /></p>
.

.
![foo](<url>)
.
<p><img src="url" alt="foo" /></p>
.

.
![](/url)
.
<p><img src="/url" alt="" /></p>
.

Reference-style:

.
![foo] [bar]

[bar]: /url
.
<p><img src="/url" alt="foo" /></p>
.

.
![foo] [bar]

[BAR]: /url
.
<p><img src="/url" alt="foo" /></p>
.

Collapsed:

.
![foo][]

[foo]: /url "title"
.
<p><img src="/url" alt="foo" title="title" /></p>
.

.
![*foo* bar][]

[*foo* bar]: /url "title"
.
<p><img src="/url" alt="&lt;em&gt;foo&lt;/em&gt; bar" title="title" /></p>
.

The labels are case-insensitive:

.
![Foo][]

[foo]: /url "title"
.
<p><img src="/url" alt="Foo" title="title" /></p>
.

As with full reference links, whitespace is allowed
between the two sets of brackets:

.
![foo] 
[]

[foo]: /url "title"
.
<p><img src="/url" alt="foo" title="title" /></p>
.

Shortcut:

.
![foo]

[foo]: /url "title"
.
<p><img src="/url" alt="foo" title="title" /></p>
.

.
![*foo* bar]

[*foo* bar]: /url "title"
.
<p><img src="/url" alt="&lt;em&gt;foo&lt;/em&gt; bar" title="title" /></p>
.

.
![[foo]]

[[foo]]: /url "title"
.
<p><img src="/url" alt="[foo]" title="title" /></p>
.

The link labels are case-insensitive:

.
![Foo]

[foo]: /url "title"
.
<p><img src="/url" alt="Foo" title="title" /></p>
.

If you just want bracketed text, you can backslash-escape the
opening `!` and `[`:

.
\!\[foo]

[foo]: /url "title"
.
<p>![foo]</p>
.

If you want a link after a literal `!`, backslash-escape the
`!`:

.
\![foo]

[foo]: /url "title"
.
<p>!<a href="/url" title="title">foo</a></p>
.

## Autolinks

Autolinks are absolute URIs and email addresses inside `<` and `>`.
They are parsed as links, with the URL or email address as the link
label.

A [URI autolink](#uri-autolink) <a id="uri-autolink"></a>
consists of `<`, followed by an [absolute
URI](#absolute-uri) not containing `<`, followed by `>`.  It is parsed
as a link to the URI, with the URI as the link's label.

An [absolute URI](#absolute-uri), <a id="absolute-uri"></a>
for these purposes, consists of a [scheme](#scheme) followed by a colon (`:`)
followed by zero or more characters other than ASCII whitespace and
control characters, `<`, and `>`.  If the URI includes these characters,
you must use percent-encoding (e.g. `%20` for a space).

The following [schemes](#scheme) <a id="scheme"></a>
are recognized (case-insensitive):
`coap`, `doi`, `javascript`, `aaa`, `aaas`, `about`, `acap`, `cap`,
`cid`, `crid`, `data`, `dav`, `dict`, `dns`, `file`, `ftp`, `geo`, `go`,
`gopher`, `h323`, `http`, `https`, `iax`, `icap`, `im`, `imap`, `info`,
`ipp`, `iris`, `iris.beep`, `iris.xpc`, `iris.xpcs`, `iris.lwz`, `ldap`,
`mailto`, `mid`, `msrp`, `msrps`, `mtqp`, `mupdate`, `news`, `nfs`,
`ni`, `nih`, `nntp`, `opaquelocktoken`, `pop`, `pres`, `rtsp`,
`service`, `session`, `shttp`, `sieve`, `sip`, `sips`, `sms`, `snmp`,`
soap.beep`, `soap.beeps`, `tag`, `tel`, `telnet`, `tftp`, `thismessage`,
`tn3270`, `tip`, `tv`, `urn`, `vemmi`, `ws`, `wss`, `xcon`,
`xcon-userid`, `xmlrpc.beep`, `xmlrpc.beeps`, `xmpp`, `z39.50r`,
`z39.50s`, `adiumxtra`, `afp`, `afs`, `aim`, `apt`,` attachment`, `aw`,
`beshare`, `bitcoin`, `bolo`, `callto`, `chrome`,` chrome-extension`,
`com-eventbrite-attendee`, `content`, `cvs`,` dlna-playsingle`,
`dlna-playcontainer`, `dtn`, `dvb`, `ed2k`, `facetime`, `feed`,
`finger`, `fish`, `gg`, `git`, `gizmoproject`, `gtalk`, `hcp`, `icon`,
`ipn`, `irc`, `irc6`, `ircs`, `itms`, `jar`, `jms`, `keyparc`, `lastfm`,
`ldaps`, `magnet`, `maps`, `market`,` message`, `mms`, `ms-help`,
`msnim`, `mumble`, `mvn`, `notes`, `oid`, `palm`, `paparazzi`,
`platform`, `proxy`, `psyc`, `query`, `res`, `resource`, `rmi`, `rsync`,
`rtmp`, `secondlife`, `sftp`, `sgn`, `skype`, `smb`, `soldat`,
`spotify`, `ssh`, `steam`, `svn`, `teamspeak`, `things`, `udp`,
`unreal`, `ut2004`, `ventrilo`, `view-source`, `webcal`, `wtai`,
`wyciwyg`, `xfire`, `xri`, `ymsgr`.

Here are some valid autolinks:

.
<http://foo.bar.baz>
.
<p><a href="http://foo.bar.baz">http://foo.bar.baz</a></p>
.

.
<http://foo.bar.baz?q=hello&id=22&boolean>
.
<p><a href="http://foo.bar.baz?q=hello&amp;id=22&amp;boolean">http://foo.bar.baz?q=hello&amp;id=22&amp;boolean</a></p>
.

.
<irc://foo.bar:2233/baz>
.
<p><a href="irc://foo.bar:2233/baz">irc://foo.bar:2233/baz</a></p>
.

Uppercase is also fine:

.
<MAILTO:FOO@BAR.BAZ>
.
<p><a href="MAILTO:FOO@BAR.BAZ">MAILTO:FOO@BAR.BAZ</a></p>
.

Spaces are not allowed in autolinks:

.
<http://foo.bar/baz bim>
.
<p>&lt;http://foo.bar/baz bim&gt;</p>
.

An [email autolink](#email-autolink) <a id="email-autolink"></a>
consists of `<`, followed by an [email address](#email-address),
followed by `>`.  The link's label is the email address,
and the URL is `mailto:` followed by the email address.

An [email address](#email-address), <a id="email-address"></a>
for these purposes, is anything that matches
the [non-normative regex from the HTML5
spec](http://www.whatwg.org/specs/web-apps/current-work/multipage/forms.html#e-mail-state-%28type=email%29):

    /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?
    (?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/

Examples of email autolinks:

.
<foo@bar.baz.com>
.
<p><a href="mailto:foo@bar.baz.com">foo@bar.baz.com</a></p>
.

.
<foo+special@Bar.baz-bar0.com>
.
<p><a href="mailto:foo+special@Bar.baz-bar0.com">foo+special@Bar.baz-bar0.com</a></p>
.

These are not autolinks:

.
<>
.
<p>&lt;&gt;</p>
.

.
<heck://bing.bong>
.
<p>&lt;heck://bing.bong&gt;</p>
.

.
< http://foo.bar >
.
<p>&lt; http://foo.bar &gt;</p>
.

.
<foo.bar.baz>
.
<p>&lt;foo.bar.baz&gt;</p>
.

.
<localhost:5001/foo>
.
<p>&lt;localhost:5001/foo&gt;</p>
.

.
http://google.com
.
<p>http://google.com</p>
.

.
foo@bar.baz.com
.
<p>foo@bar.baz.com</p>
.

## Raw HTML

Text between `<` and `>` that looks like an HTML tag is parsed as a
raw HTML tag and will be rendered in HTML without escaping.
Tag and attribute names are not limited to current HTML tags,
so custom tags (and even, say, DocBook tags) may be used.

Here is the grammar for tags:

A [tag name](#tag-name) <a id="tag-name"></a> consists of an ASCII letter
followed by zero or more ASCII letters or digits.

An [attribute](#attribute) <a id="attribute"></a> consists of whitespace,
an **attribute name**, and an optional **attribute value
specification**.

An [attribute name](#attribute-name) <a id="attribute-name"></a>
consists of an ASCII letter, `_`, or `:`, followed by zero or more ASCII
letters, digits, `_`, `.`, `:`, or `-`.  (Note:  This is the XML
specification restricted to ASCII.  HTML5 is laxer.)

An [attribute value specification](#attribute-value-specification)
<a id="attribute-value-specification"></a> consists of optional whitespace,
a `=` character, optional whitespace, and an [attribute
value](#attribute-value).

An [attribute value](#attribute-value) <a id="attribute-value"></a>
consists of an [unquoted attribute value](#unquoted-attribute-value),
a [single-quoted attribute value](#single-quoted-attribute-value),
or a [double-quoted attribute value](#double-quoted-attribute-value).

An [unquoted attribute value](#unquoted-attribute-value)
<a id="unquoted-attribute-value"></a> is a nonempty string of characters not
including spaces, `"`, `'`, `=`, `<`, `>`, or `` ` ``.

A [single-quoted attribute value](#single-quoted-attribute-value)
<a id="single-quoted-attribute-value"></a> consists of `'`, zero or more
characters not including `'`, and a final `'`.

A [double-quoted attribute value](#double-quoted-attribute-value)
<a id="double-quoted-attribute-value"></a> consists of `"`, zero or more
characters not including `"`, and a final `"`.

An [open tag](#open-tag) <a id="open-tag"></a> consists of a `<` character,
a [tag name](#tag-name), zero or more [attributes](#attribute),
optional whitespace, an optional `/` character, and a `>` character.

A [closing tag](#closing-tag) <a id="closing-tag"></a> consists of the
string `</`, a [tag name](#tag-name), optional whitespace, and the
character `>`.

An [HTML comment](#html-comment) <a id="html-comment"></a> consists of the
string `<!--`, a string of characters not including the string `--`, and
the string `-->`.

A [processing instruction](#processing-instruction)
<a id="processing-instruction"></a> consists of the string `<?`, a string
of characters not including the string `?>`, and the string
`?>`.

A [declaration](#declaration) <a id="declaration"></a> consists of the
string `<!`, a name consisting of one or more uppercase ASCII letters,
whitespace, a string of characters not including the character `>`, and
the character `>`.

A [CDATA section](#cdata-section) <a id="cdata-section"></a> consists of
the string `<![CDATA[`, a string of characters not including the string
`]]>`, and the string `]]>`.

An [HTML tag](#html-tag) <a id="html-tag"></a> consists of an [open
tag](#open-tag), a [closing tag](#closing-tag), an [HTML
comment](#html-comment), a [processing
instruction](#processing-instruction), an [element type
declaration](#element-type-declaration), or a [CDATA
section](#cdata-section).

Here are some simple open tags:

.
<a><bab><c2c>
.
<p><a><bab><c2c></p>
.

Empty elements:

.
<a/><b2/>
.
<p><a/><b2/></p>
.

Whitespace is allowed:

.
<a  /><b2
data="foo" >
.
<p><a  /><b2
data="foo" ></p>
.

With attributes:

.
<a foo="bar" bam = 'baz <em>"</em>'
_boolean zoop:33=zoop:33 />
.
<p><a foo="bar" bam = 'baz <em>"</em>'
_boolean zoop:33=zoop:33 /></p>
.

Illegal tag names, not parsed as HTML:

.
<33> <__>
.
<p>&lt;33&gt; &lt;__&gt;</p>
.

Illegal attribute names:

.
<a h*#ref="hi">
.
<p>&lt;a h*#ref=&quot;hi&quot;&gt;</p>
.

Illegal attribute values:

.
<a href="hi'> <a href=hi'>
.
<p>&lt;a href=&quot;hi'&gt; &lt;a href=hi'&gt;</p>
.

Illegal whitespace:

.
< a><
foo><bar/ >
.
<p>&lt; a&gt;&lt;
foo&gt;&lt;bar/ &gt;</p>
.

Missing whitespace:

.
<a href='bar'title=title>
.
<p>&lt;a href='bar'title=title&gt;</p>
.

Closing tags:

.
</a>
</foo >
.
<p></a>
</foo ></p>
.

Illegal attributes in closing tag:

.
</a href="foo">
.
<p>&lt;/a href=&quot;foo&quot;&gt;</p>
.

Comments:

.
foo <!-- this is a
comment - with hyphen -->
.
<p>foo <!-- this is a
comment - with hyphen --></p>
.

.
foo <!-- not a comment -- two hyphens -->
.
<p>foo &lt;!-- not a comment -- two hyphens --&gt;</p>
.

Processing instructions:

.
foo <?php echo $a; ?>
.
<p>foo <?php echo $a; ?></p>
.

Declarations:

.
foo <!ELEMENT br EMPTY>
.
<p>foo <!ELEMENT br EMPTY></p>
.

CDATA sections:

.
foo <![CDATA[>&<]]>
.
<p>foo <![CDATA[>&<]]></p>
.

Entities are preserved in HTML attributes:

.
<a href="&ouml;">
.
<p><a href="&ouml;"></p>
.

Backslash escapes do not work in HTML attributes:

.
<a href="\*">
.
<p><a href="\*"></p>
.

.
<a href="\"">
.
<p>&lt;a href=&quot;&quot;&quot;&gt;</p>
.

## Hard line breaks

A line break (not in a code span or HTML tag) that is preceded
by two or more spaces is parsed as a linebreak (rendered
in HTML as a `<br />` tag):

.
foo  
baz
.
<p>foo<br />
baz</p>
.

For a more visible alternative, a backslash before the newline may be
used instead of two spaces:

.
foo\
baz
.
<p>foo<br />
baz</p>
.

More than two spaces can be used:

.
foo       
baz
.
<p>foo<br />
baz</p>
.

Leading spaces at the beginning of the next line are ignored:

.
foo  
     bar
.
<p>foo<br />
bar</p>
.

.
foo\
     bar
.
<p>foo<br />
bar</p>
.

Line breaks can occur inside emphasis, links, and other constructs
that allow inline content:

.
*foo  
bar*
.
<p><em>foo<br />
bar</em></p>
.

.
*foo\
bar*
.
<p><em>foo<br />
bar</em></p>
.

Line breaks do not occur inside code spans

.
`code  
span`
.
<p><code>code span</code></p>
.

.
`code\
span`
.
<p><code>code\ span</code></p>
.

or HTML tags:

.
<a href="foo  
bar">
.
<p><a href="foo  
bar"></p>
.

.
<a href="foo\
bar">
.
<p><a href="foo\
bar"></p>
.

## Soft line breaks

A regular line break (not in a code span or HTML tag) that is not
preceded by two or more spaces is parsed as a softbreak.  (A
softbreak may be rendered in HTML either as a newline or as a space.
The result will be the same in browsers. In the examples here, a
newline will be used.)

.
foo
baz
.
<p>foo
baz</p>
.

Spaces at the end of the line and beginning of the next line are
removed:

.
foo 
 baz
.
<p>foo
baz</p>
.

A conforming parser may render a soft line break in HTML either as a
line break or as a space.

A renderer may also provide an option to render soft line breaks
as hard line breaks.

## Strings

Any characters not given an interpretation by the above rules will
be parsed as string content.

.
hello $.;'there
.
<p>hello $.;'there</p>
.

.
Foo χρῆν
.
<p>Foo χρῆν</p>
.

Internal spaces are preserved verbatim:

.
Multiple     spaces
.
<p>Multiple     spaces</p>
.

<!-- END TESTS -->

# Appendix A: A parsing strategy {-}

## Overview {-}

Parsing has two phases:

1. In the first phase, lines of input are consumed and the block
structure of the document---its division into paragraphs, block quotes,
list items, and so on---is constructed.  Text is assigned to these
blocks but not parsed. Link reference definitions are parsed and a
map of links is constructed.

2. In the second phase, the raw text contents of paragraphs and headers
are parsed into sequences of Markdown inline elements (strings,
code spans, links, emphasis, and so on), using the map of link
references constructed in phase 1.

## The document tree {-}

At each point in processing, the document is represented as a tree of
**blocks**.  The root of the tree is a `document` block.  The `document`
may have any number of other blocks as **children**.  These children
may, in turn, have other blocks as children.  The last child of a block
is normally considered **open**, meaning that subsequent lines of input
can alter its contents.  (Blocks that are not open are **closed**.)
Here, for example, is a possible document tree, with the open blocks
marked by arrows:

``` tree
-> document
  -> block_quote
       paragraph
         "Lorem ipsum dolor\nsit amet."
    -> list (type=bullet tight=true bullet_char=-)
         list_item
           paragraph
             "Qui *quodsi iracundia*"
      -> list_item
        -> paragraph
             "aliquando id"
```

## How source lines alter the document tree {-}

Each line that is processed has an effect on this tree.  The line is
analyzed and, depending on its contents, the document may be altered
in one or more of the following ways:

1. One or more open blocks may be closed.
2. One or more new blocks may be created as children of the
   last open block.
3. Text may be added to the last (deepest) open block remaining
   on the tree.

Once a line has been incorporated into the tree in this way,
it can be discarded, so input can be read in a stream.

We can see how this works by considering how the tree above is
generated by four lines of Markdown:

``` markdown
> Lorem ipsum dolor
sit amet.
> - Qui *quodsi iracundia*
> - aliquando id
```

At the outset, our document model is just

``` tree
-> document
```

The first line of our text,

``` markdown
> Lorem ipsum dolor
```

causes a `block_quote` block to be created as a child of our
open `document` block, and a `paragraph` block as a child of
the `block_quote`.  Then the text is added to the last open
block, the `paragraph`:

``` tree
-> document
  -> block_quote
    -> paragraph
         "Lorem ipsum dolor"
```

The next line,

``` markdown
sit amet.
```

is a "lazy continuation" of the open `paragraph`, so it gets added
to the paragraph's text:

``` tree
-> document
  -> block_quote
    -> paragraph
         "Lorem ipsum dolor\nsit amet."
```

The third line,

``` markdown
> - Qui *quodsi iracundia*
```

causes the `paragraph` block to be closed, and a new `list` block
opened as a child of the `block_quote`.  A `list_item` is also
added as a child of the `list`, and a `paragraph` as a child of
the `list_item`.  The text is then added to the new `paragraph`:

``` tree
-> document
  -> block_quote
       paragraph
         "Lorem ipsum dolor\nsit amet."
    -> list (type=bullet tight=true bullet_char=-)
      -> list_item
        -> paragraph
             "Qui *quodsi iracundia*"
```

The fourth line,

``` markdown
> - aliquando id
```

causes the `list_item` (and its child the `paragraph`) to be closed,
and a new `list_item` opened up as child of the `list`.  A `paragraph`
is added as a child of the new `list_item`, to contain the text.
We thus obtain the final tree:

``` tree
-> document
  -> block_quote
       paragraph
         "Lorem ipsum dolor\nsit amet."
    -> list (type=bullet tight=true bullet_char=-)
         list_item
           paragraph
             "Qui *quodsi iracundia*"
      -> list_item
        -> paragraph
             "aliquando id"
```

## From block structure to the final document {-}

Once all of the input has been parsed, all open blocks are closed.

We then "walk the tree," visiting every node, and parse raw
string contents of paragraphs and headers as inlines.  At this
point we have seen all the link reference definitions, so we can
resolve reference links as we go.

``` tree
document
  block_quote
    paragraph
      str "Lorem ipsum dolor"
      softbreak
      str "sit amet."
    list (type=bullet tight=true bullet_char=-)
      list_item
        paragraph
          str "Qui "
          emph
            str "quodsi iracundia"
      list_item
        paragraph
          str "aliquando id"
```

Notice how the newline in the first paragraph has been parsed as
a `softbreak`, and the asterisks in the first list item have become
an `emph`.

The document can be rendered as HTML, or in any other format, given
an appropriate renderer.
