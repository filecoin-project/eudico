package common

import (
	"context"
	"sort"

	"github.com/filecoin-project/go-address"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/types"
)

type MessageArray []*types.Message

func (ma MessageArray) Len() int {
	return len(ma)
}

func (ma MessageArray) Less(i, j int) bool {
	return ma[i].Nonce <= ma[j].Nonce
}

func (ma MessageArray) Swap(i, j int) {
	ma[i], ma[j] = ma[j], ma[i]
}

type NonceArray []uint64

func (ma NonceArray) Len() int {
	return len(ma)
}

func (ma NonceArray) Less(i, j int) bool {
	return ma[i] <= ma[j]
}

func (ma NonceArray) Swap(i, j int) {
	ma[i], ma[j] = ma[j], ma[i]
}

// Take messages from meta before the transformation
// to the meta nonce, and recover original nonce
func recoverOriginalNonce(ctx context.Context, r *resolver.Resolver, n uint64, sca *sca.SCAState,
	store adt.Store, msg []*types.Message) ([]*types.Message, error) {
	meta, found, err := sca.GetBottomUpMsgMeta(store, n)
	if err != nil {
		return []*types.Message{}, xerrors.Errorf("getting bottomup msgmeta: %w", err)
	}
	if !found {
		return []*types.Message{}, xerrors.Errorf("No BottomUp meta found for nonce in SCA: %d", n)
	}
	c, _ := meta.Cid()
	orig, _, err := r.ResolveCrossMsgs(ctx, c, address.SubnetID(meta.From))
	if err != nil {
		return []*types.Message{}, xerrors.Errorf("error resolving cross-msgs: %w", err)
	}

	for _, o := range orig {
		for _, m := range msg {
			origNonce := o.Nonce
			// If changing original to the meta nonce is equal
			// it means they are the same msg and we can recover original nonce
			o.Nonce = n
			if o.Equals(m) {
				m.Nonce = origNonce
			}
		}
	}
	return msg, nil

}

func sortByOriginalNonce(ctx context.Context, r *resolver.Resolver, n uint64, sca *sca.SCAState,
	store adt.Store, msg []*types.Message) ([]*types.Message, error) {
	bu, err := recoverOriginalNonce(ctx, r, n, sca, store, msg)
	if err != nil {
		return []*types.Message{}, err
	}

	// Sort messages
	mabu := MessageArray(bu)
	sort.Sort(mabu)

	// Recover meta nonce in msgs.
	for i := range mabu {
		mabu[i].Nonce = n
	}

	return mabu, nil
}
