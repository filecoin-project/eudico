package sca

import (
	"context"

	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	bstore "github.com/filecoin-project/lotus/blockstore"
	hierarchical "github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	schema "github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	cid "github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

// CrossMsgMeta aggregates all the information related to crossMsgs that need to be persisted
type CrossMsgMeta struct {
	Msgs  []ltypes.Message      // Raw msgs from the subnet
	Metas []schema.CrossMsgMeta // Metas propagated from child subnets
}

// MetaTag is a convenient struct
// used to compute the Cid of the MsgMeta
type MetaTag struct {
	MsgsCid  cid.Cid
	MetasCid cid.Cid
}

// Cid computes the cid for the CrossMsgMeta
func (cm *CrossMsgMeta) Cid() (cid.Cid, error) {
	cst := cbor.NewCborStore(bstore.NewMemory())
	store := blockadt.WrapStore(context.TODO(), cst)
	cArr := blockadt.MakeEmptyArray(store)
	mArr := blockadt.MakeEmptyArray(store)

	// Compute CID for list of messages generated in subnet
	for i, m := range cm.Msgs {
		c := cbg.CborCid(m.Cid())
		if err := cArr.Set(uint64(i), &c); err != nil {
			return cid.Undef, err
		}
	}

	// Compute Cid for msgsMeta propagated from child subnets.
	for i, m := range cm.Metas {
		// NOTE: Instead of using the metaCID to compute CID of msgMeta
		// we use from/to to de-duplicate between Cids of different msgMeta.
		// This may be deemed unecessary, but it is a sanity-check for the case
		// where a subnet may try to push the same Cid of MsgMeta of other subnets
		// and thus remove previously stored msgMetas.
		// _, mc, err := cid.CidFromBytes(m.MsgsCid)
		// if err != nil {
		// 	return cid.Undef, err
		// }
		mc, err := abi.CidBuilder.Sum([]byte(string(m.MsgsCid) + m.From + m.To))
		if err != nil {
			return cid.Undef, err
		}
		c := cbg.CborCid(mc)
		if err := mArr.Set(uint64(i), &c); err != nil {
			return cid.Undef, err
		}
	}

	croot, err := cArr.Root()
	if err != nil {
		return cid.Undef, err
	}
	mroot, err := mArr.Root()
	if err != nil {
		return cid.Undef, err
	}

	return store.Put(store.Context(), &MetaTag{
		MsgsCid:  croot,
		MetasCid: mroot,
	})
}

// AddMsg adds a the Cid of a new message to MsgMeta
func (cm *CrossMsgMeta) AddMsg(msg ltypes.Message) {
	cm.Msgs = append(cm.Msgs, msg)
}

func (cm *CrossMsgMeta) hasEqualMeta(meta *schema.CrossMsgMeta) bool {
	for _, m := range cm.Metas {
		if m.Equal(meta) {
			return true
		}
	}
	return false
}

// AddMetas adds a list of MsgMetas from child subnets to the CrossMsgMeta
func (cm *CrossMsgMeta) AddMetas(metas []schema.CrossMsgMeta) {
	for _, m := range metas {
		// If the same meta is already there don't include it.
		if cm.hasEqualMeta(&m) {
			continue
		}
		cm.Metas = append(cm.Metas, m)
	}
}

// AddMsgMeta adds a the Cid of a msgMeta from a child subnet
// to aggregate it and propagated in the checkpoint
func (cm *CrossMsgMeta) AddMsgMeta(from, to hierarchical.SubnetID, meta schema.CrossMsgMeta) {
	cm.Metas = append(cm.Metas, meta)
}

func (st *SCAState) releaseMsg(rt runtime.Runtime, value big.Int) {
	// The way we identify it is a release message from the subnet is by
	// setting the burntFundsActor as the from of the message
	// See hierarchical/types.go
	source := builtin.BurntFundsActorAddr

	// Build message.
	msg := ltypes.Message{
		To:         rt.Caller(),
		From:       source,
		Value:      value,
		Nonce:      st.Nonce,
		GasLimit:   1 << 30, // This is will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}

	// Store msg in registry, update msgMeta and include in checkpoint
	//
	// It is a releaseMsg so the source is the current chain and the
	// destination is our parent chain.
	st.storeCheckMsg(rt, msg, st.NetworkName, st.NetworkName.Parent())

	// Increase nonce.
	incrementNonce(rt, &st.Nonce)
}

func (st *SCAState) storeDownTopMsgMeta(rt runtime.Runtime, meta schema.CrossMsgMeta) {
	meta.Nonce = int(st.DownTopNonce)
	crossMsgs, err := adt.AsArray(adt.AsStore(rt), st.DownTopMsgsMeta, CrossMsgsAMTBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cross-messages")
	// Set message in AMT
	err = crossMsgs.Set(uint64(meta.Nonce), &meta)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to store cross-messages")
	// Flush AMT
	st.DownTopMsgsMeta, err = crossMsgs.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush cross-messages")

	// Increase nonce.
	incrementNonce(rt, &st.DownTopNonce)
}

func (st *SCAState) GetTopDownMsg(s adt.Store, id hierarchical.SubnetID, nonce uint64) (*ltypes.Message, bool, error) {
	sh, found, err := st.GetSubnet(s, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, xerrors.Errorf("subnet not registered in hierarchical consensus")
	}
	return sh.GetTopDownMsg(s, nonce)
}

func (st *SCAState) GetDownTopMsgMeta(s adt.Store, nonce uint64) (*schema.CrossMsgMeta, bool, error) {
	crossMsgs, err := adt.AsArray(s, st.DownTopMsgsMeta, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load cross-msgs: %w", err)
	}
	return getDownTopMsgMeta(crossMsgs, nonce)
}

func getDownTopMsgMeta(crossMsgs *adt.Array, nonce uint64) (*schema.CrossMsgMeta, bool, error) {
	if nonce > MaxNonce {
		return nil, false, xerrors.Errorf("maximum cross-message nonce is 2^63-1")
	}
	var out schema.CrossMsgMeta
	found, err := crossMsgs.Get(nonce, &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get cross-msg with nonce %v: %w", nonce, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

// Using this approach to increment nonce to avoid code repetition.
// We could probably do better and be more efficient if we had generics.
func incrementNonce(rt runtime.Runtime, nonceCounter *uint64) {
	// Increment nonce.
	(*nonceCounter)++

	// If overflow we restart from zero.
	if *nonceCounter > MaxNonce {
		// FIXME: This won't be a problem in the short-term, but we should handle this.
		// We could maybe use a snapshot or paging approach so new peers can sync
		// from scratch while restarting the nonce for cross-message for subnets to zero.
		// sh.Nonce = 0
		rt.Abortf(exitcode.ErrIllegalState, "nonce overflow not supported yet")
	}
}

func (st *SCAState) aggChildMsgMeta(rt runtime.Runtime, ch *schema.Checkpoint, aux map[string][]schema.CrossMsgMeta) {
	for to, mm := range aux {
		// Get the cid of MsgMeta from this subnet (if any)
		metaIndex, msgMeta := ch.CrossMsgMeta(st.NetworkName, hierarchical.SubnetID(to))
		if msgMeta == nil {
			msgMeta = schema.NewCrossMsgMeta(st.NetworkName, hierarchical.SubnetID(to))
		}

		// If there is already a msgMeta for that to/from update with new message
		if len(msgMeta.MsgsCid) != 0 {
			_, prevMetaCid, err := cid.CidFromBytes(msgMeta.MsgsCid)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to compute Cid for msgMeta")
			metaCid := st.appendMetasToMeta(rt, prevMetaCid, mm)
			// Update msgMeta in checkpoint
			ch.SetMsgMetaCid(metaIndex, metaCid)
		} else {
			// if not populate a new one
			meta := &CrossMsgMeta{Metas: mm}
			msgMetas, err := adt.AsMap(adt.AsStore(rt), st.CheckMsgsMetaRegistry, builtin.DefaultHamtBitwidth)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load msgMeta registry")
			metaCid, err := putMsgMeta(msgMetas, meta)
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put updates MsgMeta in registry")
			// Flush registry
			st.CheckMsgsMetaRegistry, err = msgMetas.Root()
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush msgMeta registry")
			// Append msgMeta to registry
			msgMeta.MsgsCid = metaCid.Bytes()
			ch.AppendMsgMeta(msgMeta)
		}
	}
}

func (st *SCAState) storeCheckMsg(rt runtime.Runtime, msg ltypes.Message, from, to hierarchical.SubnetID) {
	// Get the checkpoint for the current window
	ch := st.currWindowCheckpoint(rt)
	// Get the cid of MsgMeta
	metaIndex, msgMeta := ch.CrossMsgMeta(from, to)
	if msgMeta == nil {
		msgMeta = schema.NewCrossMsgMeta(from, to)
	}

	// If there is already a msgMeta for that to/from update with new message
	if len(msgMeta.MsgsCid) != 0 {
		_, prevMetaCid, err := cid.CidFromBytes(msgMeta.MsgsCid)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to compute Cid for msgMeta")
		metaCid := st.appendMsgToMeta(rt, prevMetaCid, msg)
		// Update msgMeta in checkpoint
		ch.SetMsgMetaCid(metaIndex, metaCid)
	} else {
		// if not populate a new one
		meta := &CrossMsgMeta{Msgs: []ltypes.Message{msg}}
		msgMetas, err := adt.AsMap(adt.AsStore(rt), st.CheckMsgsMetaRegistry, builtin.DefaultHamtBitwidth)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load msgMeta registry")
		metaCid, err := putMsgMeta(msgMetas, meta)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put updates MsgMeta in registry")
		// Flush registry
		st.CheckMsgsMetaRegistry, err = msgMetas.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush msgMeta registry")
		// Append msgMeta to registry
		msgMeta.MsgsCid = metaCid.Bytes()
		ch.AppendMsgMeta(msgMeta)
	}

	st.flushCheckpoint(rt, ch)

}

func getMsgMeta(msgMetas *adt.Map, c cid.Cid) (*CrossMsgMeta, bool, error) {
	var out CrossMsgMeta
	found, err := msgMetas.Get(abi.CidKey(c), &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get crossMsgMeta from registry with cid %v: %w", c, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

// PutMsgMeta puts a new msgMeta in registry and returns the Cid of the MsgMeta
func putMsgMeta(msgMetas *adt.Map, meta *CrossMsgMeta) (cid.Cid, error) {
	metaCid, err := meta.Cid()
	if err != nil {
		return cid.Undef, err
	}
	return metaCid, msgMetas.Put(abi.CidKey(metaCid), meta)
}

// Puts meta in registry, deletes previous one, and flushes updated registry
func (st *SCAState) putDeleteFlushMeta(rt runtime.Runtime, msgMetas *adt.Map, prevMetaCid cid.Cid, meta *CrossMsgMeta) cid.Cid {
	// Put updated msgMeta
	metaCid, err := putMsgMeta(msgMetas, meta)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to put updates MsgMeta in registry")
	// Delete previous meta, is no longer needed
	err = msgMetas.Delete(abi.CidKey(prevMetaCid))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to delete previous MsgMeta from registry")
	// Flush registry
	st.CheckMsgsMetaRegistry, err = msgMetas.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush msgMeta registry")
	return metaCid
}

// appendMsgToMsgMeta appends the message to MsgMeta in the registry and returns the updated Cid.
func (st *SCAState) appendMsgToMeta(rt runtime.Runtime, prevMetaCid cid.Cid, msg ltypes.Message) cid.Cid {
	msgMetas, err := adt.AsMap(adt.AsStore(rt), st.CheckMsgsMetaRegistry, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load msgMeta registry")

	// Get previous meta
	meta, found, err := getMsgMeta(msgMetas, prevMetaCid)
	if !found || err != nil {
		rt.Abortf(exitcode.ErrIllegalState, "error fetching meta by cid or not found: err=%v", err)
	}
	// Add new msg to meta
	meta.AddMsg(msg)
	return st.putDeleteFlushMeta(rt, msgMetas, prevMetaCid, meta)
}

// appendMsgToMsgMeta appends the message to MsgMeta in the registry and returns the updated Cid.
func (st *SCAState) appendMetasToMeta(rt runtime.Runtime, prevMetaCid cid.Cid, metas []schema.CrossMsgMeta) cid.Cid {
	msgMetas, err := adt.AsMap(adt.AsStore(rt), st.CheckMsgsMetaRegistry, builtin.DefaultHamtBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load msgMeta registry")

	// Get previous meta
	meta, found, err := getMsgMeta(msgMetas, prevMetaCid)
	if !found || err != nil {
		rt.Abortf(exitcode.ErrIllegalState, "error fetching meta by cid or not found: err=%v", err)
	}
	// Add new msg to meta
	meta.AddMetas(metas)
	// If the Cid hasn't change return without persisting
	mcid, err := meta.Cid()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to compute Cid")
	if mcid == prevMetaCid {
		return mcid
	}
	// FIXME: We can prevent one computation of metaCid here by adding mcid as
	// an argument in this function.
	return st.putDeleteFlushMeta(rt, msgMetas, prevMetaCid, meta)
}