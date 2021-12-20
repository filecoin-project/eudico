package sca

import (
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
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
		// Check if the nonce of the message being applied is the subsequent one (we could relax a bit this
		// requirement, but it would mean that we need to determine how we want to handle gaps, and messages
		// being validated out-of-order).
		if st.AppliedTopDownNonce != msg.Nonce {
			rt.Abortf(exitcode.ErrIllegalState, "the message being applied doesn't hold the subsequent nonce")
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
		rt.Abortf(exitcode.ErrIllegalState,
			"failed to send unsent reward to the burnt funds actor, code: %v", code)
	}
}
