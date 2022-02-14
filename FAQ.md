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

**Q: How to run a consensus protocol?
A:** Use the following commands:

Run a network:
 ```
 ./eudico tspow genesis $ADDR gen.gen
 ./eudico tspow daemon --genesis=gen.gen
 ```
 Run a miner:
 ```
 ./eudico wallet import --format=json-lotus $ADDR
 ./eudico tspow miner
 ```
 
**Q: How to run two eudico clients on the same host?
A:** Use `EUDICO_PATH` variable and `api` argument.

Terminal 1:
```
export EUDICO_PATH="~/.eudico1/"
./eudico tspow daemon --genesis=gen.gen --api=1234
```

Terminal 2:
```
export EUDICO_PATH="~/.eudico2/"
./eudico tspow daemon --genesis=gen.gen --api=1235
```

**Q: How to connect two eudico clients with each other?
A:** Suppose you have two eudico clients A and B. Run the following commands:

On the client A:
The command below outputs the target libp2p address `ADDR_A` 
```
./eudico net listen
```

On the client B:
```
./eudico net connect ADDR_A
```
