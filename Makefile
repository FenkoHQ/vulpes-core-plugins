.PHONY: test lint build clean

PLUGINS := authn-static-api-key router-weighted router-litellm upstream-openai observer-stdout observer-prometheus observer-otel observer-s3-transcripts

PACKAGES := ./sdk \
	./plugins/authn-static-api-key \
	./plugins/router-weighted \
	./plugins/router-litellm \
	./plugins/upstream-openai \
	./plugins/observer-stdout \
	./plugins/observer-prometheus \
	./plugins/observer-otel \
	./plugins/observer-s3-transcripts

test:
	go test $(PACKAGES)

lint:
	go vet $(PACKAGES)

build:
	mkdir -p bin
	for p in $(PLUGINS); do go build -o bin/$$p ./plugins/$$p; done

clean:
	rm -rf bin
