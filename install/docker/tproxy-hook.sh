#!/bin/bash

# parse the arguments
for i in "$@"; do
  case $i in
    --transparent-type=*)
      TYPE="${i#*=}"
      shift
      ;;
    --stage=*)
      STAGE="${i#*=}"
      shift
      ;;
    -*|--*)
      echo "Unknown option $i"
      shift
      ;;
    *)
      ;;
  esac
done


case "$STAGE" in
post-start)
  # at the post-start stage
  if [ "$TYPE" = "tproxy" ]; then
    # we check if the transparent type is tproxy, and if so, we disable the bridge netfilter call and remove the docker rule in the TP_RULE chain.
    modprobe br_netfilter
    sysctl net.bridge.bridge-nf-call-ip6tables=0
    sysctl net.bridge.bridge-nf-call-iptables=0
    sysctl net.bridge.bridge-nf-call-arptables=0
    iptables -t mangle -D TP_RULE -i br+ -j RETURN
    iptables -t mangle -D TP_RULE -i docker+ -j RETURN
  fi
  ;;
*)
  ;;
esac

exit 0
