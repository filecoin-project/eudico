package hierarchical

import (
	"fmt"
	"strings"

	addr "github.com/filecoin-project/go-address"
)

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

// ValidatorsFromString parses comma-separated subnet:ID@OpaqueNetAddr validators string.
// OpaqueNetAddr can contain GRPC or Libp2p addresses.
//
// Examples of the validators:
// 	- /root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ
// 	- /root:t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@127.0.0.1:1000
func ValidatorsFromString(input string) ([]Validator, error) {
	var validators []Validator
	for _, idAddr := range splitAndTrimEmpty(input, ",", " ") {
		parts := strings.Split(idAddr, "@")
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed to parse validators string")
		}
		subnetAndID := parts[0]
		opaqueNetAddr := parts[1]

		subnetAndIDParts := strings.Split(subnetAndID, ":")
		if len(subnetAndIDParts) != 2 {
			return nil, fmt.Errorf("failed to parse subnet and ID")
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
			Subnet:  subnet,
			Addr:    a,
			NetAddr: opaqueNetAddr,
		}
		validators = append(validators, v)
	}
	return validators, nil
}

// ValidatorsToString adds a validator subnet, address and network address into a string.
func ValidatorsToString(validators []Validator) string {
	var s string
	for _, v := range validators {
		s += fmt.Sprintf("%s:%s@%s,", v.Subnet.String(), v.Addr.String(), v.NetAddr)
	}
	return strings.TrimSuffix(s, ",")
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
