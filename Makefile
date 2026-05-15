.PHONY: test lint build clean

PLUGINS := authn-static-api-key router-weighted upstream-openai observer-stdout

test:
	go test ./sdk ./plugins/authn-static-api-key ./plugins/router-weighted ./plugins/upstream-openai ./plugins/observer-stdout

lint:
	go vet ./sdk ./plugins/authn-static-api-key ./plugins/router-weighted ./plugins/upstream-openai ./plugins/observer-stdout

build:
	mkdir -p bin
	for p in $(PLUGINS); do go build -o bin/$$p ./plugins/$$p; done

clean:
	rm -rf bin
