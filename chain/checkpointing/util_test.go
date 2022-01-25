package checkpointing

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestTaprootSignatureHash(t *testing.T) {
	tx, _ := hex.DecodeString("0200000001cbfbdd3778e1d2e2b22fc728f3b902ff6e4df7a40582367e20aec056e05fbd9d0000000000ffffffff0280da2d0900000000225120b9435744b668ab44e3074432bf1b167d4e655db03be13aa6db295055a220b26a0000000000000000056a0363696400000000")
	utxo, _ := hex.DecodeString("50ed400900000000225120b9435744b668ab44e3074432bf1b167d4e655db03be13aa6db295055a220b26a")

	sig_hash, _ := TaprootSignatureHash(tx, utxo, 0x00)

	if hex.EncodeToString(sig_hash) != "6b0fe64d6f1af182fb8b0d9e1f8587fafb08162b60495dfb2a1799516bb80874" {
		fmt.Println(hex.EncodeToString(sig_hash))
		t.Errorf("Invalid hash")
	}
}

func TestTaggedHash(t *testing.T) {
	tag := taggedHash("TapSighash")

	if hex.EncodeToString(tag) != "dabc11914abcd8072900042a2681e52f8dba99ce82e224f97b5fdb7cd4b9c803" {
		fmt.Println(hex.EncodeToString(tag))
		t.Errorf("Invalid Tag")
	}
}

func TestTaggedHashExtraData(t *testing.T) {
	tag := taggedHash("TapSighash", []byte{0})

	if hex.EncodeToString(tag) != "c2fd0de003889a09c4afcf676656a0d8a1fb706313ff7d509afb00c323c010cd" {
		fmt.Println(hex.EncodeToString(tag))
		t.Errorf("Invalid Tag")
	}
}

//some testvectors from https://github.com/bitcoin/bips/blob/995f45211d1baac4ac34685cf09d804eb8edd078/bip-0341/wallet-test-vectors.json
func TestTweakPubkey(t *testing.T) {
	internal_pubkey, _ := hex.DecodeString("d6889cb081036e0faefa3a35157ad71086b123b2b144b649798b494c300a961d")
	tweak, _ := hex.DecodeString("b86e7be8f39bab32a6f2c0443abbc210f0edac0e2c53d501b36b64437d9c6c70")

	tweaked_pubkey := applyTweakToPublicKeyTaproot(internal_pubkey, tweak)

	if hex.EncodeToString(tweaked_pubkey) != "53a1f6e454df1aa2776a2814a721372d6258050de330b3c6d10ee8f4e0dda343" {
		t.Errorf("Invalid tweaked pubkey")
	}
}

func TestMerkleHash(t *testing.T) {
	pubkey, _ := hex.DecodeString("187791b6f712a8ea41c8ecdd0ee77fab3e85263b37e1ec18a3651926b3a6cf27")
	merkle_root, _ := hex.DecodeString("5b75adecf53548f3ec6ad7d78383bf84cc57b55a3127c72b9a2481752dd88b21")

	test_tweak := hashTweakedValue(pubkey, merkle_root)

	if hex.EncodeToString(test_tweak) != "cbd8679ba636c1110ea247542cfbd964131a6be84f873f7f3b62a777528ed001" {
		t.Errorf("Invalid tweaked pubkey")
	}
}
