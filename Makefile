MOD=github.com/rspier/rt-static

deps:
	go get -v $(MOD)/...

# will build into ~/go/bin
build:
	go build $(MOD)/...
