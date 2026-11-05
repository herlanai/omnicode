.PHONY: run build test bundle icon pi-runtime clean

run:
	go run .

build:
	go build -o bin/omnicode .

test:
	go test ./...
	go vet ./...

icon: assets/omnicode-icon.png scripts/build-icon.sh
	sh scripts/build-icon.sh

pi-runtime:
	sh scripts/prepare-pi.sh

lsp-runtime:
	sh scripts/prepare-lsp.sh

bundle: build icon pi-runtime lsp-runtime
	bash scripts/package-macos.sh

clean:
	rm -rf bin artifacts
