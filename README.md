
<h1 align="center">Project Eudico</h1>

Eudico is a modularised implementation of [Lotus](https://github.com/filecoin-project/lotus), itself an implementation of the Filecoin Distributed Storage Network. For more details about Filecoin, check out the [Filecoin Spec](https://spec.filecoin.io). This is a work-in-progress, intended to enable easier experimentation with future protocol features, and is not meant to be used in the production network.

## Building & Documentation

> Note: The default `master` branch is the dev branch, please use with caution. 
 
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

To build Eudico, you need a working installation of [Go 1.16.4 or higher](https://golang.org/dl/):

```bash
wget -c https://golang.org/dl/go1.16.4.linux-amd64.tar.gz -O - | sudo tar -xz -C /usr/local
```

**TIP:**
You'll need to add `/usr/local/go/bin` to your path. For most Linux distributions you can run something like:

```shell
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc && source ~/.bashrc
```

See the [official Golang installation instructions](https://golang.org/doc/install) if you get stuck.

### Build and install Eudico

Once all the dependencies are installed, you can build and install the Eudico suite (`eudico`, `eudico-miner`, and `eudico-worker`).

1. Clone the repository:

   ```sh
   git clone https://github.com/filecoin-project/eudico.git
   cd lotus/
   ```
   
Note: The default branch `master` is the dev branch where the latest new features, bug fixes and improvement are in. 

2. To join mainnet -- don't!

   If you are changing networks from a previous installation or there has been a network reset, read the [Switch networks guide](https://docs.filecoin.io/get-started/lotus/switch-networks/) before proceeding.

   For networks other than mainnet, look up the current branch or tag/commit for the network you want to join in the [Filecoin networks dashboard](https://network.filecoin.io), then build Eudico for your specific network below.

   ```sh
   git checkout <tag_or_branch>
   # For example:
   git checkout <vX.X.X> # tag for a release
   ```

   Currently, the latest code on the _master_ branch corresponds to mainnet.

3. If you are in China, see "[Lotus: tips when running in China](https://docs.filecoin.io/get-started/lotus/tips-running-in-china/)".
4. This build instruction uses the prebuilt proofs binaries. If you want to build the proof binaries from source check the [complete instructions](https://docs.filecoin.io/get-started/lotus/installation/#build-and-install-lotus). Note, if you are building the proof binaries from source, [installing rustup](https://docs.filecoin.io/get-started/lotus/installation/#rustup) is also needed.

5. Build and install Eudico:

   ```sh
   make clean all #mainnet

   # Or to join a testnet or devnet:
   make clean calibnet # Calibration with min 32GiB sectors
   make clean nerpanet # Nerpa with min 512MiB sectors

   sudo make install
   ```

   This will put `eudico`, `eudico-miner` and `eudico-worker` in `/usr/local/bin`.

   `eudico` will use the `$HOME/.eudico` folder by default for storage (configuration, chain data, wallets, etc). See [advanced options](https://docs.filecoin.io/get-started/lotus/configuration-and-advanced-usage/) for information on how to customize the folder.

6. You should now have Eudico installed. You can now [start the Eudico daemon and sync the chain](https://docs.filecoin.io/get-started/lotus/installation/#start-the-lotus-daemon-and-sync-the-chain).

## License

Dual-licensed under [MIT](https://github.com/filecoin-project/lotus/blob/master/LICENSE-MIT) + [Apache 2.0](https://github.com/filecoin-project/lotus/blob/master/LICENSE-APACHE)
