package modules

import (
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/vm"
	"go.uber.org/fx"
)

func StateManager(lc fx.Lifecycle, cs *store.ChainStore, exec stmgr.Executor, r *resolver.Resolver, sys vm.SyscallBuilder, us stmgr.UpgradeSchedule, b beacon.Schedule) (*stmgr.StateManager, error) {
	sm, err := stmgr.NewStateManager(cs, exec, r, sys, us, b)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStart: sm.Start,
		OnStop:  sm.Stop,
	})
	return sm, nil
}
