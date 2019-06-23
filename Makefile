MOD=github.com/rspier/rt-static

deps:
	go get -v $(MOD)/...

# will build into ~/go/bin
build:
	go build $(MOD)/...

docker-build:
	docker build -t rt-static-server:latest .

docker-push:
# docker tag and docker push goes here
