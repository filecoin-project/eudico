package api

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

type EudicoStats interface {
	Listen(ctx context.Context, id address.SubnetID, epochThreshold abi.ChainEpoch, observerConfigs map[string]string) error // perm:write
}
