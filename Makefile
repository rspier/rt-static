MOD=github.com/rspier/rt-static

default:
	@echo -n "Targets:  "
	@egrep '^[a-z]+:' Makefile | cut -d: -f1 | xargs echo

deps:
	go get -v $(MOD)/...

# will build into ~/go/bin
build:
	go build $(MOD)/...

DATAZIP=/big/rt-static/perl5.zip
SITE=perl5
PREFIX=/perl5
run:
	go run cmd/server/server.go \
		--logtostderr \
		--data "$(DATAZIP)" \
		--index "$(DATAZIP)" \
		--site "$(SITE)" \
		--githubprefix https://github.com/perl/perl5 \
		--snapshot "2019-10-17T15:30" \
		--prefix "$(PREFIX)"

VERSION=latest
docker-build:
	docker build -t rt-static-server:$(VERSION) .

docker-push:
# docker tag and docker push goes here
