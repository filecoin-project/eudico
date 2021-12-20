package exchange

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/store"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("chainxchg")

const (
	// BlockSyncProtocolID is the protocol ID of the former blocksync protocol.
	// Deprecated.
	BlockSyncProtocolID = "/fil/sync/blk/0.0.1"

	// ChainExchangeProtocolID is the protocol ID of the chain exchange
	// protocol.
	ChainExchangeProtocolID = "/fil/chain/xchg/0.0.1"
)

// FIXME: Bumped from original 800 to this to accommodate `syncFork()`
//  use of `GetBlocks()`. It seems the expectation of that API is to
//  fetch any amount of blocks leaving it to the internal logic here
//  to partition and reassemble the requests if they go above the maximum.
//  (Also as a consequence of this temporarily removing the `const`
//   qualifier to avoid "const initializer [...] is not a constant" error.)
var MaxRequestLength = uint64(build.ForkLengthThreshold)

const (
	// Extracted constants from the code.
	// FIXME: Should be reviewed and confirmed.
	SuccessPeerTagValue = 25
	WriteReqDeadline    = 5 * time.Second
	ReadResDeadline     = WriteReqDeadline
	ReadResMinSpeed     = 50 << 10
	ShufflePeersPrefix  = 16
	WriteResDeadline    = 60 * time.Second
)

// FIXME: Rename. Make private.
type Request struct {
	// List of ordered CIDs comprising a `TipSetKey` from where to start
	// fetching backwards.
	// FIXME: Consider using `TipSetKey` now (introduced after the creation
	//  of this protocol) instead of converting back and forth.
	Head []cid.Cid
	// Number of block sets to fetch from `Head` (inclusive, should always
	// be in the range `[1, MaxRequestLength]`).
	Length uint64
	// Request options, see `Options` type for more details. Compressed
	// in a single `uint64` to save space.
	Options uint64
}

// `Request` processed and validated to query the tipsets needed.
type validatedRequest struct {
	head    types.TipSetKey
	length  uint64
	options *parsedOptions
}

// Request options. When fetching the chain segment we can fetch
// either block headers, messages, or both.
const (
	Headers = 1 << iota
	Messages
)

// Decompressed options into separate struct members for easy access
// during internal processing..
type parsedOptions struct {
	IncludeHeaders  bool
	IncludeMessages bool
}

func (options *parsedOptions) noOptionsSet() bool {
	return options.IncludeHeaders == false &&
		options.IncludeMessages == false
}

func parseOptions(optfield uint64) *parsedOptions {
	return &parsedOptions{
		IncludeHeaders:  optfield&(uint64(Headers)) != 0,
		IncludeMessages: optfield&(uint64(Messages)) != 0,
	}
}

// FIXME: Rename. Make private.
type Response struct {
	Status status
	// String that complements the error status when converting to an
	// internal error (see `statusToError()`).
	ErrorMessage string

	Chain []*BSTipSet
}

type status uint64

const (
	Ok status = 0
	// We could not fetch all blocks requested (but at least we returned
	// the `Head` requested). Not considered an error.
	Partial = 101

	// Errors
	NotFound      = 201
	GoAway        = 202
	InternalError = 203
	BadRequest    = 204
)

// Convert status to internal error.
func (res *Response) statusToError() error {
	switch res.Status {
	case Ok, Partial:
		return nil
		// FIXME: Consider if we want to not process `Partial` responses
		//  and return an error instead.
	case NotFound:
		return xerrors.Errorf("not found")
	case GoAway:
		return xerrors.Errorf("not handling 'go away' chainxchg responses yet")
	case InternalError:
		return xerrors.Errorf("block sync peer errored: %s", res.ErrorMessage)
	case BadRequest:
		return xerrors.Errorf("block sync request invalid: %s", res.ErrorMessage)
	default:
		return xerrors.Errorf("unrecognized response code: %d", res.Status)
	}
}

// FIXME: Rename.
type BSTipSet struct {
	// List of blocks belonging to a single tipset to which the
	// `CompactedMessages` are linked.
	Blocks   []*types.BlockHeader
	Messages *CompactedMessages
}

// All messages of a single tipset compacted together instead
// of grouped by block to save space, since there are normally
// many repeated messages per tipset in different blocks.
//
// `BlsIncludes`/`SecpkIncludes` matches `Bls`/`Secpk` messages
// to blocks in the tipsets with the format:
// `BlsIncludes[BI][MI]`
//  * BI: block index in the tipset.
//  * MI: message index in `Bls` list
//
// FIXME: The logic to decompress this structure should belong
//  to itself, not to the consumer.
type CompactedMessages struct {
	Bls         []*types.Message
	BlsIncludes [][]uint64

	Secpk         []*types.SignedMessage
	SecpkIncludes [][]uint64

	Cross         []*types.Message
	CrossIncludes [][]uint64
}

// OldCompactedMessages is as the serialized representation of
// previously validated blocks that didn't support the inclusion of
// cross-net messages inside the block.
//
// When a block without cross-messages is received through the wire,
// it is unmarshalled into and OldCompactedMessage that is afterwards
// translated into a CompactedMessage without cross-mesasges.
type OldCompactedMessages struct {
	Bls         []*types.Message
	BlsIncludes [][]uint64

	Secpk         []*types.SignedMessage
	SecpkIncludes [][]uint64
}

// NewCompactedMesasges is the serialized representation of a modern
// compactedMessages including cross-net messages. This is an auxiliary
// type used to ensure backward compatibility with previous versions of
// compactedMessages used in the protocol.
type NewCompactedMessages struct {
	Bls         []*types.Message
	BlsIncludes [][]uint64

	Secpk         []*types.SignedMessage
	SecpkIncludes [][]uint64

	Cross         []*types.Message
	CrossIncludes [][]uint64
}

func (t *CompactedMessages) MarshalCBOR(w io.Writer) error {
	// NOTE: I don't think its needed to added a handler here to determine
	// if to marshal a new or old CompactedMessages format. UnmarshalCBOR will
	// handle any format conveniently. However, if something breaks
	// in the chain exchange protocol bear in mind that we are marshalling new
	// CompactedMessages type here.
	bm := NewCompactedMessages{t.Bls, t.BlsIncludes, t.Secpk, t.SecpkIncludes, t.Cross, t.CrossIncludes}
	return bm.MarshalCBOR(w)
}

// isOldCompacted checks if a new or old version of compactedMesasges is being
// send in the reader.
func isOldCompacted(r io.Reader) (bool, io.Reader, error) {
	scratch := make([]byte, 1)
	n, err := r.Read(scratch[:1])
	if err != nil {
		return false, nil, err
	}
	if n != 1 {
		return false, nil, fmt.Errorf("failed to read a byte")
	}

	extra := scratch[0] & 0x1f
	u := io.MultiReader(bytes.NewReader(scratch[:1]), r)
	// The check is performed through the number of fields in the
	// type being sent.
	if extra != 4 {
		return false, u, err
	}
	return true, u, err
}

func (t *CompactedMessages) UnmarshalCBOR(r io.Reader) error {
	isOld, u, err := isOldCompacted(r)
	if err != nil {
		return err
	}
	if isOld {
		var obm OldCompactedMessages
		if err := obm.UnmarshalCBOR(u); err != nil {
			return err
		}
		// unwrapping old into CompactedMessages
		t.Bls = obm.Bls
		t.BlsIncludes = obm.BlsIncludes
		t.Secpk = obm.Secpk
		t.SecpkIncludes = obm.SecpkIncludes
		t.Cross = make([]*types.Message, 0)
		t.CrossIncludes = make([][]uint64, 0)
		return nil
	}

	var nbm NewCompactedMessages
	if err := nbm.UnmarshalCBOR(u); err != nil {
		return err
	}
	// unwrapping new into CompactedMessages
	t.Bls = nbm.Bls
	t.BlsIncludes = nbm.BlsIncludes
	t.Secpk = nbm.Secpk
	t.SecpkIncludes = nbm.SecpkIncludes
	t.Cross = nbm.Cross
	t.CrossIncludes = nbm.CrossIncludes

	return nil
}

// Response that has been validated according to the protocol
// and can be safely accessed.
type validatedResponse struct {
	tipsets []*types.TipSet
	// List of all messages per tipset (grouped by tipset,
	// not by block, hence a single index like `tipsets`).
	messages []*CompactedMessages
}

// Decompress messages and form full tipsets with them. The headers
// need to have been requested as well.
func (res *validatedResponse) toFullTipSets() []*store.FullTipSet {
	if len(res.tipsets) == 0 || len(res.tipsets) != len(res.messages) {
		// This decompression can only be done if both headers and
		// messages are returned in the response. (The second check
		// is already implied by the guarantees of `validatedResponse`,
		// added here just for completeness.)
		return nil
	}
	ftsList := make([]*store.FullTipSet, len(res.tipsets))
	for tipsetIdx := range res.tipsets {
		fts := &store.FullTipSet{} // FIXME: We should use the `NewFullTipSet` API.
		msgs := res.messages[tipsetIdx]
		for blockIdx, b := range res.tipsets[tipsetIdx].Blocks() {
			fb := &types.FullBlock{
				Header: b,
			}
			for _, mi := range msgs.BlsIncludes[blockIdx] {
				fb.BlsMessages = append(fb.BlsMessages, msgs.Bls[mi])
			}
			for _, mi := range msgs.SecpkIncludes[blockIdx] {
				fb.SecpkMessages = append(fb.SecpkMessages, msgs.Secpk[mi])
			}

			// Only validate tipset with cross-messages if any are present.
			if msgs.CrossIncludes != nil && len(msgs.CrossIncludes) > 0 {
				for _, mi := range msgs.CrossIncludes[blockIdx] {
					fb.CrossMessages = append(fb.CrossMessages, msgs.Cross[mi])
				}
			}

			fts.Blocks = append(fts.Blocks, fb)
		}
		ftsList[tipsetIdx] = fts
	}
	return ftsList
}
