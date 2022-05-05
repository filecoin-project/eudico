package subnet

import (
	"fmt"
	"strings"

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
	return addr.NewHAddress(v.Subnet, v.Addr)
}

func (v *Validator) ID() string {
	return fmt.Sprintf("%s:%s", v.NetAddr, v.Addr)
}

func GetValidatorAddrs(vals []Validator) []addr.Address {
	var addrs []addr.Address
	for _, v := range vals {
		addrs = append(addrs, v.Addr)
	}
	return addrs
}

// EncodeValidatorInfo adds a validator subnet, address and network address into a string.
func EncodeValidatorInfo(vals []Validator) string {
	var s string
	for _, v := range vals {
		s += fmt.Sprintf("%s:%s@%s,", v.Subnet.String(), v.Addr.String(), v.NetAddr)
	}
	return strings.TrimSuffix(s, ",")
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
