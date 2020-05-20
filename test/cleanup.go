package main

import "log"
import "os"

import "switcheroo"

func main() {
	logger := log.New(os.Stdout, "[cleanup] ", log.Ltime)
	swo, err := switcheroo.NewSwitcherooWithSudoIptables("WhateverNamespace", 9999, logger)
	if err != nil {
		panic(err)
	}
	swo.SetFlags(switcheroo.ENABLE_LOCALHOST | switcheroo.ENABLE_NETWORK)
	err = swo.Cleanup()
	if err != nil {
		panic(err)
	}
}
