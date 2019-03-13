#!/bin/sh
go build -o gowiki
debuild -us -uc -b
