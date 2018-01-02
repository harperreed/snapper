FROM golang:alpine
MAINTAINER Harper Rules, LLC. <harper@harperrules.com>

FROM golang

ARG app_env
ENV APP_ENV $app_env

RUN mkdir -p /go/src/github.com/harperreed/snapper/
COPY snapper.go /go/src/github.com/harperreed/snapper/
WORKDIR /go/src/github.com/harperreed/snapper/

RUN go get ./
RUN go build

ENTRYPOINT /go/bin/snapper
  
EXPOSE 8080