# syntax=docker/dockerfile:1

FROM golang:1.18-alpine
RUN apk --no-cache add make git gcc libtool musl-dev ca-certificates dumb-init
RUN mkdir /app
WORKDIR /app
ADD . /app

RUN go build -o main .

EXPOSE 8080

CMD [ "/app/main" ]
