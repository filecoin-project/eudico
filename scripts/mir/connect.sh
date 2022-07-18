# This script is a helper connecting Eudico nodes with each other.
# The script is called from other main scripts.

NODE_0_PATH="$HOME/.eudico-node0"
NODE_1_PATH="$HOME/.eudico-node1"
NODE_2_PATH="$HOME/.eudico-node2"
NODE_3_PATH="$HOME/.eudico-node3"

NODE_0_NETADDR="$NODE_0_PATH/.netaddr"
NODE_1_NETADDR="$NODE_1_PATH/.netaddr"
NODE_2_NETADDR="$NODE_2_PATH/.netaddr"
NODE_3_NETADDR="$NODE_3_PATH/.netaddr"

T=6

if [[ $1 -eq 0 ]];
then
  ./eudico net listen | grep '/ip6/::1/' > $NODE_0_NETADDR; sleep $T;
fi

if [[ $1 -eq 1 ]];
then
  ./eudico net listen | grep '/ip6/::1/' > $NODE_1_NETADDR; sleep $T;
  ./eudico net connect $(cat $NODE_0_NETADDR);
fi

if [[ $1 -eq 2 ]];
then
  ./eudico net listen | grep '/ip6/::1/' > $NODE_2_NETADDR; sleep $T;
  ./eudico net connect $(cat $NODE_0_NETADDR);
  ./eudico net connect $(cat $NODE_1_NETADDR);
fi

if [[ $1 -eq 3 ]];
then
  ./eudico net listen | grep '/ip6/::1/' > $NODE_3_NETADDR; sleep $T;
  ./eudico net connect $(cat $NODE_0_NETADDR);
  ./eudico net connect $(cat $NODE_1_NETADDR);
  ./eudico net connect $(cat $NODE_2_NETADDR);
fi
