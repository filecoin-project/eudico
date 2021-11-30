package schema

import (
	"bytes"
	"io"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	ltypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/bindnode"
	"github.com/ipld/go-ipld-prime/schema"
	"golang.org/x/xerrors"
)

// Linkproto is the default link prototype used for Checkpoints
// It uses the default CidBuilder for Filecoin (see abi)
var Linkproto = cidlink.LinkPrototype{
	Prefix: cid.Prefix{
		Version:  1,
		Codec:    abi.CidBuilder.GetCodec(),
		MhType:   abi.HashFunction,
		MhLength: 16,
	},
}

var (
	CheckpointSchema schema.Type
	// NoPreviousCheck is a work-around to avoid undefined CIDs,
	// that results in unexpected errors when marshalling.
	// This needs a fix in go-ipld-prime::bindnode
	NoPreviousCheck cid.Cid

	// EmptyCheckpoint is an empty checkpoint that can be Marshalled
	EmptyCheckpoint *Checkpoint
)

func init() {
	CheckpointSchema = initCheckpointSchema()
	var err error
	NoPreviousCheck, err = Linkproto.Sum([]byte("nil"))
	if err != nil {
		panic(err)
	}

	EmptyCheckpoint = &Checkpoint{
		Data: CheckData{
			Source:       "",
			Epoch:        0,
			PrevCheckCid: NoPreviousCheck.Bytes(),
		},
	}
}

// ChildCheck
type ChildCheck struct {
	Source string
	Checks []cid.Cid
}

// MsgTreeList is the list of trees with cross-shard messages
// to propagate to the rest of the hierarchy.
// TODO: This is still under development.
type MsgTreeList struct{}

// CheckData is the data included in a Checkpoint.
type CheckData struct {
	Source       string
	TipSet       []byte // NOTE: For simplicity we add TipSetKey. We could include full TipSet
	Epoch        int
	PrevCheckCid []byte // NOTE: To prevent the VM from interpreting it as a CID from the state
	// tree, we store the Cid for PrevCheckpoint as bytes
	Childs    []ChildCheck
	XShardMsg *MsgTreeList
}

// Checkpoint data structure
//
// - Data includes all the data for the checkpoint. The Cid of Data
// is what identifies a checkpoint uniquely.
// - Signature adds the signature from a miner. According to the verifier
// used for checkpoint this may be different things.
type Checkpoint struct {
	Data      CheckData
	Signature []byte
}

// initCheckpointType initializes the Checkpoint schema
func initCheckpointSchema() schema.Type {
	ts := schema.TypeSystem{}
	ts.Init()
	ts.Accumulate(schema.SpawnString("String"))
	ts.Accumulate(schema.SpawnInt("Int"))
	ts.Accumulate(schema.SpawnLink("Link"))
	ts.Accumulate(schema.SpawnBytes("Bytes"))

	ts.Accumulate(schema.SpawnStruct("ChildCheck",
		[]schema.StructField{
			schema.SpawnStructField("Source", "String", false, false),
			schema.SpawnStructField("Checks", "List_Link", false, false),
		},
		schema.SpawnStructRepresentationMap(map[string]string{}),
	))
	ts.Accumulate(schema.SpawnStruct("MsgTreeList",
		[]schema.StructField{},
		schema.SpawnStructRepresentationMap(map[string]string{}),
	))
	ts.Accumulate(schema.SpawnStruct("CheckData",
		[]schema.StructField{
			schema.SpawnStructField("Source", "String", false, false),
			schema.SpawnStructField("TipSet", "Bytes", false, false),
			schema.SpawnStructField("Epoch", "Int", false, false),
			schema.SpawnStructField("PrevCheckCid", "Bytes", false, false),
			schema.SpawnStructField("Childs", "List_ChildCheck", false, false),
			schema.SpawnStructField("XShardMsg", "MsgTreeList", false, true),
		},
		schema.SpawnStructRepresentationMap(nil),
	))
	ts.Accumulate(schema.SpawnStruct("Checkpoint",
		[]schema.StructField{
			schema.SpawnStructField("Data", "CheckData", false, false),
			schema.SpawnStructField("Signature", "Bytes", false, false),
		},
		schema.SpawnStructRepresentationMap(nil),
	))
	ts.Accumulate(schema.SpawnList("List_String", "String", false))
	ts.Accumulate(schema.SpawnList("List_Link", "Link", false))
	ts.Accumulate(schema.SpawnList("List_ChildCheck", "ChildCheck", false))

	return ts.TypeByName("Checkpoint")
}

// Dumb linksystem used to generate links
//
// This linksystem doesn't store anything, just computes the Cid
// for a node.
func noStoreLinkSystem() ipld.LinkSystem {
	lsys := cidlink.DefaultLinkSystem()
	lsys.StorageWriteOpener = func(lctx ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := bytes.NewBuffer(nil)
		return buf, func(lnk ipld.Link) error {
			return nil
		}, nil
	}
	return lsys
}

// NewRawCheckpoint creates a checkpoint template to populate by the user.
//
// This is the template returned by the SCA actor for the miners to include
// the corresponding information and sign before commitment.
func NewRawCheckpoint(source hierarchical.SubnetID, epoch abi.ChainEpoch) *Checkpoint {
	return &Checkpoint{
		Data: CheckData{
			Source:       source.String(),
			Epoch:        int(epoch),
			PrevCheckCid: NoPreviousCheck.Bytes(),
		},
	}

}

func (c *Checkpoint) IsEmpty() (bool, error) {
	return c.Equals(EmptyCheckpoint)
}

func (c *Checkpoint) SetPrevious(cid cid.Cid) {
	c.Data.PrevCheckCid = cid.Bytes()
}

func (c *Checkpoint) SetTipsetKey(ts ltypes.TipSetKey) {
	c.Data.TipSet = ts.Bytes()
}

func (c *Checkpoint) SetEpoch(ep abi.ChainEpoch) {
	c.Data.Epoch = int(ep)
}

func (c *Checkpoint) PreviousCheck() (cid.Cid, error) {
	_, cid, err := cid.CidFromBytes(c.Data.PrevCheckCid)
	return cid, err
}

func (c *Checkpoint) Source() hierarchical.SubnetID {
	return hierarchical.SubnetID(c.Data.Source)
}

func (c *Checkpoint) MarshalBinary() ([]byte, error) {
	node := bindnode.Wrap(c, CheckpointSchema)
	nodeRepr := node.Representation()
	var buf bytes.Buffer
	err := dagjson.Encode(nodeRepr, &buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Checkpoint) UnmarshalBinary(b []byte) error {
	// TODO: This could fix the need of NoPrevCheckpoint but it hasn't been implemented yet.
	// This returns `panic: TODO: schema.StructRepresentation_Map`
	// nb := bindnode.Prototype(c, CheckpointSchema).Representation().NewBuilder()
	nb := bindnode.Prototype(c, CheckpointSchema).NewBuilder()
	err := dagjson.Decode(nb, bytes.NewReader(b))
	if err != nil {
		return err
	}
	n := bindnode.Unwrap(nb.Build())

	ch, ok := n.(*Checkpoint)
	if !ok {
		return xerrors.Errorf("Unmarshalled node not of type Checkpoint")
	}
	*c = *ch
	return nil
}

func (c *Checkpoint) MarshalCBOR(w io.Writer) error {
	node := bindnode.Wrap(c, CheckpointSchema)
	nodeRepr := node.Representation()
	err := dagcbor.Encode(nodeRepr, w)
	if err != nil {
		return err
	}
	return nil
}

func (c *Checkpoint) UnmarshalCBOR(r io.Reader) error {
	nb := bindnode.Prototype(c, CheckpointSchema).NewBuilder()
	err := dagcbor.Decode(nb, r)
	if err != nil {
		return err
	}
	n := bindnode.Unwrap(nb.Build())

	ch, ok := n.(*Checkpoint)
	if !ok {
		return xerrors.Errorf("Unmarshalled node not of type CheckData")
	}
	*c = *ch
	return nil
}

func (c *Checkpoint) Equals(ch *Checkpoint) (bool, error) {
	c1, err := c.Cid()
	if err != nil {
		return false, err
	}
	c2, err := ch.Cid()
	if err != nil {
		return false, err
	}
	return c1 == c2, nil

}

// Cid returns the unique identifier for a checkpoint.
//
// It is computed by removing the signature from the checkpoint.
// The checkpoints are unique but miners need to include additional
// signature information.
func (c *Checkpoint) Cid() (cid.Cid, error) {
	// The Cid of a checkpoint is computed from the data.
	// The signature may differ according to the verifier used.
	ch := &Checkpoint{Data: c.Data}
	lsys := noStoreLinkSystem()
	lnk, err := lsys.ComputeLink(Linkproto, bindnode.Wrap(ch, CheckpointSchema))
	if err != nil {
		return cid.Undef, err
	}
	return lnk.(cidlink.Link).Cid, nil
}

// AddListChilds adds a list of child checkpoints into the checkpoint.
//
// The overwrite flag is set in AddChild so if a checkpoint for the
// source is encountered, the previous checkpoint is overwritten.
func (c *Checkpoint) AddListChilds(childs []*Checkpoint) {
	for _, ch := range childs {
		c.AddChild(ch)
	}
}

// AddChild adds a single child to the checkpoint
//
// If a child with the same Cid or the same epoch already
// exists, nothing is added.
func (c *Checkpoint) AddChild(ch *Checkpoint) error {
	chcid, err := ch.Cid()
	if err != nil {
		return err
	}
	ind := c.HasChildSource(ch.Source())
	if ind >= 0 {
		if ci := c.Data.Childs[ind].hasCheck(chcid); ci >= 0 {
			return xerrors.Errorf("source already has a checkpoint with that Cid")
		}
		c.Data.Childs[ind].Checks = append(c.Data.Childs[ind].Checks, chcid)
		return nil
	}
	chcc := ChildCheck{ch.Data.Source, []cid.Cid{chcid}}
	c.Data.Childs = append(c.Data.Childs, chcc)
	return nil
}

func (c *ChildCheck) hasCheck(cid cid.Cid) int {
	for i, ch := range c.Checks {
		if ch == cid {
			return i
		}
	}
	return -1
}

func (c *Checkpoint) HasChildSource(source hierarchical.SubnetID) int {
	for i, ch := range c.Data.Childs {
		if ch.Source == source.String() {
			return i
		}
	}
	return -1
}

func (c *Checkpoint) LenChilds() int {
	return len(c.Data.Childs)
}

func (c *Checkpoint) GetSourceChilds(source hierarchical.SubnetID) *ChildCheck {
	i := c.HasChildSource(source)
	return &c.GetChilds()[i]
}

func (c *Checkpoint) GetChilds() []ChildCheck {
	return c.Data.Childs
}

func (c *Checkpoint) Epoch() abi.ChainEpoch {
	return abi.ChainEpoch(c.Data.Epoch)
}
