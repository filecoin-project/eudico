package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	abciserver "github.com/tendermint/tendermint/abci/server"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/filecoin-project/lotus/chain/consensus/tendermint"
)

var socketAddr string

func init() {
	flag.StringVar(&socketAddr, "socket-addr", "tcp://127.0.0.1:26658", "Unix domain socket address")
}

func main() {
	app, err := tendermint.NewApplication()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating tendermint application: %v", err)
		os.Exit(1)
	}

	flag.Parse()

	logger := log.MustNewDefaultLogger(log.LogFormatPlain, log.LogLevelInfo, false)

	server := abciserver.NewSocketServer(socketAddr, app)
	server.SetLogger(logger)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "error starting socket server: %v", err)
		os.Exit(1)
	}
	defer server.Stop()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	os.Exit(0)
}
