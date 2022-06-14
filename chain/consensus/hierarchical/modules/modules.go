package module

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/manager"
)

func SetSubMgrIface(mgr *manager.Service) subnet.Manager {
	return mgr
}
