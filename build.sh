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
    podman run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp golang:bullseye go build -buildmode=pie -v -ldflags "-X main.sha1ver=$(git rev-parse HEAD) -X main.buildTime=$(date +'%Y-%m-%d_%T')" -o $APPNAME
}

function test_it() {
    go test -race -cover
    #go test -cover
    #go test -bench=.
}

# Build Debian package inside a container
function build_package() {
    podman run --rm -v "$PWD":/usr/src/myapp -w /usr/src/myapp debian:bullseye ./build-pkg.sh $DEBVERSION
}

# Build plain binary on host system
function build_binary() {
    go build -buildmode=pie -ldflags "-X main.sha1ver=$(git rev-parse HEAD) -X main.buildTime=$(date +'%Y-%m-%d_%T')" -o $APPNAME
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
            go run -ldflags "-X main.sha1ver=$(git rev-parse HEAD) -X main.buildTime=$(date +'%Y-%m-%d_%T')" -race .
            ;;
        css)
            build_css
            exit
            ;;
        build)
            test_it
            build_css
            build_binary
            exit
            ;;
        pkg)
            if [ "$(which dch)" != "" ]; then 
                test_it
                build_css
                build_binary
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
        build-bsd)
            build_css
            GOOS=openbsd GOARCH=amd64 go build -ldflags "-X main.sha1ver=$(git rev-parse HEAD) -X main.buildTime=$(date +'%Y-%m-%d_%T')" -o $APPNAME-obsd
            exit
            ;;
    esac
done
