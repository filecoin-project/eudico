package types

import "github.com/ipfs/go-cid"

type FullBlock struct {
	Header        *BlockHeader
	BlsMessages   []*Message
	SecpkMessages []*SignedMessage
	CrossMessages []*Message
}

func (fb *FullBlock) Cid() cid.Cid {
	return fb.Header.Cid()
}
