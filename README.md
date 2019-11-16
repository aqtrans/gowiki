# Gowiki - a Gitit clone written in Golang

[![build status](https://git.jba.io/go/wiki/badges/master/build.svg)](https://git.jba.io/go/wiki/commits/master) | [![coverage report](https://git.jba.io/go/wiki/badges/master/coverage.svg)](https://git.jba.io/go/wiki/commits/master)

This is my attempt to replicate the featureset of Gitit, written in Go. 

At this point basic functionality, view/edit/history is stable and at parity with Gitit. 

Bonus features: 
- Instead of a global public/private switch, pages are presumed private, and can be made public via a boolean set in the 'frontmatter' of each page.
- No public write access whatsoever.
- Tag support inside the frontmatter.

