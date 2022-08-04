package mir

import (
	"context"
	"fmt"
	"os"

	"github.com/multiformats/go-multiaddr"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	t "github.com/filecoin-project/mir/pkg/types"
)

func newMirID(subnet, addr string) string {
	return fmt.Sprintf("%s:%s", subnet, addr)
}

// getSubnetValidators retrieves subnet validators from the environment variable or from the state.

// NOTE: You must unset the environment variable in tests if you use Mir in the rootnet and in a subnet.
// TODO: Implement a sophisticated way to separate getting validator set via environment variable and subnet actor.
// Should we support passing validators via the environment variable?
func getSubnetValidators(
	ctx context.Context,
	subnetID address.SubnetID,
	api v1api.FullNode,
) (
	*hierarchical.ValidatorSet, error) {
	var err error
	var validators []hierarchical.Validator
	validatorsEnv := os.Getenv(ValidatorsEnv)
	if validatorsEnv != "" {
		validators, err = hierarchical.ValidatorsFromString(validatorsEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to get validators from string: %w", err)
		}
	} else {
		if subnetID == address.RootSubnet {
			return nil, fmt.Errorf("can't be run in rootnet without validators")
		}
		validators, err = api.SubnetStateGetValidators(ctx, subnetID)
		if err != nil {
			return nil, fmt.Errorf("failed to get validators from state")
		}
	}
	return hierarchical.NewValidatorSet(validators), nil
}

// ValidatorsMembership validates that validators addresses are valid multi-addresses and
// returns all validators IDs and map between IDs and multi-addresses.
func ValidatorsMembership(validators []hierarchical.Validator) ([]t.NodeID, map[t.NodeID]t.NodeAddress, error) {
	var nodeIDs []t.NodeID
	nodeAddrs := make(map[t.NodeID]t.NodeAddress)

	for _, v := range validators {
		id := t.NodeID(v.ID())
		a, err := multiaddr.NewMultiaddr(v.NetAddr)
		if err != nil {
			return nil, nil, err
		}
		nodeIDs = append(nodeIDs, id)
		nodeAddrs[id] = a
	}

	return nodeIDs, nodeAddrs, nil
}
