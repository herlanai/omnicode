.PHONY: run build test bundle bundle-intel bundle-all icon pi-runtime lsp-runtime clean

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

bundle:
	bash scripts/build-macos.sh

bundle-intel:
	OMNICODE_ARCH=x86_64 bash scripts/build-macos.sh

bundle-all:
	OMNICODE_ARCH=arm64 bash scripts/build-macos.sh
	OMNICODE_ARCH=x86_64 bash scripts/build-macos.sh

clean:
	rm -rf bin artifacts
