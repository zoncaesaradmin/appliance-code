//go:build linux

package main

import (
	"log"
	"syscall"
)

func applyVLLMRlimits() {
	const rlimitMemlock = 8
	var memlock syscall.Rlimit
	if err := syscall.Getrlimit(rlimitMemlock, &memlock); err == nil {
		memlock.Cur = memlock.Max
		if err := syscall.Setrlimit(rlimitMemlock, &memlock); err != nil {
			log.Printf("unable to raise memlock limit to container maximum: %v", err)
		}
	}
	var stack syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_STACK, &stack); err == nil {
		const wanted = uint64(67108864)
		if stack.Max >= wanted {
			stack.Cur = wanted
		} else {
			stack.Cur = stack.Max
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_STACK, &stack); err != nil {
			log.Printf("unable to set vLLM stack limit: %v", err)
		}
	}
}
