package module

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
)

func SetSubMgrIface(mgr *submgr.Service) subnet.Manager {
	return mgr
}
