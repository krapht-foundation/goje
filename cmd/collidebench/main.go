// Command collidebench prints one JSON line that measures the collision package
// on a dense scene. The same program runs natively and as WebAssembly, so the
// two reports are directly comparable and their hashes must match.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/krapht-foundation/goje/internal/collidebench"
)

const (
	warmup = 60
	ticks  = 600
)

func main() {
	report, err := collidebench.Run(warmup, ticks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "collidebench:", err)
		os.Exit(1)
	}
	line, err := json.Marshal(struct {
		Target string `json:"target"`
		collidebench.Report
	}{runtime.GOOS + "/" + runtime.GOARCH, report})
	if err != nil {
		fmt.Fprintln(os.Stderr, "collidebench:", err)
		os.Exit(1)
	}
	fmt.Println(string(line))
}
