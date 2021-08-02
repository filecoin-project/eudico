package main

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/node"
)

var delegatedCmd = &cli.Command{
	Name:  "delegated",
	Usage: "Delegated consensus testbed",
	Subcommands: []*cli.Command{

		daemonCmd(node.Override(new(consensus.Consensus), delegcns.NewDelegatedConsensus)),
	},
}
