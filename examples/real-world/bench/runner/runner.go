package main

import (
	"log"

	"github.com/utkarsh5026/poolme/examples/real-world/common/runner"
)

func main() {
	if err := runner.Run(); err != nil {
		log.Fatalf("Runner failed: %v", err)
	}
}
