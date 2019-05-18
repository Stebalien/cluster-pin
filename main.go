package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	cid "github.com/ipfs/go-cid"
	ipfshttp "github.com/ipfs/go-ipfs-http-client"
	ipfsapi "github.com/ipfs/interface-go-ipfs-core"
	clusterapi "github.com/ipfs/ipfs-cluster/api"
	clusterhttp "github.com/ipfs/ipfs-cluster/api/rest/client"
	configdir "github.com/kirsle/configdir"
	peer "github.com/libp2p/go-libp2p-peer"
	pstore "github.com/libp2p/go-libp2p-peerstore"
)

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	os.Exit(1)
}

func clusterClient() (clusterhttp.Client, error) {
	type decodeConfig struct {
		clusterhttp.Config
		APIAddr, ProxyAddr clusterapi.Multiaddr
	}
	configFilePath := configdir.LocalConfig("ipfs-cluster", "client.json")
	configFile, err := os.Open(configFilePath)
	if err != nil {
		return nil, err
	}
	defer configFile.Close()

	var config decodeConfig
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		return nil, err
	}
	config.Config.APIAddr = config.APIAddr.Multiaddr
	config.Config.ProxyAddr = config.ProxyAddr.Multiaddr

	return clusterhttp.NewDefaultClient(&config.Config)
}

func connect(ctx context.Context, ipfs ipfsapi.CoreAPI, cluster clusterhttp.Client) error {
	clusterPeers, err := cluster.Peers(ctx)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	pinfos := make(map[peer.ID]*pstore.PeerInfo, len(clusterPeers))
	for _, p := range clusterPeers {
		for _, addr := range p.IPFS.Addresses {
			pii, err := pstore.InfoFromP2pAddr(addr.Multiaddr)
			if err != nil {
				return err
			}
			pi, ok := pinfos[pii.ID]
			if !ok {
				pi = &pstore.PeerInfo{ID: pii.ID}
				pinfos[pi.ID] = pi
			}
			pi.Addrs = append(pi.Addrs, pii.Addrs...)
		}
	}

	wg.Add(len(pinfos))
	for _, pi := range pinfos {
		go func(pi *pstore.PeerInfo) {
			defer wg.Done()
			err := ipfs.Swarm().Connect(ctx, *pi)
			if err != nil {
				log.Printf("failed to connect to %s: %s", pi.ID, err)
			}
		}(pi)
	}
	wg.Wait()
	return nil
}

func pin(ctx context.Context, cluster clusterhttp.Client, hash, name string) error {
	object, err := cid.Decode(hash)
	if err != nil {
		return fmt.Errorf("could not decode cid")
	}
	return cluster.Pin(ctx, object, clusterapi.PinOptions{
		Name: name,
	})
}

type pinJob struct {
	name, hash string
}

type pinResult struct {
	pinJob
	error
}

func run(args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cluster, err := clusterClient()
	if err != nil {
		return err
	}

	ipfs, err := ipfshttp.NewLocalApi()
	if err != nil {
		return err
	}

	// Connect in the background.
	connectJob := make(chan struct{})
	go func() {
		defer close(connectJob)
		err := connect(ctx, ipfs, cluster)
		if err != nil {
			log.Printf("failed to connect to cluster: %s", err)
		}
	}()

	jobs := make(chan pinJob)
	// read in jobs
	go func() {
		defer close(jobs)
		if len(args) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				pieces := strings.SplitN(scanner.Text(), " ", 2)
				hash := pieces[0]
				name := ""
				if len(pieces) == 2 {
					name = pieces[1]
				}
				select {
				case jobs <- pinJob{name: name, hash: hash}:
				case <-ctx.Done():
					return
				}
			}
		} else {
			hash := args[0]
			name := strings.Join(args[1:], " ")
			select {
			case jobs <- pinJob{name: name, hash: hash}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// process jobs
	results := make(chan pinResult)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case results <- pinResult{job, pin(ctx, cluster, job.hash, job.name)}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	// print results
	failed := false
	for r := range results {
		if r.error == nil {
			fmt.Printf("pinned: %s\t%s\n", r.hash, r.name)
		} else {
			failed = true
			fmt.Printf("failed: %s\t%s\n", r.hash, r.error)
		}
	}

	if failed {
		return fmt.Errorf("failed to pin everything")
	}
	select {
	case <-connectJob:
	default:
		fmt.Printf("waiting to finish connecting to cluster...")
		<-connectJob
	}
	return nil
}
