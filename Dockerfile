FROM debian:jessie
MAINTAINER Harper Rules, LLC. <harper@harperrules.com>

# Install necessary packages
ENV DEBIAN_FRONTEND noninteractive
RUN echo "Acquire::http {No-Cache=True;};" > /etc/apt/apt.conf.d/no-cache
RUN apt-get update --fix-missing -y &&\
    apt-get install golang -y &&\
    rm -rf /var/cache/apt/* && rm -rf /var/lib/apt/lists/*_*

ADD ./ /snapper
WORKDIR /snapper
RUN go get
RUN go build

# Configure runtime
USER nobody
CMD snapper
