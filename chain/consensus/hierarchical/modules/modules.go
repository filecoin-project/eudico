package module

import (
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	subnetmgr "github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/manager"
)

func SetSubMgrIface(mgr *subnetmgr.SubnetMgr) subnet.SubnetMgr {
	return mgr
}
