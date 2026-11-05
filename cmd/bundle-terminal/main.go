// Copy the exact runtime selected by Golet into the application resources.
package main

import (
	"github.com/HyperparameterSI/golet/backend/plugins/sqlite"
	"github.com/HyperparameterSI/golet/backend/plugins/terminal"
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	for _, locate := range []func() (string, error){terminal.LibraryPath, sqlite.LibraryPath} {
		path, err := locate()
		if err != nil {
			log.Fatal(err)
		}
		in, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		defer in.Close()
		dst := "bin/Omnicode.app/Contents/Resources"
		if len(os.Args) > 1 {
			dst = os.Args[1]
		}
		if err = os.MkdirAll(dst, 0755); err != nil {
			log.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dst, filepath.Base(path)))
		if err != nil {
			log.Fatal(err)
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			log.Fatal(err)
		}
		if closeErr != nil {
			log.Fatal(closeErr)
		}
	}
}
