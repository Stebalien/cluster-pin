# IPFS Cluster Pinning Tool

A simple tool for pinning IPFS files to an IPFS cluster. This tool:

1. Connects your local IPFS node to your chosen cluster.
2. Asks the cluster to pin the specified files.

## Config

To configure, write a config file of the form:

```json
{
  "Username": "$CLUSTER_USER",
  "Password": "$CLUSTER_PASSWORD",
  "APIAddr": "$CLUSTER_MULTIADDR"
}
```

To:

* Windows: `%APPDATA%\ipfs-cluster\client.json`
* Linux: `$XDG_CONFIG_HOME/ipfs-cluster/client.json`
* MacOS: `$HOME/Library/Application Support/ipfs-cluster/client.json`

## Usage

Once you've configured cluster-pin, invoke it with a set of CIDs to pin:

```bash
> cluster-pin CID...
> echo CID... | cluster-pin
```

