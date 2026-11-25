// Command skirmishbench prints one JSON line for each of two measurements of the
// skirmish simulation on a 128 against 128 battle: the battle the scouts fight
// on their own, and the same battle with six squads receiving group orders. The
// same program runs natively and as WebAssembly, so the reports are directly
// comparable and their traces must match.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/krapht-foundation/goje/internal/skirmishbench"
)

const (
	warmup = 300
	ticks  = 600
)

func main() {
	for _, run := range []func(int, int) (skirmishbench.Report, error){skirmishbench.Run, skirmishbench.RunOrders} {
		report, err := run(warmup, ticks)
		if err != nil {
			fmt.Fprintln(os.Stderr, "skirmishbench:", err)
			os.Exit(1)
		}
		line, err := json.Marshal(struct {
			Target string `json:"target"`
			skirmishbench.Report
		}{runtime.GOOS + "/" + runtime.GOARCH, report})
		if err != nil {
			fmt.Fprintln(os.Stderr, "skirmishbench:", err)
			os.Exit(1)
		}
		fmt.Println(string(line))
	}
}
