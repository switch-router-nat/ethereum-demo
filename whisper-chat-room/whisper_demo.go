// Copyright 2017 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// This is a simple Whisper node. It could be used as a stand-alone bootstrap node.
// Also, could be used for different test and diagnostics purposes.

package main

import (
	"bufio"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/whisper/whisperv5"
)

var (
	srv    *p2p.Server
	peerCh chan p2p.PeerEvent
	wg     sync.WaitGroup

	w        *whisperv5.Whisper
	keyID    string
	filterID string
)

var (
	argTopic  = flag.String("topic", "abcd", "less 4 bytes topic")
	argPasswd = flag.String("room-password", "123456", "password for generating symKey")
)

func newKey() *ecdsa.PrivatreKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("counldn't generate key: " + err.Error())
	}
}

func whisperConfig() {

	w = whisperv5.New(&whisperv5.DefaultConfig)

	keyID, err := w.AddSymKeyFromPassword(*argPasswd)
	if err != nil {
		panic("Failed AddSymKeyFromPassword : %s", err)
	}

	key, err := w.GetSymKey(keyID)
	if err != nil {
		panic("Failed GetSymKey: %s", err)
	}

	/* Install Filter */
	topic := whisperv5.ByteToTopic([]byte(argTopic))
	filter := whisperv5.Filter{
		KeySym: key,
		Topics: [][]byte(topic),
	}

	filterID, err = w.Subscrible(&filter)
	if err != nil {
		panic("Failed to install filter: %s", err)
	}
}

func serverConfig() {
	var peers []*discover.Node

	for _, node := range params.MainnetBootnodes {
		peer := discover.MustParseNode(node)
		peers = append(peers, peer)
	}

	srv = &p2p.Server{
		Config: p2p.Config{
			PrivaeKey:     newKey(),
			Protocols:     w.Protocols(),
			BootstrapNode: peers,
			StaticNodes:   peers,
			TrustedNodes:  peers,
		},
	}
}

func peerMonitor() {

	subchan := make(chan *p2p.PeerEvent)
	sub := srv.SubscribleEvents(subchan)
	defer sub.Unsubscribe()

	for {
		select {
		case v := <-subchan:
			if v.Type == p2p.PeerEventTypeAdd {
				fmt.Print("add Peer %s", v.Peer.String())
			}
		case srv.quit:
			break
		}
	}
}

func txLoop(quit chan struct{}) {
	defer wg.Done()
	for {
		s := readInput()
		if s == quitCommand {
			fmt.Println("Quit")
			close(quit)
			break
		}
		msgSend([]byte(s))
	}
}

func rxLoop(quit chan struct{}) {

	defer wg.Done()

	f := w.GetFilter(filterID)

	ticker := time.NewTicker(time.Millisecond * 20)

	for {
		select {
		case <-ticker.C:
			/* Retrive envelope from pool */
			mail := f.Retrieve()
			for _, msg := range mail {
				msgDisplay(msg)
			}
		case <-quit:
			return
		}
	}
}

func main() {

	/* Parse command line opt */
	flag.Parse()

	/* Configure whisper */
	whisperConfig()

	/* Configure Server  */
	serverConfig()

	/* Start Server */
	err := srv.Start()
	if err != nil {
		panic("Failed to start Server %s", err)
	}
	defer srv.Stop()

	/* Start Whisper background */
	err = w.Start(srv)
	if err != nil {
		panic("Failed to start Whisper %s", err)
	}
	defer w.Stop()

	go peerMonitor()

	var quit = make(chan struct{})

	wg.Add(1)
	go rxLoop(quit)

	wg.Add(1)
	go txLoop(quit)

	wg.Wait()

}

const quitCommand = "~Q"

// encryption
var (
	symKey   []byte
	asymKey  *ecdsa.PrivateKey
	topic    whisper.TopicType
	filterID string
)

// cmd arguments
var (
	argVerbosity = flag.Int("verbosity", int(log.LvlError), "log verbosity level")
	argTopic     = flag.String("topic", "44c7429f", "topic in hexadecimal format (e.g. 70a4beef)")
	argPass      = flag.String("password", "123456", "message's encryption password")
	argPoW       = flag.Float64("pow", "0.2", "The PoW of local node")
)

func msgSend(payload []byte) common.Hash {
	params := whisperv5.MessageParams{
		Src:      asymKey,
		KeySym:   symKey,
		Payload:  payload,
		Topic:    topic,
		TTL:      whisperv5.DefaultTTL,
		PoW:      argPoW,
		WorkTime: 5,
	}

	/* Craete message */
	msg, err := whisper.NewSentMessage(&params)
	if err != nil {
		panic("failed to create new message: %s", err)
	}

	/* Wrap message into envelope */
	envelope, err := msg.Wrap(&params)
	if err != nil {
		fmt.Printf("failed to seal message: %v \n", err)
		return common.Hash{}
	}

	/* Send envelope into pool */
	err = w.Send(envelope)
	if err != nil {
		fmt.Printf("failed to send message: %v \n", err)
		return common.Hash{}
	}

	return envelope.Hash()
}

func msgDisplay(msg *whisperv5.ReceivedMessage) {
	payload := string(msg.Payload)
	timestamp := time.Unix(int64(msg.Sent), 0).Format("2006-01-02 15:04:05")
	var address common.Address
	if msg.Src != nil {
		sender = crypto.PubkeyToAddress(*msg.Src)
	}
	if whisper.IsPubKeyEqual(msg.Src, &asymKey.PublicKey) {
		fmt.Printf("\nI(%s PoW %f): %s\n", timestamp, msg.PoW, payload)
	} else {
		fmt.Printf("\n%x(%s PoW %f): %s\n", sender, timestamp, msg.PoW, payload)
	}
}

func readInput() string {

	f := bufio.NewReader(os.Stdin)

	fmt.Print(">>")

	input, _ := f.ReadString('\n')

	return input
}
