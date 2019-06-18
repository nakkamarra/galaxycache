/*
Copyright 2012 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// peers.go defines how processes find and communicate with their peers.

package groupcache

import (
	"context"
	"sync"

	"github.com/vimeo/groupcache/consistenthash"
	pb "github.com/vimeo/groupcache/groupcachepb"
)

// RemoteFetcher is the interface that must be implemented by a peer.
type RemoteFetcher interface {
	Fetch(context context.Context, in *pb.GetRequest, out *pb.GetResponse) error
}

type PeerPicker struct {
	protocol Protocol
	selfURL  string
	peers    *consistenthash.Map
	fetchers map[string]RemoteFetcher
	mu       sync.RWMutex
	opts     PeerPickerOptions

	// testing for allowing alternate PickPeer()
	pickPeerFunc func(key string, fetchers []RemoteFetcher) (RemoteFetcher, bool)
}

// PeerPickerOptions are the configurations of a PeerPicker.
type PeerPickerOptions struct {
	// BasePath specifies the HTTP path that will serve groupcache requests.
	// If blank, it defaults to "/_groupcache/".
	BasePath string

	// Replicas specifies the number of key replicas on the consistent hash.
	// If blank, it defaults to 50.
	Replicas int

	// HashFn specifies the hash function of the consistent hash.
	// If blank, it defaults to crc32.ChecksumIEEE.
	HashFn consistenthash.Hash
}

func newPeerPicker(proto Protocol, self string, options *PeerPickerOptions) *PeerPicker {
	pp := &PeerPicker{
		protocol: proto,
		selfURL:  self,
		fetchers: make(map[string]RemoteFetcher),
	}
	if options != nil {
		pp.opts = *options
	}
	if pp.opts.BasePath == "" {
		pp.opts.BasePath = defaultBasePath
	}
	if pp.opts.Replicas == 0 {
		pp.opts.Replicas = defaultReplicas
	}
	pp.peers = consistenthash.New(pp.opts.Replicas, pp.opts.HashFn)
	return pp
}

func (pp *PeerPicker) PickPeer(key string) (RemoteFetcher, bool) {
	// can define alternate pickPeerFunc for testing
	if pp.pickPeerFunc == nil {
		pp.mu.Lock()
		defer pp.mu.Unlock()
		if pp.peers.IsEmpty() {
			return nil, false
		}
		if peer := pp.peers.Get(key); peer != pp.selfURL {
			// fmt.Println("peer:", peer)
			return pp.fetchers[peer], true
		}
		return nil, false
	}
	return pp.pickPeerFunc(key, nil)
}

// Set updates the PeerPicker's list of peers.
// Each peer value should be a valid base URL,
// for example "http://example.net:8000".
func (pp *PeerPicker) Set(peers ...string) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.peers = consistenthash.New(pp.opts.Replicas, pp.opts.HashFn)
	pp.peers.Add(peers...)
	pp.fetchers = make(map[string]RemoteFetcher, len(peers))
	for _, peer := range peers {
		pp.fetchers[peer] = pp.protocol.NewFetcher(peer, pp.opts.BasePath)
	}
}

// Protocol defines the chosen connection protocol between peers (namely HTTP or GRPC) and implements the instantiation method for that connection
type Protocol interface {
	// NewFetcher instantiates the connection between peers and returns a RemoteFetcher to be used for fetching from a peer
	NewFetcher(url string, basePath string) RemoteFetcher
}
