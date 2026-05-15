.PHONY: test lint build clean

PLUGINS := authn-static-api-key router-weighted upstream-openai observer-stdout observer-prometheus

PACKAGES := ./sdk \
	./plugins/authn-static-api-key \
	./plugins/router-weighted \
	./plugins/upstream-openai \
	./plugins/observer-stdout \
	./plugins/observer-prometheus

test:
	go test $(PACKAGES)

lint:
	go vet $(PACKAGES)

build:
	mkdir -p bin
	for p in $(PLUGINS); do go build -o bin/$$p ./plugins/$$p; done

clean:
	rm -rf bin
