./scripts/delete_eudico_data.sh
export LOTUS_SKIP_GENESIS_CHECK=_yes_
./eudico delegated genesis t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba gen.gen
./scripts/data-permissions.sh
