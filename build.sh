#!/bin/bash

set -euo pipefail

DEBVERSION=1.0.$(date +'%s')-$(git rev-parse --short HEAD)
APPNAME=gowiki

function build_css()
{
    sassc -t compressed -M scss/grid.scss assets/css/wiki.css
}

function build_debian()
{
    podman run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang:buster go build -buildmode=pie -v -o $APPNAME
}

function test_it() {
    go test -race -cover
    #go test -cover
    #go test -bench=.
}

# Build Debian package inside a container
function build_package() {
    podman run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp debian:buster ./build-pkg.sh $DEBVERSION
}

while [ "$1" != "" ]; do 
    case $1 in
        test)
            test_it
            exit
            ;;
        run)
            test_it
            build_css
            go run -race .
            ;;
        css)
            build_css
            exit
            ;;
        build)
            test_it
            build_css
            go build -buildmode=pie -o $APPNAME
            exit
            ;;
        pkg)
            if [ "$(which dch)" != "" ]; then 
                test_it
                build_css
                go build -buildmode=pie -o $APPNAME
                ./build-pkg.sh $DEBVERSION
            else
                echo "dch not found. building inside container."
                test_it
                build_css                
                build_debian
                build_package
            fi
            exit
            ;;
        build-debian)
            echo "Building binary inside Debian container..."
            test_it
            build_css
            build_debian
            exit
            ;;
        deploy-binary)
            test_it
            build_css
            build_debian
            ansible-playbook -i bob.jba.io, deploy.yml
            exit
            ;;
        deploy)
            test_it
            build_css
            build_debian
            build_package
            scp $APPNAME-$DEBVERSION.deb bob:
            ssh bob.jba.io sudo dpkg -i $APPNAME-$DEBVERSION.deb
            exit
            ;;            
    esac
done
