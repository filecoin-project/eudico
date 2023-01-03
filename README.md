
<h1 align="center">[Archived] Project Eudico</h1>

------
> This repo is where the first of MVP for the IPC (InterPlanetary Consensus) framework and other consensus-related experiments by ConsensusLab were implemented and tested. This fork of Lotus is no longer maintained and a lot of the features and protocol tested here are being productionized in a new and [improved version of Eudico](https://github.com/consensus-shipyard/lotus/). 
>
> We keep this repo archived for historical relevance and future reference (it still includes a lot of valuable code that haven't been merged and implemented in production). If you are looking to test Eudico in a testnet refer to [Spacenet](https://github.com/consensus-shipyard/spacenet). You can also learn more about all of our ongoing projects and research at https://consensuslab.world.
------

Eudico is a modularised implementation of [Lotus](https://github.com/filecoin-project/lotus), itself an implementation of the Filecoin Distributed Storage Network. For more details about Filecoin, check out the [Filecoin Spec](https://spec.filecoin.io). This is a work-in-progress, intended to enable easier experimentation with future protocol features, and is not meant to be used in the production network.

## Building & Documentation

> Note: The default `eudico` branch is the dev branch, please use with caution. 
 
For complete instructions on how to build, install and setup eudico, please visit the Lotus documentation at [https://docs.filecoin.io/get-started/lotus](https://docs.filecoin.io/get-started/lotus/). Basic build instructions can be found further down in this readme.

## Reporting a Vulnerability

Please send an email to security@filecoin.org. See our [security policy](SECURITY.md) for more details.

## Related packages

These repos are independent and reusable modules, but are tightly integrated into Lotus/Eudico to make up a fully featured Filecoin implementation:

- [go-fil-markets](https://github.com/filecoin-project/go-fil-markets) which has its own [kanban work tracker available here](https://app.zenhub.com/workspaces/markets-shared-components-5daa144a7046a60001c6e253/board)
- [specs-actors](https://github.com/filecoin-project/specs-actors) which has its own [kanban work tracker available here](https://app.zenhub.com/workspaces/actors-5ee6f3aa87591f0016c05685/board)

## Contribute

Eudico is an open project and welcomes contributions of all kinds: code, docs, and more. However, before making a contribution, we ask you to heed these recommendations.

When implementing a change:

1. Adhere to the standard Go formatting guidelines, e.g. [Effective Go](https://golang.org/doc/effective_go.html). Run `go fmt`.
2. Stick to the idioms and patterns used in the codebase. Familiar-looking code has a higher chance of being accepted than eerie code. Pay attention to commonly used variable and parameter names, avoidance of naked returns, error handling patterns, etc.
3. Comments: follow the advice on the [Commentary](https://golang.org/doc/effective_go.html#commentary) section of Effective Go.
4. Minimize code churn. Modify only what is strictly necessary. Well-encapsulated changesets will get a quicker response from maintainers.
5. Lint your code with [`golangci-lint`](https://golangci-lint.run) (CI will reject your PR if unlinted).
6. Add tests.
7. Title the PR in a meaningful way and describe the rationale and the thought process in the PR description.
8. Write clean, thoughtful, and detailed [commit messages](https://chris.beams.io/posts/git-commit/). This is even more important than the PR description, because commit messages are stored _inside_ the Git history. One good rule is: if you are happy posting the commit message as the PR description, then it's a good commit message.

## Basic Build Instructions
**System-specific Software Dependencies**:

Building Eudico requires some system dependencies, usually provided by your distribution.

Ubuntu/Debian:
```
sudo apt install mesa-opencl-icd ocl-icd-opencl-dev gcc git bzr jq pkg-config curl clang build-essential hwloc libhwloc-dev wget -y && sudo apt upgrade -y
```

Fedora:
```
sudo dnf -y install gcc make git bzr jq pkgconfig mesa-libOpenCL mesa-libOpenCL-devel opencl-headers ocl-icd ocl-icd-devel clang llvm wget hwloc libhwloc-dev
```

For other distributions you can find the required dependencies [here.](https://docs.filecoin.io/get-started/lotus/installation/#system-specific) For instructions specific to macOS, you can find them [here.](https://docs.filecoin.io/get-started/lotus/installation/#macos)

#### Go

To build Eudico, you need a working installation of [Go 1.17.9 or higher](https://golang.org/dl/):

```bash
wget -c https://golang.org/dl/go1.17.9.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
```

**TIP:**
You'll need to add `/usr/local/go/bin` to your path. For most Linux distributions you can run something like:

```shell
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc && source ~/.bashrc
```

See the [official Golang installation instructions](https://golang.org/doc/install) if you get stuck.

### Build and install Eudico

Once all the dependencies are installed, you can build and install Eudico.

1. Clone the repository:

   ```sh
   git clone https://github.com/filecoin-project/eudico.git
   cd eudico/
   ```
   
Note: The default branch `eudico` is the dev branch where the latest new features, bug fixes and improvement are in. 

2. Build Eudico:

   ```sh
   make eudico
   ```
   This will create the `eudico` executable in the current directory.

### Run a local test network.

**Note**: `eudico` uses the `$HOME/.eudico` folder by default for storage (configuration, chain data, wallets, etc). See [advanced options](https://docs.filecoin.io/get-started/lotus/configuration-and-advanced-usage/) for information on how to customize the folder.
If you want to run more than one Eudico node the same host, you need to tell the nodes to use different folders (see [FAQ](FAQ.md#q-how-can-i-run-two-eudico-peers-on-the-same-host))
Make sure that this directory does not exist when you are starting a new network.

First, a key needs to be generated. 
In order to do that, compile the Lotus key generator:

   ```bash
   make lotus-keygen
   ```

Then, generate a key:

   ```bash
   ./lotus-keygen -t secp256k1
   ```
This creates a key file, with the name `f1[...].key` (e.g. `f16dv4rlp3b33d5deasf3lxkrbfwhi4q4a5uw5scy.key`) in the local directory.
The file name, without the `.key` extension, is the corresponding Filecoin address.
If this is the only key you generated so far, you can obtain the address, for example, by running

   ```bash
   ADDR=$(echo f1* | tr '.' ' ' | awk '{print $1}')
   ```

Use this address to create a genesis block for the system and start the Eudico daemon.
The following command uses the `delegated` consensus.

   ```bash
   ./eudico delegated genesis $ADDR gen.gen
   ./eudico delegated daemon --genesis=gen.gen
   ```

The daemon will continue running until you stop it.
To start a miner, first import a wallet, using the generated key
(replacing `f1*.key` by the generated key file if more than one key is present in the directory).

   ```bash
   ./eudico wallet import --format=json-lotus f1*.key
   ```

Then, start the miner.

   ```bash
   ./eudico delegated miner
   ```

## License

Dual-licensed under [MIT](https://github.com/filecoin-project/lotus/blob/master/LICENSE-MIT) + [Apache 2.0](https://github.com/filecoin-project/lotus/blob/master/LICENSE-APACHE)
