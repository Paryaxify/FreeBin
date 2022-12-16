# syntax=docker/dockerfile:1

FROM golang:1.18-alpine
RUN apk --no-cache add make git gcc libtool musl-dev ca-certificates dumb-init
WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

# RUN go build -o /docker-freebin

EXPOSE 8080

CMD [ "./fossbin" ]
