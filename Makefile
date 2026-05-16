.PHONY: test lint build clean

PLUGINS := authn-static-api-key authn-postgres-api-key cache-memory ratelimit-memory router-weighted router-litellm router-consul prompt-context-injector prompt-template-registry upstream-openai upstream-codex observer-stdout observer-prometheus observer-otel observer-s3-transcripts

PACKAGES := ./sdk \
	./plugins/authn-static-api-key \
	./plugins/authn-postgres-api-key \
	./plugins/cache-memory \
	./plugins/ratelimit-memory \
	./plugins/router-weighted \
	./plugins/router-litellm \
	./plugins/router-consul \
	./plugins/prompt-context-injector \
	./plugins/prompt-template-registry \
	./plugins/upstream-openai \
	./plugins/upstream-codex \
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
