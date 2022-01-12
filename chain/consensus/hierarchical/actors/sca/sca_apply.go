package sca

import (
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	rtt "github.com/filecoin-project/go-state-types/rt"
	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	types "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/v6/actors/builtin"
	"github.com/filecoin-project/specs-actors/v6/actors/runtime"
)

func applyFund(rt runtime.Runtime, msg types.Message) {
	var st SCAState

	// Check that msg.From == msg.To (this is what determines that it is a funding message.
	if msg.To != msg.From {
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
		Addr:  msg.To,
		Value: msg.Value,
	}
	code := rt.Send(reward.RewardActorAddr, reward.Methods.ExternalFunding, params, big.Zero(), &builtin.Discard{})
	if !code.IsSuccess() {
		noop(rt, code)
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
