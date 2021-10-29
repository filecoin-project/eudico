package sharding

import (
	"context"
	"reflect"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/actors/shard"
	"github.com/filecoin-project/lotus/chain/types"
	builtin "github.com/filecoin-project/specs-actors/v6/actors/builtin"
	adt "github.com/filecoin-project/specs-actors/v6/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
)

// Info included in diff structure.
type diffInfo struct {
	consensus shard.ConsensusType
	genesis   []byte
	isMiner   bool
	isRm      bool
}

// Checks if there's a new shard and if we should start listening to it.
func (s *ShardingSub) checkNewShard(ctx context.Context, outDiff map[string]diffInfo, cst *cbor.BasicIpldStore, oldSt, newSt shard.ShardState) error {
	if oldSt.TotalShards < newSt.TotalShards {
		log.Infow("New shard event detected")
		// Get shard maps
		newM, err := shards(adt.WrapStore(ctx, cst), newSt)
		if err != nil {
			return err
		}
		oldM, err := shards(adt.WrapStore(ctx, cst), oldSt)
		if err != nil {
			return err
		}

		// Get the id of new shards in a map.
		diff, err := newShards(oldM, newM)
		if err != nil {
			return err
		}

		// For each shard check if we should join
		for k := range diff {
			err := s.diffShards(ctx, outDiff, k, cst, oldSt, newSt)
			if err != nil {
				log.Errorf("error checking if I should join new shard: %s", err)
				return err
			}
		}
	}
	return nil
}

// Checks if there are new joiners or miners.
func (s *ShardingSub) checkShardChange(ctx context.Context, outDiff map[string]diffInfo, cst *cbor.BasicIpldStore, oldSt, newSt shard.ShardState) error {
	// Get shard maps
	newM, err := shards(adt.WrapStore(ctx, cst), newSt)
	if err != nil {
		return err
	}
	oldM, err := shards(adt.WrapStore(ctx, cst), oldSt)
	if err != nil {
		return err
	}
	chSh, err := changedShards(oldM, newM)
	if err != nil {
		return err
	}

	// If any shard has changed, or a shard has been removed.
	if len(chSh) > 0 || oldSt.TotalShards > newSt.TotalShards {
		log.Infow("Shard change event detected")
		left := map[cid.Cid]struct{}{}

		// If the number of shards is reduced.
		// Get the id of the shards that have left.
		// We invert the order of states because
		// we want to check if a shard left.
		if oldSt.TotalShards > newSt.TotalShards {
			left, err = newShards(newM, oldM)
			if err != nil {
				return err
			}
		}

		// For each shard that has been removed.
		for shid := range left {
			// Check if I previously was in the list of stakers.
			err = s.rmShards(ctx, outDiff, shid, cst, oldSt, newSt)
			if err != nil {
				log.Errorf("error checking if shard changed: %s", err)
				return err
			}

		}

		// For each shard that has changed
		for shid := range chSh {
			// First check if it is because I was added as a staker.
			err := s.diffShards(ctx, outDiff, shid, cst, oldSt, newSt)
			if err != nil {
				log.Errorf("error checking if shard changed: %s", err)
				return err
			}
			// Then check if it is beacuse I left the stakers list.
			err = s.rmShards(ctx, outDiff, shid, cst, oldSt, newSt)
			if err != nil {
				log.Errorf("error checking if shard changed: %s", err)
				return err
			}

		}
		return nil

	}
	return nil
}

func (s *ShardingSub) rmShards(ctx context.Context, outDiff map[string]diffInfo, shid cid.Cid, cst *cbor.BasicIpldStore, oldSt, newSt shard.ShardState) error {
	// Check if we've been removed from the list of stakers.
	//
	// We invert oldSt for newSt so we can also check if we were
	// stakers in the removed shards. The old state will have the shard
	// while in the new state the shard has been removed.
	in, err := s.addrInStakes(ctx, adt.WrapStore(ctx, cst), shid, newSt, oldSt)
	if err != nil {
		log.Errorf("Error checking states to see if peer in list of stakers: %s", err)
		return err
	}
	if in {
		outDiff[shid.String()] = diffInfo{
			isRm: true,
		}
	}
	return nil
}

func (s *ShardingSub) diffShards(ctx context.Context, outDiff map[string]diffInfo, shid cid.Cid, cst *cbor.BasicIpldStore, oldSt, newSt shard.ShardState) error {
	// Check if we are in the mining list.
	store := adt.WrapStore(ctx, cst)
	in, err := s.isMiner(ctx, store, shid, oldSt, newSt)
	if err != nil {
		log.Errorf("Error getting shards in old state: %s", err)
		return err
	}
	// Get genesis and consensus from shard
	sh, err := getShard(ctx, store, newSt, shid)
	if err != nil {
		log.Errorf("Error getting shards in new state: %s", err)
		return err
	}
	if in {
		outDiff[shid.String()] = diffInfo{
			isMiner:   true,
			genesis:   sh.Genesis,
			consensus: sh.Consensus,
		}
		// If we are in the list of miners we are also in the
		// list fo stakers, so we can move on to the next shard.
		return nil
	}

	// Check if we are in the list of stakers
	in, err = s.addrInStakes(ctx, adt.WrapStore(ctx, cst), shid, oldSt, newSt)
	if err != nil {
		log.Errorf("Error checking states to see if peer in list of stakers: %s", err)
		return err
	}
	if in {
		outDiff[shid.String()] = diffInfo{
			genesis:   sh.Genesis,
			consensus: sh.Consensus,
		}
	}
	return nil
}

// Checks if a shard has left or joined the list of shards.
func newShards(oldM *adt.Map, newM *adt.Map) (map[cid.Cid]struct{}, error) {
	diff := map[cid.Cid]struct{}{}
	var sh shard.Shard
	// TODO: Can we get the ID from the key instead of having
	// to load and get from the shard object?
	err := newM.ForEach(&sh, func(k string) error {
		diff[sh.ID] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = oldM.ForEach(&sh, func(k string) error {
		delete(diff, sh.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return diff, err
}

// Check if shard states have changed.
func changedShards(oldM *adt.Map, newM *adt.Map) (map[cid.Cid]*shard.Shard, error) {
	diff := make(map[cid.Cid]*shard.Shard)
	var sh shard.Shard

	err := newM.ForEach(&sh, func(k string) error {
		diff[sh.ID] = &sh
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = oldM.ForEach(&sh, func(k string) error {
		// If the shard is equal there are no changes.
		if reflect.DeepEqual(sh.ID, diff[sh.ID]) {
			delete(diff, sh.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return diff, err
}

func (s *ShardingSub) addrInStakes(ctx context.Context, store adt.Store, shID cid.Cid, oldSt, newSt shard.ShardState) (bool, error) {
	oldSh, oldHas, err := oldSt.GetShard(store, shID)
	if err != nil {
		return false, err
	}
	newSh, newHas, err := newSt.GetShard(store, shID)
	if err != nil {
		return false, err
	}
	if !newHas {
		return false, nil
	}

	wallAddrs, err := s.api.WalletAPI.WalletList(ctx)
	if err != nil {
		return false, err
	}
	newM, err := stakes(store, newSh)
	if err != nil {
		return false, err
	}
	for _, addr := range wallAddrs {
		addr, err := s.api.StateLookupID(ctx, addr, types.EmptyTSK)
		if err != nil {
			// Disregard errors here. We want to check if the
			// state changes, if we can't check this, well, we keep going!
			continue
		}
		if oldHas {
			oldM, err := stakes(store, oldSh)
			if err != nil {
				return false, err
			}
			_, has, err := shard.GetMinerState(newM, addr)
			if err != nil {
				return false, err
			}
			_, oldhas, err := shard.GetMinerState(oldM, addr)
			if err != nil {
				return false, err
			}
			// If we are in the new state but not in the previous one.
			if has && !oldhas {
				return true, nil
			}
		} else {
			_, has, err := shard.GetMinerState(newM, addr)
			if err != nil {
				return false, err
			}
			if has {
				return true, nil
			}
		}
	}
	return false, nil
}

func getShard(ctx context.Context, store adt.Store, st shard.ShardState, shid cid.Cid) (*shard.Shard, error) {
	sh, has, err := st.GetShard(store, shid)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, xerrors.New("no shard with specified shardID")
	}
	return sh, nil
}

func (s *ShardingSub) isMiner(ctx context.Context, store adt.Store, shID cid.Cid, oldSt, newSt shard.ShardState) (bool, error) {
	oldSh, oldHas, err := oldSt.GetShard(store, shID)
	if err != nil {
		return false, err
	}
	newSh, newHas, err := newSt.GetShard(store, shID)
	if err != nil {
		return false, err
	}
	if !newHas {
		return false, nil
	}

	wallAddrs, err := s.api.WalletAPI.WalletList(ctx)
	if err != nil {
		return false, err
	}
	newMineL := newSh.Miners
	for _, addr := range wallAddrs {
		addr, err := s.api.StateLookupID(ctx, addr, types.EmptyTSK)
		if err != nil {
			// Disregard errors here. We want to check if the
			// state changes, if we can't check this, well, we keep going!
			continue
		}
		if oldHas {
			oldMineL := oldSh.Miners
			has := containsAddr(addr, newMineL)
			oldhas := containsAddr(addr, oldMineL)
			// If we are in the new state but not in the previous one.
			if has && !oldhas {
				return true, nil
			}
		} else {
			has := containsAddr(addr, newMineL)
			if has {
				return true, nil
			}
		}
	}
	return false, nil
}

func containsAddr(addr address.Address, ls []address.Address) bool {
	for _, a := range ls {
		if a == addr {
			return true
		}
	}
	return false
}

// wraps a shard map.
func shards(s adt.Store, st shard.ShardState) (*adt.Map, error) {
	return adt.AsMap(s, st.Shards, builtin.DefaultHamtBitwidth)
}

// wraps a stake map.
func stakes(s adt.Store, sh *shard.Shard) (*adt.Map, error) {
	return adt.AsMap(s, sh.Stake, builtin.DefaultHamtBitwidth)
}
