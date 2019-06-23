FROM golang:alpine as BUILDER

RUN apk update && apk add --no-cache git
WORKDIR /src
COPY . .

RUN go get -d -v github.com/rspier/rt-static/cmd/server
RUN CGO_ENABLED=0 go build -o /go/bin/server github.com/rspier/rt-static/cmd/server

FROM scratch

WORKDIR /

COPY --from=builder /src/web/templates/* /web/templates/
COPY --from=builder /go/bin/server /server.bin

ENTRYPOINT ["/server.bin"]
