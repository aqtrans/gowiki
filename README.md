# My Golang-powered wiki

[![build status](https://git.jba.io/go/wiki/badges/master/build.svg)](https://git.jba.io/go/wiki/commits/master) | [![coverage report](https://git.jba.io/go/wiki/badges/master/coverage.svg)](https://git.jba.io/go/wiki/commits/master)

This is my attempt to replicate the featureset of Gitit, my current wiki of choice.

Main features that stand apart from Gitit and most other wikis I tried:  
- Instead of a public/private switch, pages are presumed private, and can be made public via a frontmatter boolean.
- No public write access whatsoever.
- Tag support inside the frontmatter.