package sca

import (
	address "github.com/filecoin-project/go-address"
	abi "github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	rtt "github.com/filecoin-project/go-state-types/rt"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	types "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
	"github.com/filecoin-project/specs-actors/v6/actors/util/adt"
)

func fromToRawAddr(rt runtime.Runtime, from, to address.Address) (address.Address, address.Address) {
	var err error
	from, err = from.RawAddr()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get raw address from HAddress")
	to, err = to.RawAddr()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get raw address from HAddress")
	return from, to
}

func applyTopDown(rt runtime.Runtime, msg types.Message) {
	var st SCAState
	_, rto := fromToRawAddr(rt, msg.From, msg.To)
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get subnet from HAddress")

	if hierarchical.GetMsgType(&msg) != hierarchical.TopDown {
		rt.Abortf(exitcode.ErrIllegalArgument, "msg passed as argument not topDown")
	}

	rt.StateTransaction(&st, func() {
		// NOTE: Check if the nonce of the message being applied is the subsequent one (we could relax a bit this
		// requirement, but it would mean that we need to determine how we want to handle gaps, and messages
		// being validated out-of-order).
		if st.AppliedTopDownNonce != msg.Nonce {
			rt.Abortf(exitcode.ErrIllegalState, "the message being applied doesn't hold the subsequent nonce (nonce=%d, applied=%d)",
				msg.Nonce, st.AppliedTopDownNonce)
		}

		// Increment latest nonce applied for topDown
		incrementNonce(rt, &st.AppliedTopDownNonce)
	})

	// Mint funds for SCA so it can direct them accordingly as part of the message.
	params := &reward.FundingParams{
		Addr:  hierarchical.SubnetCoordActorAddr,
		Value: msg.Value,
	}
	code := rt.Send(reward.RewardActorAddr, reward.Methods.ExternalFunding, params, big.Zero(), &builtin.Discard{})
	if !code.IsSuccess() {
		noop(rt, code)
		return
	}

	// If not directed to this subnet we need to go down.
	if sto != st.NetworkName {
		rt.StateTransaction(&st, func() {
			commitTopDownMsg(rt, &st, msg)
		})
	} else {
		// Send the cross-message
		// FIXME: We are currently discarding the output, this will change once we
		// support calling arbitrary actors. And we don't support params. We'll need a way
		// to support arbitrary calls.
		code = rt.Send(rto, msg.Method, nil, msg.Value, &builtin.Discard{})
		if !code.IsSuccess() {
			noop(rt, code)
		}
	}
}

func applyBottomUp(rt runtime.Runtime, msg types.Message) {
	var st SCAState

	_, rto := fromToRawAddr(rt, msg.From, msg.To)
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get subnet from HAddress")
	sFrom, err := msg.From.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting subnet from HAddress")

	if hierarchical.GetMsgType(&msg) != hierarchical.BottomUp {
		rt.Abortf(exitcode.ErrIllegalArgument, "msg passed as argument not bottomUp")
	}

	rt.StateTransaction(&st, func() {
		bottomUpStateTransition(rt, &st, msg)
		st.releaseCircSupply(rt, sFrom, msg.Value)

		if sto != st.NetworkName {
			// If directed to a child we need to commit message as a
			// top-down transaction to propagate it down.
			commitTopDownMsg(rt, &st, msg)
			return
		}
	})

	// Release funds to the destination address if it is directed to the current network.
	// FIXME: We currently don't support sending messages with arbitrary params. We should
	// support this.
	code := rt.Send(rto, msg.Method, nil, msg.Value, &builtin.Discard{})
	if !code.IsSuccess() {
		noop(rt, code)
	}
}

func (st *SCAState) releaseCircSupply(rt runtime.Runtime, id address.SubnetID, value abi.TokenAmount) {
	// Update circulating supply reducing release value.
	sh, has, err := st.GetSubnet(adt.AsStore(rt), id)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
	if !has {
		rt.Abortf(exitcode.ErrIllegalState, "subnet for actor hasn't been registered yet")
	}
	// Even if the actor has balance, we shouldn't allow releasing more than
	// the current circulating supply, as it would mean that we are
	// releasing funds from the collateral.
	if sh.CircSupply.LessThan(value) {
		rt.Abortf(exitcode.ErrIllegalState, "wtf! we can't release funds below the circulating supply. Something went wrong!")
	}
	sh.CircSupply = big.Sub(sh.CircSupply, value)
	st.flushSubnet(rt, sh)
}

func bottomUpStateTransition(rt runtime.Runtime, st *SCAState, msg types.Message) {
	// Bottom-up messages include the nonce of their message meta. Several messages
	// will include the same nonce. They need to be applied in order of nonce.

	// As soon as we see a message with the next msgMeta nonce, we increment the nonce
	// and start accepting the one for the next nonce.
	// FIXME: Once we have the end-to-end flow of cross-message this REALLY needs to
	// be revisited.
	if st.AppliedBottomUpNonce+1 == msg.Nonce {
		// Increment latest nonce applied for bottomup
		incrementNonce(rt, &st.AppliedBottomUpNonce)
	}

	// NOTE: Check if the nonce of the message being applied is the subsequent one (we could relax a bit this
	// requirement, but it would mean that we need to determine how we want to handle gaps, and messages
	// being validated out-of-order).
	if st.AppliedBottomUpNonce != msg.Nonce {
		rt.Abortf(exitcode.ErrIllegalState, "the message being applied doesn't hold the subsequent nonce (nonce=%d, applied=%d)",
			msg.Nonce, st.AppliedBottomUpNonce)
	}

}

// noop is triggered to notify when a crossMsg fails to be applied.
func noop(rt runtime.Runtime, code exitcode.ExitCode) {
	// rt.Abortf(exitcode.ErrIllegalState, "failed to apply crossMsg, code: %v", code)
	// NOTE: If the message is not well-formed and something fails when applying the mesasge,
	// instead of aborting (which could be harming the liveliness of the subnet consensus protocol, as there wouldn't
	// be a way of applying the top-down message and allowing the nonce sequence continue), we log the error
	// and seamlessly increment the nonce without triggering the state changes for the cross-msg. This may require
	// notifying the source subnet in some way so it may revert potential state changes in the cross-msg path.
	rt.Log(rtt.WARN, `cross-msg couldn't be applied. Failed with code: %v. 
	Some state changes in other subnet may need to be reverted`, code)
}
