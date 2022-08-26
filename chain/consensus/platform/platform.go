package platform

import (
	"fmt"
	"net/http"
	"path"
	"sync"

	"github.com/gorilla/mux"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/submgr"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/impl"
)

func ServeNamedAPI(m *sync.Mutex, mux *mux.Router, serverOptions []jsonrpc.ServerOption) func(p string, iapi api.FullNode) error {
	var err error

	return func(p string, iapi api.FullNode) error {
		pp := path.Join("/subnet/", p+"/")

		var h http.Handler
		// If this is a full node API
		nodeAPI, ok := iapi.(*impl.FullNodeAPI)
		if ok {
			// Instantiate the full node handler.
			h, err = node.FullNodeHandler(pp, nodeAPI, true, serverOptions...)
			if err != nil {
				return fmt.Errorf("failed to instantiate rpc handler: %s", err)
			}
		} else {
			// If not instantiate a subnet api
			managerAPI, ok := iapi.(*submgr.API)
			if !ok {
				return xerrors.Errorf("Couldn't instantiate new subnet API. Something went wrong: %s", err)
			}
			// Instantiate the full node handler.
			m.Lock()
			h, err = submgr.FullNodeHandler(pp, managerAPI, true, serverOptions...)
			m.Unlock()
			if err != nil {
				m.Unlock()
				return fmt.Errorf("failed to instantiate rpc handler: %s", err)
			}
		}
		m.Lock()
		mux.NewRoute().PathPrefix(pp).Handler(h)
		m.Unlock()
		return nil
	}
}
