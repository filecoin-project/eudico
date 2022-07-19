package hierarchical

import (
	"fmt"
	"strings"

	"github.com/multiformats/go-multiaddr"
	"golang.org/x/xerrors"

	addr "github.com/filecoin-project/go-address"
	t "github.com/filecoin-project/mir/pkg/types"
)

// Validator encapsulated string to be compatible with CBOR implementation.
type Validator struct {
	Subnet  addr.SubnetID
	Addr    addr.Address
	NetAddr string
}

func NewValidator(subnet addr.SubnetID, addr addr.Address, netAddr string) Validator {
	return Validator{
		subnet, addr, netAddr,
	}
}

func (v *Validator) HAddr() (addr.Address, error) {
	return addr.NewHCAddress(v.Subnet, v.Addr)
}

func (v *Validator) ID() string {
	return fmt.Sprintf("%s:%s", v.Subnet, v.Addr)
}

// EncodeValidatorInfo adds a validator subnet, address and network address into a string.
func EncodeValidatorInfo(vilidators []Validator) string {
	var s string
	for _, v := range vilidators {
		s += fmt.Sprintf("%s:%s@%s,", v.Subnet.String(), v.Addr.String(), v.NetAddr)
	}
	return strings.TrimSuffix(s, ",")
}

// ParseValidatorsString parses comma-delimited subnet:ID@OpaqueNetAddr validators string.
//
// Examples of the validators:
// 	- /root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ
// 	- /root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@127.0.0.1:1000
func ParseValidatorsString(input string) ([]Validator, error) {
	var validators []Validator
	for _, idAddr := range splitAndTrimEmpty(input, ",", " ") {
		parts := strings.Split(idAddr, "@")
		if len(parts) != 2 {
			return nil, xerrors.New("failed to parse validators string")
		}
		subnetAndID := parts[0]
		opaqueNetAddr := parts[1]

		subnetAndIDParts := strings.Split(subnetAndID, ":")
		if len(subnetAndIDParts) != 2 {
			return nil, xerrors.New("failed to parse subnet and ID")
		}
		subnetStr := subnetAndIDParts[0]
		ID := subnetAndIDParts[1]

		a, err := addr.NewFromString(ID)
		if err != nil {
			return nil, err
		}

		subnet, err := addr.SubnetIDFromString(subnetStr)
		if err != nil {
			return nil, err
		}

		v := Validator{
			subnet,
			a,
			opaqueNetAddr,
		}
		validators = append(validators, v)
	}
	return validators, nil
}

// ParseValidatorInfo parses comma-delimited ID@host:port persistent validator string.
// Example of the peers sting: "ID1@IP1:26656,ID2@IP2:26656,ID3@IP3:26656,ID4@IP4:26656".
// At present, we suppose that input is trusted.
// TODO: add input validation.
func ParseValidatorInfo(input string) ([]t.NodeID, map[t.NodeID]string, error) {
	var nodeIds []t.NodeID
	nodeAddrs := make(map[t.NodeID]string)

	for _, idAddr := range splitAndTrimEmpty(input, ",", " ") {
		ss := strings.Split(idAddr, "@")
		if len(ss) != 2 {
			return nil, nil, xerrors.New("failed to parse persistent nodes")
		}

		id := t.NodeID(ss[0])
		netAddr := ss[1]
		nodeIds = append(nodeIds, id)
		nodeAddrs[id] = netAddr
	}
	return nodeIds, nodeAddrs, nil
}

func BuildValidatorsMembership(validators []Validator) ([]t.NodeID, map[t.NodeID]string, error) {
	var nodeIds []t.NodeID
	nodeAddrs := make(map[t.NodeID]string)

	for _, v := range validators {
		id := t.NodeID(v.ID())
		netAddr := v.NetAddr
		nodeIds = append(nodeIds, id)
		nodeAddrs[id] = netAddr
	}
	return nodeIds, nodeAddrs, nil
}

// Libp2pValidatorsMembership validates that validators addresses are valid multi addresses and
// returns all validators IDs and map between IDs and multi addresses.
func Libp2pValidatorsMembership(validators []Validator) ([]t.NodeID, map[t.NodeID]multiaddr.Multiaddr, error) {
	var nodeIDs []t.NodeID
	nodeAddrs := make(map[t.NodeID]multiaddr.Multiaddr)

	for _, v := range validators {
		id := t.NodeID(v.ID())
		info, err := multiaddr.NewMultiaddr(v.NetAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse multi address: %w", err)
		}
		nodeIDs = append(nodeIDs, id)
		nodeAddrs[id] = info
	}
	return nodeIDs, nodeAddrs, nil
}

func splitAndTrimEmpty(s, sep, cutset string) []string {
	if s == "" {
		return []string{}
	}

	spl := strings.Split(s, sep)
	nonEmptyStrings := make([]string, 0, len(spl))

	for i := 0; i < len(spl); i++ {
		element := strings.Trim(spl[i], cutset)
		if element != "" {
			nonEmptyStrings = append(nonEmptyStrings, element)
		}
	}

	return nonEmptyStrings
}
