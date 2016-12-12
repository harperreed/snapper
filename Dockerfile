FROM golang:onbuild
MAINTAINER Harper Rules, LLC. <harper@harperrules.com>
ADD snapper-config.yaml /go/src/bin/
EXPOSE 8080