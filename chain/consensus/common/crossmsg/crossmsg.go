package crossmsg

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/checkpoints/schema"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/subnet/resolver"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

var log = logging.Logger("crossmsg-val")

const crossMsgResolutionTimeout = 30 * time.Second

func CheckCrossMsg(ctx context.Context, r *resolver.Resolver, pstore, snstore blockadt.Store, parentSCA, snSCA *sca.SCAState, msg *types.Message) error {

	buApply, err := hierarchical.ApplyAsBottomUp(snSCA.NetworkName, msg)
	if err != nil {
		return xerrors.Errorf("error processing type to apply: %w", err)
	}
	if buApply {
		return checkBottomUpMsg(ctx, r, snstore, snSCA, msg)
	}

	// If the message needs to be applied top-down.
	// sanity-check: the root chain doesn't support topDown messages,
	// so return an error if parentSCA is nil and we are here.
	if parentSCA == nil {
		return xerrors.Errorf("root chains (id=%v) does not support topDown cross msgs", snSCA.NetworkName)
	}
	return checkTopDownMsg(pstore, parentSCA, snSCA, msg)
}

// checkTopDownMsg validates the topdown message.
// - It checks that the msg nonce is larger than AppliedBottomUpNonce in the subnet SCA
// - It checks that the msg meta has been committed.
// - It resolves messages for msg-meta and verifies that the corresponding mesasge is included
// as part of MsgMeta.
func checkBottomUpMsg(ctx context.Context, r *resolver.Resolver, snstore blockadt.Store, snSCA *sca.SCAState, msg *types.Message) error {
	// Check valid nonce in subnet where message is applied.
	if snSCA.AppliedBottomUpNonce != sca.MaxNonce && msg.Nonce < snSCA.AppliedBottomUpNonce {
		return xerrors.Errorf("bottomup msg nonce reuse in subnet (nonce=%v, applied=%v", msg.Nonce, snSCA.AppliedTopDownNonce)
	}

	// check bottomup meta has been committed for nonce in SCA
	comMeta, found, err := snSCA.GetBottomUpMsgMeta(snstore, msg.Nonce)
	if err != nil {
		return xerrors.Errorf("getting bottomup msgmeta: %w", err)
	}
	if !found {
		return xerrors.Errorf("No BottomUp meta found for nonce in SCA: %d", msg.Nonce)
	}

	// Wait to resolve bottom-up messages for meta
	c, err := comMeta.Cid()
	if err != nil {
		return err
	}
	// Adding a 30 seconds time out for block resolution.
	// FIXME: We may need to figure out what to do if we never find the msgs
	// to check.
	ctx, cancel := context.WithTimeout(ctx, crossMsgResolutionTimeout)
	defer cancel()
	out := r.WaitCrossMsgsResolved(ctx, c, address.SubnetID(comMeta.From))
	select {
	case <-ctx.Done():
		return xerrors.Errorf("context timeout")
	case err := <-out:
		if err != nil {
			return xerrors.Errorf("error fully resolving messages: %s", err)
		}
	}

	// Get cross-messages
	cross, found, err := r.ResolveCrossMsgs(ctx, c, address.SubnetID(comMeta.From))
	if err != nil {
		return xerrors.Errorf("Error resolving messages: %v", err)
	}
	// sanity-check, it should always be found
	if !found {
		return xerrors.Errorf("messages haven't been resolver: %v", err)
	}
	// Check if the message is included in the committed msgMeta.
	if !hasMsg(comMeta, msg, cross) {
		return xerrors.Errorf("message proposed no included in committed bottom-up msgMeta")
	}

	// NOTE: Any additional check required?
	return nil
}

func hasMsg(meta *schema.CrossMsgMeta, msg *types.Message, batch []types.Message) bool {
	for _, m := range batch {
		// Changing original nonce to that of the MsgMeta as done
		// by the crossPool.
		m.Nonce = uint64(meta.Nonce)
		if msg.Equals(&m) { //nolint
			return true
		}
	}
	return false
}

// checkTopDownMsg validates the topdown message.
// - It checks that the msg nonce is larger than AppliedBottomUpNonce in the subnet SCA
// Recall that applying crossMessages increases the AppliedNonce of the SCA in the subnet
// where the message is applied.
// - It checks that the cross-msg is committed in the sca of the parent chain
func checkTopDownMsg(pstore blockadt.Store, parentSCA, snSCA *sca.SCAState, msg *types.Message) error {
	// Check valid nonce in subnet where message is applied.
	if msg.Nonce < snSCA.AppliedTopDownNonce {
		return xerrors.Errorf("topDown msg nonce reuse in subnet (nonce=%v, applied=%v", msg.Nonce, snSCA.AppliedTopDownNonce)
	}

	// check the message for nonce is committed in sca.
	comMsg, found, err := parentSCA.GetTopDownMsg(pstore, snSCA.NetworkName, msg.Nonce)
	if err != nil {
		return xerrors.Errorf("getting topDown msgs: %w", err)
	}
	if !found {
		return xerrors.Errorf("No TopDownMsg found for nonce in parent SCA: %d", msg.Nonce)
	}

	if !comMsg.Equals(msg) {
		return xerrors.Errorf("Committed and proposed TopDownMsg for nonce %d not equal", msg.Nonce)
	}

	// NOTE: Any additional check required?
	return nil

}

func ApplyCrossMsg(ctx context.Context, vmi vm.Interface, submgr subnet.SubnetMgr,
	em stmgr.ExecMonitor, msg *types.Message,
	ts *types.TipSet) error {
	switch hierarchical.GetMsgType(msg) {
	case hierarchical.TopDown, hierarchical.BottomUp:
		// At this point, both messages are applied in the same way
		return applyMsg(ctx, vmi, em, msg, ts)
	}

	return xerrors.Errorf("Unknown cross-msg type")
}

func applyMsg(ctx context.Context, vmi vm.Interface, em stmgr.ExecMonitor,
	msg *types.Message, ts *types.TipSet) error {
	// Serialize params
	params := &sca.CrossMsgParams{
		Msg: *msg,
	}
	serparams, aerr := actors.SerializeParams(params)
	if aerr != nil {
		return xerrors.Errorf("failed serializing init actor params: %s", aerr)
	}
	apply := &types.Message{
		From:       builtin.SystemActorAddr,
		To:         hierarchical.SubnetCoordActorAddr,
		Nonce:      msg.Nonce,
		Value:      big.Zero(),
		GasFeeCap:  types.NewInt(0),
		GasPremium: types.NewInt(0),
		GasLimit:   1 << 30,
		Method:     sca.Methods.ApplyMessage,
		Params:     serparams,
	}

	// Before applying the message in subnet, if the destination
	// account hasn't been initialized, init the account actor.
	// TODO: When handling arbitrary cross-messages, we should check if
	// we need to trigger the state change in this subnet, if not we may not
	// need to do this.
	// NOTE: If we are still using the legacy-vm, we need to initialize
	// the account actor before sending funds or send messages to them.
	if os.Getenv("LOTUS_USE_FVM_EXPERIMENTAL") != "1" {
		rto, err := params.Msg.To.RawAddr()
		if err != nil {
			return err
		}
		st := vmi.(*vm.LegacyVM).StateTree()
		_, acterr := st.GetActor(rto)
		if acterr != nil {
			log.Debugw("Initializing To address for crossmsg", "address", rto)
			_, _, err := vmi.(*vm.LegacyVM).CreateAccountActor(ctx, apply, rto)
			if err != nil {
				return xerrors.Errorf("failed to initialize address for crossmsg: %w", err)
			}
		}
	}

	ret, actErr := vmi.ApplyImplicitMessage(ctx, apply)
	if actErr != nil {
		return xerrors.Errorf("failed to apply cross message :%w", actErr)
	}
	if em != nil {
		if err := em.MessageApplied(ctx, ts, apply.Cid(), apply, ret, true); err != nil {
			return xerrors.Errorf("callback failed on reward message: %w", err)
		}
	}

	if ret.ExitCode != 0 {
		return xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
	}
	log.Debugw("Applied cross msg implicitly (original msg Cid)", "cid", msg.Cid())
	return nil
}

func GetSCAState(ctx context.Context, sm *stmgr.StateManager, submgr subnet.SubnetMgr, id address.SubnetID, ts *types.TipSet) (*sca.SCAState, blockadt.Store, error) {

	var st sca.SCAState
	// if submgr == nil we are in root, so we can load the actor using the state manager.
	if submgr == nil {
		// Getting SCA state for the base tipset being checked/validated in the current chain
		subnetAct, err := sm.LoadActor(ctx, hierarchical.SubnetCoordActorAddr, ts)
		if err != nil {
			return nil, nil, xerrors.Errorf("loading actor state: %w", err)
		}
		if err := sm.ChainStore().ActorStore(ctx).Get(ctx, subnetAct.Head, &st); err != nil {
			return nil, nil, xerrors.Errorf("getting actor state: %w", err)
		}
		return &st, sm.ChainStore().ActorStore(ctx), nil
	}

	// For subnets getting SCA state for the current baseTs is worthless.
	// We get it the standard way.
	return submgr.GetSCAState(ctx, id)
}

func SortCrossMsgs(ctx context.Context, sm *stmgr.StateManager, r *resolver.Resolver, msgs []types.ChainMsg, ts *types.TipSet) ([]*types.Message, error) {
	buApply := map[uint64][]*types.Message{}
	out := make([]*types.Message, len(msgs))

	// Get messages that require sorting and organize them by duplicate nonce
	i := 0
	for _, cm := range msgs {
		m := cm.VMMessage()

		netName, err := stmgr.GetNetworkName(ctx, sm, ts.ParentState())
		if err != nil {
			return []*types.Message{}, xerrors.Errorf("error getting network name: %w", err)
		}
		isBu, err := hierarchical.ApplyAsBottomUp(address.SubnetID(netName), m)
		if err != nil {
			return []*types.Message{}, xerrors.Errorf("error processing type to apply: %w", err)
		}

		// Bottom-up messages are the ones that require exhaustive ordering
		// top-down already come in order of nonce.
		if isBu {
			_, ok := buApply[m.Nonce]
			if !ok {
				buApply[m.Nonce] = make([]*types.Message, 0)
			}
			buApply[m.Nonce] = append(buApply[m.Nonce], m)
		} else {
			// Append top-down messages as they can be ordered directly.
			out[i] = m
			i++
		}
	}

	// Sort meta nonces
	j := 0
	metaNonces := make(NonceArray, len(buApply))
	for k := range buApply {
		metaNonces[j] = k
		j++
	}
	sort.Sort(metaNonces)

	// GetSCA to get bottomUp messages for nonce. We don't need
	// subnet-specific information here.
	sca, store, err := GetSCAState(ctx, sm, nil, address.UndefSubnetID, ts)
	if err != nil {
		return []*types.Message{}, err
	}
	// For each meta nonce, get all messages and sort them
	// by nonce.
	for _, n := range metaNonces {
		mabu, err := sortByOriginalNonce(ctx, r, n, sca, store, buApply[n])
		if err != nil {
			return []*types.Message{}, err
		}
		copy(out[i:], mabu)
		i += len(mabu)
	}
	return out, nil

}
