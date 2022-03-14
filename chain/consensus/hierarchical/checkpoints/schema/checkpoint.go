package schema

import (
	"bytes"
	"io"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/node/bindnode"
	"github.com/ipld/go-ipld-prime/schema"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/types"
)

// Linkproto is the default link prototype used for Checkpoints
// It uses the default CidBuilder for Filecoin (see abi)
//
// NOTE: Maybe we should consider using another CID proto
// for checkpoints so they can be identified uniquely.
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
	MsgMetaSchema    schema.Type

	// NoPreviousCheck is a work-around to avoid undefined CIDs,
	// that results in unexpected errors when marshalling.
	NoPreviousCheck cid.Cid

	// EmptyCheckpoint is an empty checkpoint that can be Marshalled
	EmptyCheckpoint *Checkpoint
)

func init() {
	CheckpointSchema = initCheckpointSchema()
	MsgMetaSchema = initCrossMsgMetaSchema()
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
	// NOTE: Same problem as below, checks is
	// []cid.Cid, but we are hiding it behind a bunch
	// of bytes to prevent the VM from trying to fetch the
	// cid from the state tree. We still want to use IPLD
	// for now. We could fix this by setting an empty AMT
	// with the list of Cids, but it may be too complex just
	// for the sake of using CBOR.
	Checks [][]byte //[]cid.Cid
}

// CrossMsgMeta includes information about the messages being propagated from and to
// a subnet.
//
// MsgsCid is the cid of the list of cids of the mesasges propagated
// for a specific subnet in that checkpoint
type CrossMsgMeta struct {
	From    string // Determines the source of the messages being propagated in MsgsCid
	To      string // Determines the destination of the messages included in MsgsCid
	MsgsCid []byte // cid.Cid of the msgMeta with the list of msgs.
	Nonce   int    // Nonce of the msgMeta
	Value   string // Token amount being propagated in MsgMeta
}

// CheckData is the data included in a Checkpoint.
type CheckData struct {
	Source string
	TipSet []byte // NOTE: For simplicity we add TipSetKey. We could include full TipSet
	Epoch  int
	// FIXME: Under these bytes there's a cid.Cid. The reason for doing this is
	// to prevent the VM from interpreting it as a CID from the state
	// tree trying to fetch it and failing because it can't find anything, so we
	// are "hiding" them behing a byte type. We could choose to use an EmptyCid
	// and use cbor-gen.
	PrevCheckCid []byte
	Childs       []ChildCheck   // List of child checks
	CrossMsgs    []CrossMsgMeta // List with meta of msgs being propagated.
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
func initCrossMsgMetaSchema() schema.Type {
	ts := schema.TypeSystem{}
	ts.Init()
	ts.Accumulate(schema.SpawnString("String"))
	ts.Accumulate(schema.SpawnInt("Int"))
	ts.Accumulate(schema.SpawnLink("Link"))
	ts.Accumulate(schema.SpawnBytes("Bytes"))

	ts.Accumulate(schema.SpawnStruct("CrossMsgMeta",
		[]schema.StructField{
			schema.SpawnStructField("From", "String", false, false),
			schema.SpawnStructField("To", "String", false, false),
			schema.SpawnStructField("MsgsCid", "Bytes", false, false),
			schema.SpawnStructField("Nonce", "Int", false, false),
			schema.SpawnStructField("Value", "String", false, false),
		},
		schema.SpawnStructRepresentationMap(map[string]string{}),
	))

	return ts.TypeByName("CrossMsgMeta")
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
			schema.SpawnStructField("Checks", "List_Bytes", false, false),
		},
		schema.SpawnStructRepresentationMap(map[string]string{}),
	))
	ts.Accumulate(initCrossMsgMetaSchema())

	ts.Accumulate(schema.SpawnStruct("CheckData",
		[]schema.StructField{
			schema.SpawnStructField("Source", "String", false, false),
			schema.SpawnStructField("TipSet", "Bytes", false, false),
			schema.SpawnStructField("Epoch", "Int", false, false),
			schema.SpawnStructField("PrevCheckCid", "Bytes", false, false),
			schema.SpawnStructField("Childs", "List_ChildCheck", false, false),
			schema.SpawnStructField("CrossMsgs", "List_CrossMsgMeta", false, false),
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
	ts.Accumulate(schema.SpawnList("List_Bytes", "Bytes", false))
	ts.Accumulate(schema.SpawnList("List_ChildCheck", "ChildCheck", false))
	ts.Accumulate(schema.SpawnList("List_CrossMsgMeta", "CrossMsgMeta", false))

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
func NewRawCheckpoint(source address.SubnetID, epoch abi.ChainEpoch) *Checkpoint {
	return &Checkpoint{
		Data: CheckData{
			Source:       source.String(),
			Epoch:        int(epoch),
			PrevCheckCid: NoPreviousCheck.Bytes(),
		},
	}

}

func NewCrossMsgMeta(from, to address.SubnetID) *CrossMsgMeta {
	return &CrossMsgMeta{
		From:  from.String(),
		To:    to.String(),
		Nonce: -1,
		Value: "0",
	}
}

func (c *Checkpoint) IsEmpty() (bool, error) {
	return c.Equals(EmptyCheckpoint)
}

func (c *Checkpoint) SetPrevious(cid cid.Cid) {
	c.Data.PrevCheckCid = cid.Bytes()
}

func (c *Checkpoint) SetTipsetKey(ts types.TipSetKey) {
	c.Data.TipSet = ts.Bytes()
}

func (c *Checkpoint) SetEpoch(ep abi.ChainEpoch) {
	c.Data.Epoch = int(ep)
}

func (c *Checkpoint) PreviousCheck() (cid.Cid, error) {
	_, cid, err := cid.CidFromBytes(c.Data.PrevCheckCid)
	return cid, err
}

func (c *Checkpoint) Source() address.SubnetID {
	return address.SubnetID(c.Data.Source)
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

func (cm *CrossMsgMeta) Cid() (cid.Cid, error) {
	_, c, err := cid.CidFromBytes(cm.MsgsCid)
	return c, err
}

func (cm *CrossMsgMeta) GetFrom() address.SubnetID {
	return address.SubnetID(cm.From)
}

func (cm *CrossMsgMeta) GetTo() address.SubnetID {
	return address.SubnetID(cm.To)
}

func (cm *CrossMsgMeta) SetCid(c cid.Cid) {
	cm.MsgsCid = c.Bytes()
}

func (cm *CrossMsgMeta) Equal(other *CrossMsgMeta) bool {
	return cm.From == other.From && cm.To == other.To && bytes.Equal(cm.MsgsCid, other.MsgsCid)
}

func (cm *CrossMsgMeta) MarshalCBOR(w io.Writer) error {
	node := bindnode.Wrap(cm, MsgMetaSchema)
	nodeRepr := node.Representation()
	err := dagcbor.Encode(nodeRepr, w)
	if err != nil {
		return err
	}
	return nil
}

func (cm *CrossMsgMeta) UnmarshalCBOR(r io.Reader) error {
	nb := bindnode.Prototype(cm, MsgMetaSchema).NewBuilder()
	err := dagcbor.Decode(nb, r)
	if err != nil {
		return err
	}
	n := bindnode.Unwrap(nb.Build())

	ch, ok := n.(*CrossMsgMeta)
	if !ok {
		return xerrors.Errorf("Unmarshalled node not of type CheckData")
	}
	*cm = *ch
	return nil
}

func (cm *CrossMsgMeta) GetValue() (abi.TokenAmount, error) {
	return big.FromString(cm.Value)
}

func (cm *CrossMsgMeta) AddValue(x abi.TokenAmount) error {
	v, err := cm.GetValue()
	if err != nil {
		return err
	}
	cm.Value = big.Add(v, x).String()
	return nil
}

func (cm *CrossMsgMeta) SubValue(x abi.TokenAmount) error {
	v, err := cm.GetValue()
	if err != nil {
		return err
	}
	cm.Value = big.Sub(v, x).String()
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
		c.Data.Childs[ind].Checks = append(c.Data.Childs[ind].Checks, chcid.Bytes())
		return nil
	}
	chcc := ChildCheck{ch.Data.Source, [][]byte{chcid.Bytes()}}
	c.Data.Childs = append(c.Data.Childs, chcc)
	return nil
}

func (c *ChildCheck) hasCheck(cid cid.Cid) int {
	for i, ch := range c.Checks {
		if bytes.Equal(ch, cid.Bytes()) {
			return i
		}
	}
	return -1
}

func (c *Checkpoint) HasChildSource(source address.SubnetID) int {
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

func (c *Checkpoint) GetSourceChilds(source address.SubnetID) ChildCheck {
	i := c.HasChildSource(source)
	return c.GetChilds()[i]
}

func (c *Checkpoint) GetChilds() []ChildCheck {
	return c.Data.Childs
}

func (c *Checkpoint) Epoch() abi.ChainEpoch {
	return abi.ChainEpoch(c.Data.Epoch)
}

func (c *Checkpoint) TipSet() (types.TipSetKey, error) {
	return types.TipSetKeyFromBytes(c.Data.TipSet)
}

func (c *Checkpoint) EqualTipSet(tsk types.TipSetKey) bool {
	return bytes.Equal(tsk.Bytes(), c.Data.TipSet)
}

// CrossMsgs returns crossMsgs data included in checkpoint
func (c *Checkpoint) CrossMsgs() []CrossMsgMeta {
	return c.Data.CrossMsgs
}

// CrossMsgMeta returns the MsgMeta from and to a subnet from a checkpoint
// and the index the crossMsgMeta is in the slice
func (c *Checkpoint) CrossMsgMeta(from, to address.SubnetID) (int, *CrossMsgMeta) {
	for i, m := range c.Data.CrossMsgs {
		if m.From == from.String() && m.To == to.String() {
			return i, &m
		}
	}
	return -1, nil
}

func (c *Checkpoint) AppendMsgMeta(meta *CrossMsgMeta) {
	_, has := c.CrossMsgMeta(meta.GetFrom(), meta.GetTo())
	// If no previous, append right away
	if has == nil {
		c.Data.CrossMsgs = append(c.Data.CrossMsgs, *meta)
		return
	}

	// If not equal Cids
	if !bytes.Equal(has.MsgsCid, meta.MsgsCid) {
		c.Data.CrossMsgs = append(c.Data.CrossMsgs, *meta)
		return
	}

	// Do nothing in the rest of the cases
}

func (c *Checkpoint) SetMsgMetaCid(i int, cd cid.Cid) {
	c.Data.CrossMsgs[i].MsgsCid = cd.Bytes()
}

func (c *Checkpoint) AddValueMetaCid(i int, x abi.TokenAmount) error {
	return c.Data.CrossMsgs[i].AddValue(x)
}

func (c *Checkpoint) SubValueMetaCid(i int, x abi.TokenAmount) error {
	return c.Data.CrossMsgs[i].SubValue(x)
}

// CrossMsgsTo returns the crossMsgsMeta directed to a specific subnet
func (c *Checkpoint) CrossMsgsTo(to address.SubnetID) []CrossMsgMeta {
	out := make([]CrossMsgMeta, 0)
	for _, m := range c.Data.CrossMsgs {
		if m.To == to.String() {
			out = append(out, m)
		}
	}
	return out
}

func ByteSliceToCidList(l [][]byte) ([]cid.Cid, error) {
	out := make([]cid.Cid, len(l))
	for i, x := range l {
		_, c, err := cid.CidFromBytes(x)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}
