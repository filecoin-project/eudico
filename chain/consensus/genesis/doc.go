package genesis

// This is a light copy of chain/gen/genesis. We don't want to change consensus/filcns much to
// avoid lots of conflicts and ease the rebasing with Lotus. We had to use a few tricks and
// duplicate blocks to be able to generate filcns genesis blocks from shards and prevent
// import cycles. You'll see in chain/consensus/actors/shard a few more comments of the like of
// "this is horrible and needs code de-duplication and rearchitecting the code". I sincerely
// apologize for this, but this is
// a first proof-of-concept, and we want to validate how sharding would look like and work
// before starting to mess-up with a lot of code reorgs and refactors.
