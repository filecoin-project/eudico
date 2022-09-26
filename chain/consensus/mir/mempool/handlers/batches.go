package handlers

import (
	"github.com/filecoin-project/lotus/chain/consensus/mir/mempool/types"
	"github.com/filecoin-project/mir/pkg/dsl"
	mpdsl "github.com/filecoin-project/mir/pkg/mempool/dsl"
	"github.com/filecoin-project/mir/pkg/pb/mempoolpb"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
)

type requestTxIDsContext struct {
	txs    []*requestpb.Request
	origin *mempoolpb.RequestBatchOrigin
}

// IncludeBatchCreation registers event handlers for processing NewRequests and RequestBatch events.
func IncludeBatchCreation(
	m dsl.Module,
	mc *types.ModuleConfig,
	params *types.ModuleParams,
	commonState *types.State,
) {
	mpdsl.UponTransactionIDsResponse(m, func(txIDs []t.TxID, context *requestTxIDsContext) error {
		var txs []*requestpb.Request
		txCount := 0
		for i := range txIDs {
			tx := context.txs[i]
			commonState.TxByID[txIDs[i]] = tx

			// TODO: add other limitations (if any) here.
			if txCount == params.MaxTransactionsInBatch {
				break
			}

			txs = append(txs, tx)
			txCount++
		}

		mpdsl.NewBatch(m, t.ModuleID(context.origin.Module), txIDs, txs, context.origin)
		return nil
	})

	mpdsl.UponRequestBatch(m, func(origin *mempoolpb.RequestBatchOrigin) error {
		submitChan := make(chan []*requestpb.Request)

		commonState.DescriptorChan <- types.Descriptor{
			Limit:      0,
			SubmitChan: submitChan,
		}

		receivedRequests := <-submitChan
		mpdsl.RequestTransactionIDs(m, mc.Self, receivedRequests, &requestTxIDsContext{receivedRequests, origin})

		return nil
	})
}
