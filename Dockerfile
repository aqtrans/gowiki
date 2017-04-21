FROM golang:1.8

RUN mkdir -p /go/src/wiki
WORKDIR /go/src/wiki

## Running with PWD: docker run -it --rm --name gowiki-instance -p 3000:3000 -v (PWD):/go/src/wiki -w /go/src/wiki gowiki
## Running: docker run -it --rm --name gowiki-instance -p 3000:3000 -w /go/src/wiki gowiki

# Install rice and bolt, for testing
RUN go get github.com/GeertJohan/go.rice/rice && go get github.com/boltdb/bolt && go get github.com/golang/dep/...

COPY . /go/src/wiki/
RUN go get -d
RUN dep ensure
RUN go build -o ./wiki

# Expose the application on port 3000
EXPOSE 3000

# Set the entry point of the container to the bee command that runs the
# application and watches for changes
CMD ["./wiki", "-d"]


