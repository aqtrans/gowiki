#!/bin/sh
cd ../
go build -o debbuild/gowiki
cd debbuild/
debuild -us -uc -b
