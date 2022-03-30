# Eudico Garden 🪴
In this folder you'll find everything you need to start your own Eudico Garden. _But what is Eudico Garden?_ you may be wondering. Well, it's just a set of convenient scripts to deploy an Eudico test network over a cloud infrastructure for you to test all of its features.

By default, Eudico Garden deploys a hierarchical consensus rootnet running Filecoin Expected Consensus with
`2k` sectors and new blocks every `~6 s`. With some basic tinkering to the Eudico Garden scripts, it shouldn't be 
hard for you to support other rootnet consensus or additional features for your testnet.
That being said, these are just the first strokes, if you have a specific feature request, or some code you'll like to contribute
_do not hesistate to drop an issue/PR/discussion_.

## Requirements
Eudico Garden uses Terraform and is currently implemented to use AWS as its cloud backend. In order to run Eudico garden
you'll need.
- [Terraform](https://learn.hashicorp.com/tutorials/terraform/install-cli)
- AWS CLI installed and an AWS account.
- `jq`
- An ssh key initialized in your host.

## Usage ⛲
All the scripts for Eudico Garden are designed to be run from this folder, so the first thing to do if you are still in the repos
root is to `cd eudicogarden`.

### Deployment 
Deploying your initial network is as simple as running:
```bash
./deploy.sh <num_nodes>
``` 
This script will run an Eudico network with one bootstrap node and `<num_nodes>` genesis miners. This scripts deploys de infrastructure,
generates the genesis, and bootstrap the network. _We currently only support
the deployment of up to 9 genesis miners. More genesis minerse will be supported in the future (you can always add more miners
manually)_.

These scripts takes your default ssh key as main access key for your nodes. You can check the state of your network, it logs,
as well as interact with them by:
```bash
# Check public addresses of Eudico nodes
terraform output eudico_nodes_ip
# Access node
ssh ubuntu@<node_ip>
tmux a
```
If the command succeeds you should be able to see all of your nodes syncing. An easy way to 

### Adding new nodes
If you want to add a new node to the network run:
```bash
./add_node.sh
```
This scripts adds a new miner to the network. If you are looking to deploy a full-node but not a miner,
you just need to comment the miner initialization commands from `init_new_node.sh`

### Granting SSH access to additional keys
If you want to grant access to additional ssh keys to the nodes in the Eudico Garden cluster,
you can run the following script giving the public you want to gran access to as the first argument:
```bash
./add_ssh_key.sh <path_ssh_key>
# Example
./add_ssh_key.sh ~/.ssh/mykey.pub
```

### Destroy
If you no longer want to run an Eudico Garde you can run the following script to clean the infrastructure
and all related assets of the deployment:
```bash
./destroy.sh
```

## Additional resources
If these are your first steps deploying a Filecoin network, you may find these resources quite helpful:
- [Lotus Local Network Docs](https://lotus.filecoin.io/developers/local-network/)
- [Test in lotus local devnet](https://www.notion.so/pl-strflt/Test-in-lotus-local-devnet-08618dac5bb54d00837c6dabf08913b8)
- [How to bootstrap your own network](https://www.notion.so/pl-strflt/PUBLIC-How-to-bootstrap-your-own-network-e072ff97f0bb4906930b809b630eddd0#ebfc402c7eac47c9a2a0a96b5a1da7f4)

## Contributions Welcome!
Eudico Garden is a one-week MVP to solve some of our most immediate needs. There's a lot of work to
be done to make Eudico Garden and its automation scripts stable and consistent. Any feeedback or contribution
to this code is more than welcome. Filter by the `eudicogarden` tag to find some of the things we are missing.
