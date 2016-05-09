FROM golang:1.6

EXPOSE 3001
RUN mkdir -p /go/src/app
RUN mkdir -p /root/.ssh
WORKDIR /go/src/app

ADD ssh_key /root/.ssh/id_rsa
RUN chmod 700 /root/.ssh/id_rsa
RUN echo "Host jba.io\n\tStrictHostKeyChecking no\n\n" > /root/.ssh/config

COPY . /go/src/app
RUN go-wrapper download
RUN go-wrapper install
RUN go build -o ./wiki

CMD ["./wiki"]
