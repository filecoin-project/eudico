package main

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/common"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/node"
)

var filcnsCmd = &cli.Command{
	Name:  "filcns",
	Usage: "Filecoin Consensus consensus testbed",
	Subcommands: []*cli.Command{
		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), filcns.NewFilecoinExpectedConsensus),
			node.Override(new(stmgr.Executor), common.NewFilCnsTipSetExecutor),
			node.Override(new(store.WeightFunc), filcns.Weight),
			node.Override(new(stmgr.UpgradeSchedule), common.DefaultUpgradeSchedule()),
		)),
	},
}
