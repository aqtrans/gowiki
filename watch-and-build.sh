#!/bin/sh
CompileDaemon -exclude-dir=md -exclude-dir=md2 -exclude-dir=.git -exclude-dir=vendor -include="*.tmpl" -command="./wiki -d"
