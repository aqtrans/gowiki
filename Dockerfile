FROM aqtrans/golang-npm:latest

RUN mkdir -p /go/src/wiki
WORKDIR /go/src/wiki

## Running with PWD: docker run -it --rm --name gowiki-instance -p 3000:3000 -v (PWD):/go/src/wiki -w /go/src/wiki gowiki
## Running: docker run -it --rm --name gowiki-instance -p 3000:3000 -w /go/src/wiki gowiki

# Install rice and govendor
RUN go get github.com/GeertJohan/go.rice/rice && go get github.com/kardianos/govendor

ADD . /go/src/wiki/
RUN /bin/sh ./build_css.sh
RUN govendor sync
RUN go get -d
RUN rice embed-go
RUN go build -o ./wiki

# Expose the application on port 3000
#EXPOSE 3000

# Set the entry point of the container to the bee command that runs the
# application and watches for changes
CMD ["./wiki"]
