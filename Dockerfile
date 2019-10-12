FROM golang:alpine as BUILDER

RUN apk update && apk add --no-cache git
WORKDIR /src

# << bake dependencies into image
COPY go.mod .
COPY go.sum .
RUN go mod download
# >>

# bring source into image
COPY . .

# build
RUN CGO_ENABLED=0 go build -o /go/bin/server github.com/rspier/rt-static/cmd/server

RUN touch /.empty

FROM scratch

WORKDIR /

COPY --from=builder /src/web/templates/ /web/templates/
COPY --from=builder /src/web/static/ /web/static/
COPY --from=builder /go/bin/server /server.bin
# If we don't put something in the /tmp/ directory, it doesn't exist,
# since this is FROM scratch.
COPY --from=builder /.empty /tmp/.empty

ENTRYPOINT ["/server.bin"]
