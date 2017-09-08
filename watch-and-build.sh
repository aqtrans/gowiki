#!/bin/sh
#CompileDaemon -exclude-dir=md -exclude-dir=md2 -exclude-dir=.git -exclude-dir=vendor -include="*.tmpl" -command="./wiki -d"
#go get github.com/cespare/reflex
#reflex -c reflex.conf
#ls scss/*.scss | entr sass scss/grid.scss assets/css/wiki.css &&
## Using Entr (http://entrproject.org/) now, as reflex wasn't working properly on OSX
#### Rebuilding SASS and Go in one
ls -d main.go main_test.go templates/** scss/grid.scss | entr -r sh -c 'sass scss/grid.scss assets/css/wiki.css && go run main.go'