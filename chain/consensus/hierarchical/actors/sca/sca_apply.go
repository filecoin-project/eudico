package sca

import (
	"bytes"
	"github.com/filecoin-project/lotus/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	rtt "github.com/filecoin-project/go-state-types/rt"
	"github.com/filecoin-project/specs-actors/v7/actors/builtin"
	"github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"

	"github.com/filecoin-project/lotus/chain/consensus/actors/reward"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/types"
)

func fromToRawAddr(rt runtime.Runtime, from, to address.Address) (address.Address, address.Address) {
	var err error
	from, err = from.RawAddr()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get raw address from HCAddress")
	to, err = to.RawAddr()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get raw address from HCAddress")
	return from, to
}

func applyTopDown(rt runtime.Runtime, msg types.Message) {
	var st SCAState
	_, rto := fromToRawAddr(rt, msg.From, msg.To)
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get subnet from HCAddress")

	rt.StateTransaction(&st, func() {
		if bu, err := hierarchical.ApplyAsBottomUp(st.NetworkName, &msg); bu {
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error checking type of message to be applied")
		}
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
	ret := requireSuccessWithNoop(rt, msg, code, "error applying bottomUp message")
	if ret {
		return
	}

	// If not directed to this subnet we need to go down.
	if sto != st.NetworkName {
		rt.StateTransaction(&st, func() {
			commitTopDownMsg(rt, &st, msg)
		})
	} else {
		// Send the cross-message
		// FIXME: Should we not discard the output for any reason?
		code = rt.SendWithSerializedParams(rto, msg.Method, msg.Params, msg.Value, &builtin.Discard{})
		requireSuccessWithNoop(rt, msg, code, "error applying bottomUp message")
		recordCrossNetMsgExecution(rt, msg, code, "TopDown")
	}
}

func applyBottomUp(rt runtime.Runtime, msg types.Message) {
	var st SCAState

	_, rto := fromToRawAddr(rt, msg.From, msg.To)
	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to get subnet from HCAddress")

	rt.StateTransaction(&st, func() {
		if bu, err := hierarchical.ApplyAsBottomUp(st.NetworkName, &msg); !bu {
			builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error checking type of message to be applied")
		}
		bottomUpStateTransition(rt, &st, msg)

		if sto != st.NetworkName {
			// If directed to a child we need to commit message as a
			// top-down transaction to propagate it down.
			commitTopDownMsg(rt, &st, msg)
		}
	})

	if sto == st.NetworkName {
		// Release funds to the destination address if it is directed to the current network.
		// FIXME: Should we not discard the output for any reason?
		code := rt.SendWithSerializedParams(rto, msg.Method, msg.Params, msg.Value, &builtin.Discard{})
		requireSuccessWithNoop(rt, msg, code, "error applying bottomUp message")
		recordCrossNetMsgExecution(rt, msg, code, "BottomUp")
	}
}

func (st *SCAState) releaseCircSupply(rt runtime.Runtime, curr *Subnet, id address.SubnetID, value abi.TokenAmount) {
	// For the current subnet, we don't need to get the subnet object again,
	// we can modify it directly.
	if curr.ID == id {
		curr.releaseSupply(rt, value)
		return
		// It is flushed somewhere else.
	}

	// Update circulating supply reducing release value.
	sh, has, err := st.GetSubnet(adt.AsStore(rt), id)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error fetching subnet state")
	if !has {
		rt.Abortf(exitcode.ErrIllegalState, "subnet for actor hasn't been registered yet")
	}
	sh.releaseSupply(rt, value)
	st.flushSubnet(rt, sh)
}

func (sh *Subnet) releaseSupply(rt runtime.Runtime, value abi.TokenAmount) {
	// Even if the actor has balance, we shouldn't allow releasing more than
	// the current circulating supply, as it would mean that we are
	// releasing funds from the collateral.
	if sh.CircSupply.LessThan(value) {
		rt.Abortf(exitcode.ErrIllegalState, "wtf! we can't release funds below the circulating supply. Something went wrong!")
	}
	sh.CircSupply = big.Sub(sh.CircSupply, value)
}

func bottomUpStateTransition(rt runtime.Runtime, st *SCAState, msg types.Message) {
	// Bottom-up messages include the nonce of their message meta. Several messages
	// will include the same nonce. They need to be applied in order of nonce.

	// As soon as we see a message with the next msgMeta nonce, we increment the nonce
	// and start accepting the one for the next nonce.
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

// ErrorParam wraps an error code to notify that the
// cross-messaged failed (at this point is not processed anywhere)
type ErrorParam struct {
	Code int64
}

func errorParam(rt runtime.Runtime, code exitcode.ExitCode) []byte {
	var buf bytes.Buffer
	p := &ErrorParam{Code: int64(code)}
	err := p.MarshalCBOR(&buf)
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error marshalling error params")
	return buf.Bytes()
}

// noop is triggered to notify when a crossMsg fails to be applied successfully.
func noop(rt runtime.Runtime, st *SCAState, msg types.Message, code exitcode.ExitCode, err error) {
	rt.Log(rtt.WARN, `cross-msg couldn't be applied. Failed with code: %v, error: %s. 
	A message will be sent to revert any state change performed by the cross-net message in its way here.`, code, err)
	msg.From, msg.To = msg.To, msg.From
	// Sending an errorParam that to give feedback to the source subnet about the error.
	msg.Params = errorParam(rt, code)

	sto, err := msg.To.Subnet()
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "error getting subnet from HCAddress")
	if hierarchical.IsBottomUp(st.NetworkName, sto) {
		commitBottomUpMsg(rt, st, msg)
		return
	}
	commitTopDownMsg(rt, st, msg)
}

func noopWithStateTransaction(rt runtime.Runtime, msg types.Message, code exitcode.ExitCode, err error) {
	var st SCAState
	// If the message is not well-formed and something fails when applying the mesasge,
	// we increase the nonce and send a cross-message back to the source to notify about
	// the error applying the message (and to revert all the state changes in the route traversed).
	rt.StateTransaction(&st, func() {
		noop(rt, &st, msg, code, err)
	})
}

func recordCrossNetMsgExecution(rt runtime.Runtime, msg types.Message, code exitcode.ExitCode, msgType string) {
	sFrom, err := msg.From.Subnet()
	if err != nil {
		return
	}

	sTo, _ := msg.To.Subnet() // no need to check error, should be checked by caller

	ctx, _ := tag.New(
		rt.Context(),
		tag.Upsert(metrics.SubnetTo, sTo.String()),
		tag.Upsert(metrics.SubnetFrom, sFrom.String()),
		tag.Upsert(metrics.CrossNetMsgType, msgType),
		tag.Upsert(metrics.CrossNetMsgMethod, msg.Method.String()),
		tag.Upsert(metrics.CrossNetMsgExeCode, code.String()),
	)

	stats.Record(ctx, metrics.SubnetCrossNetMsgExecutedCount.M(1))
}
