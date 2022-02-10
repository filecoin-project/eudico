# Eudico FAQ

**Q: How to import a wallet?**

**A:** Generate a key:
```
./lotus-keygen -t spec256k1
```
Import it (as default if needed):
```
./eudico wallet import –-as-default –-format=json-lotus $file
```

**Q: How to generate the cbor files when creating a new actor?**

**A:** The Cbor library is used to help with marshalling and unmarshalling.
In the same folder where your `$actorName_actor.go` and `$actorName_state.go` files are defined, create a `gen` folder with a `gen.go` file that looks like [this](https://github.com/filecoin-project/eudico/blob/eudico/chain/consensus/hierarchical/actors/sca/gen/gen.go
). Add all the correspondings states and parameters, update the import url and add the package using `go get`. 
Pre-populate the functions of the interface in a `cbor_gen.go` file as follows:
```
func (t *$YourStruct) MarshalCBOR(w io.Writer) error { return nil }

func (t *$YourStruct) MarshalCBOR(w io.Writer) error { return nil }
```
Run  `go run gen.go`. 


**Q: How do you send a transaction directly from the code?**
**A:** By using the `api-MpoolPushMessage.` See it use [here](https://github.com/filecoin-project/eudico/blob/113829e7fc115daac08ea0217170baddcb7788ba/chain/consensus/hierarchical/subnet/manager/manager.go#L375-L391).

**Q: How to initialize an actor?
A:** This is tricky and can be done by imitating [this code](https://github.com/filecoin-project/eudico/blob/113829e7fc115daac08ea0217170baddcb7788ba/chain/consensus/hierarchical/actors/subnet/genesis.go#L131).