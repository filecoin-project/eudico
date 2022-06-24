package sca

import (
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"
)

type Subnet struct {
	ID          address.SubnetID // human-readable name of the subnet ID (path in the hierarchy)
	ParentID    address.SubnetID
	Stake       abi.TokenAmount
	TopDownMsgs cid.Cid // AMT[ltypes.Messages] of cross top-down messages to subnet.
	// NOTE: We can avoid explicitly storing the Nonce here and use CrossMsgs length
	// to determine the nonce. Deferring that for future iterations.
	Nonce      uint64          // Latest nonce of cross message submitted to subnet.
	CircSupply abi.TokenAmount // Circulating supply of FIL in subnet.
	Status     Status
	// NOTE: We could probably save some gas here without affecting the
	// overall behavior of check commitment by just keeping the information
	// required for verification (prevCheck cid and epoch).
	PrevCheckpoint schema.Checkpoint
}

type SubnetOutput struct {
	Subnet    Subnet
	Consensus hierarchical.ConsensusType
}

// addStake adds new funds to the stake of the subnet.
//
// This function also accepts negative values to subtract, and checks
// if the funds are enough for the subnet to be active.
func (sh *Subnet) addStake(rt runtime.Runtime, st *SCAState, value abi.TokenAmount) {
	// Add stake to the subnet
	sh.Stake = big.Add(sh.Stake, value)

	// Check if subnet has still stake to be active
	if sh.Stake.LessThan(st.MinStake) {
		sh.Status = Inactive
	}

	// Flush subnet into subnetMap
	st.flushSubnet(rt, sh)

}

func fundMsg(rt runtime.Runtime, id address.SubnetID, secp address.Address, value big.Int) ltypes.Message {
	// Transform To and From to HAddresses
	to, err := address.NewHCAddress(id, secp)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")
	from, err := address.NewHCAddress(id.Parent(), secp)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to create HAddress")

	// Build message.
	//
	// Fund messages include the same to and from.
	return ltypes.Message{
		To:         to,
		From:       from,
		Value:      value,
		Method:     builtin.MethodSend,
		GasLimit:   1 << 30, // This will be applied as an implicit msg, add enough gas
		GasFeeCap:  ltypes.NewInt(0),
		GasPremium: ltypes.NewInt(0),
		Params:     nil,
	}
}

func (sh *Subnet) storeTopDownMsg(rt runtime.Runtime, msg *ltypes.Message) {
	crossMsgs, err := adt.AsArray(adt.AsStore(rt), sh.TopDownMsgs, CrossMsgsAMTBitwidth)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load cross-messages")
	// Set message in AMT
	err = crossMsgs.Set(msg.Nonce, msg)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to store cross-messages")
	// Flush AMT
	sh.TopDownMsgs, err = crossMsgs.Root()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush cross-messages")

}

func (sh *Subnet) GetTopDownMsg(s adt.Store, nonce uint64) (*ltypes.Message, bool, error) {
	crossMsgs, err := adt.AsArray(s, sh.TopDownMsgs, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to load cross-msgs: %w", err)
	}
	return getTopDownMsg(crossMsgs, nonce)
}

func getTopDownMsg(crossMsgs *adt.Array, nonce uint64) (*ltypes.Message, bool, error) {
	if nonce > MaxNonce {
		return nil, false, xerrors.Errorf("maximum cross-message nonce is 2^63-1")
	}
	var out ltypes.Message
	found, err := crossMsgs.Get(nonce, &out)
	if err != nil {
		return nil, false, xerrors.Errorf("failed to get cross-msg with nonce %v: %w", nonce, err)
	}
	if !found {
		return nil, false, nil
	}
	return &out, true, nil
}

// TopDownMsgFromNonce gets the latest topDownMessages from a specific nonce
// (including the one specified, i.e. [nonce, latest], both limits
// included).
func (sh *Subnet) TopDownMsgFromNonce(s adt.Store, nonce uint64) ([]*ltypes.Message, error) {
	crossMsgs, err := adt.AsArray(s, sh.TopDownMsgs, CrossMsgsAMTBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to load cross-msgs: %w", err)
	}
	// FIXME: Consider setting the length of the slice in advance
	// to improve performance.
	out := make([]*ltypes.Message, 0)
	for i := nonce; i < sh.Nonce; i++ {
		msg, found, err := getTopDownMsg(crossMsgs, i)
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, msg)
		}
	}
	return out, nil
}
