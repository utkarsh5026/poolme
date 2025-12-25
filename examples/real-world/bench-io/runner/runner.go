package main

import (
	"log"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

func main() {
	if err := runner.Run(); err != nil {
		log.Fatalf("Runner failed: %v", err)
	}
}
