#!/bin/bash

set -e

function build_css()
{
    sassc -M scss/grid.scss assets/css/wiki.css
}

while [ "$1" != "" ]; do 
    case $1 in
        run)
            GO111MODULE=on go generate
            build_css
            GO111MODULE=on go run -race -tags=dev .
            ;;
        run-prod)
            GO111MODULE=on go generate
            build_css
            GO111MODULE=on go run -race .
            ;;            
        css)
            build_css
            exit
            ;;
        build)
            GO111MODULE=on go generate
            build_css
            GO111MODULE=on go build -o gowiki .
            exit
            ;;
        build-pkg)
            GO111MODULE=on go generate
            build_css
            GO111MODULE=on go build -o gowiki
            debuild -us -uc -b
            exit
            ;;
    esac
done
