package sca

import (
	address "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	rtt "github.com/filecoin-project/go-state-types/rt"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
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

func applyFund(rt runtime.Runtime, msg types.Message) {
	var st SCAState
	rfrom, rto := fromToRawAddr(rt, msg.From, msg.To)
	// Check that msg.From == msg.To (this is what determines that it is a funding message).
	if rfrom != rto {
		rt.Abortf(exitcode.ErrIllegalArgument, "msg passed as argument not a funding message (msg != from)")
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

	params := &reward.FundingParams{
		Addr:  rto,
		Value: msg.Value,
	}
	code := rt.Send(reward.RewardActorAddr, reward.Methods.ExternalFunding, params, big.Zero(), &builtin.Discard{})
	if !code.IsSuccess() {
		noop(rt, code)
	}
}

func applyRelease(rt runtime.Runtime, msg types.Message) {
	var st SCAState

	rfrom, rto := fromToRawAddr(rt, msg.From, msg.To)
	snFrom, err := msg.From.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting subnet from HAddress")

	// Check that msg.From == BurntRewardsActorAddr, this determines that is a release message
	if rfrom != builtin.BurntFundsActorAddr {
		rt.Abortf(exitcode.ErrIllegalArgument, "msg passed as argument not a funding message (from != burtnRewardActorAddr)")
	}

	rt.StateTransaction(&st, func() {
		bottomUpStateTransition(rt, &st, msg)

		// Update circulating supply reducing release value.
		sh, has, err := st.GetSubnet(adt.AsStore(rt), snFrom)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
		if !has {
			rt.Abortf(exitcode.ErrIllegalState, "subnet for actor hasn't been registered yet")
		}
		// Even if the actor has balance, we shouldn't allow releasing more than
		// the current circulating supply, as it would mean that we are
		// releasing funds from the collateral.
		if sh.CircSupply.LessThan(msg.Value) {
			rt.Abortf(exitcode.ErrIllegalState, "wtf! we can't release funds below the circulating supply. Something went wrong!")
		}
		sh.CircSupply = big.Sub(sh.CircSupply, msg.Value)
		st.flushSubnet(rt, sh)
	})

	// Release funds to the destination address.
	code := rt.Send(rto, builtin.MethodSend, nil, msg.Value, &builtin.Discard{})
	if !code.IsSuccess() {
		noop(rt, code)
	}
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
