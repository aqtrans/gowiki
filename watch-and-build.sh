#!/bin/sh
CompileDaemon -exclude-dir=.git -include="*.tmpl" -command="./wiki -d"
