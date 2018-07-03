
package main

import (
	"bufio"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"
        "log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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
        hasPeerConnected bool
        asymKey  *ecdsa.PrivateKey

	w        *whisperv5.Whisper
        topic    whisperv5.TopicType
	keyID    string
	filterID string
)

var (
	argTopic  = flag.String("topic", "abcd", "less 4 bytes topic")
	argPasswd = flag.String("room-password", "123456", "password for generating symKey")
        argPoW    = flag.Float64("pow",0.2,"The PoW of local node")

)

func newKey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		log.Panic("counldn't generate key: " + err.Error())
	}

        return key
}

func whisperConfig() {

	w = whisperv5.New(&whisperv5.DefaultConfig)

	keyID, err := w.AddSymKeyFromPassword(*argPasswd)
	if err != nil {
		log.Panic("Failed AddSymKeyFromPassword : %s", err)
	}

	key, err := w.GetSymKey(keyID)
	if err != nil {
		log.Panic("Failed GetSymKey: %s", err)
	}

	/* Install Filter */
	topic = whisperv5.BytesToTopic([]byte(*argTopic))
	filter := whisperv5.Filter{
		KeySym: key,
		Topics: [][]byte{topic[:]},
	}

	filterID, err = w.Subscribe(&filter)
	if err != nil {
		log.Panic("Failed to install filter: %s", err)
	}
}

func serverConfig() {
	var peers []*discover.Node

	for _, node := range params.MainnetBootnodes {
		peer := discover.MustParseNode(node)
		peers = append(peers, peer)
	}
        asymKey =  newKey()
	srv = &p2p.Server{
		Config: p2p.Config{
			PrivateKey:    asymKey,
			Protocols:     w.Protocols(),
			StaticNodes:   peers,
                        BootstrapNodes: peers,
                        TrustedNodes:  peers,
                        NAT: nat.Any(),
		},
	}
}

func peerMonitor() {

	subchan := make(chan *p2p.PeerEvent)
	sub := srv.SubscribeEvents(subchan)
	defer sub.Unsubscribe()
        
        ticker := time.NewTicker(time.Second)
	for {
		select {
		case v := <-subchan:
			if v.Type == p2p.PeerEventTypeAdd {
				fmt.Print("add Peer %s", v.Peer.String())
			} else if v.Type == p2p.PeerEventTypeDrop{
                                fmt.Print("drop Peer %s", v.Peer.String())
                        }

                        if srv.PeerCount() > 0 {
                            hasPeerConnected = true
                        }else{
                            hasPeerConnected = false
                        }
               case <-ticker.C:
                        fmt.Print("Peer count %d\n", srv.PeerCount())
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
		}else if (len(s) == 0){
                     continue 
                }
                
                if hasPeerConnected{
		    msgSend([]byte(s))
                }
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
		log.Panic("Failed to start Server %s", err)
	}
	defer srv.Stop()
	fmt.Println("Server Start...\n")

	/* Start Whisper background */
	err = w.Start(srv)
	if err != nil {
		log.Panic("Failed to start Whisper %s", err)
	}
	defer w.Stop()

	fmt.Println("Whisper Start...\n")
	
        go peerMonitor()

	var quit = make(chan struct{})

	wg.Add(1)
	go rxLoop(quit)

	wg.Add(1)
	go txLoop(quit)

	wg.Wait()

}

const quitCommand = "~Q"


func msgSend(payload []byte) common.Hash {
	key,err := w.GetSymKey(keyID)
        if err != nil {
             log.Panic("GetSymKey failed in msgSend",err)
        }
        params := whisperv5.MessageParams{
		Src:      asymKey,
		KeySym:   key,
		Payload:  payload,
		Topic:    topic,
		TTL:      whisperv5.DefaultTTL,
		PoW:      *argPoW,
		WorkTime: 5,
	}

	/* Craete message */
	msg, err := whisperv5.NewSentMessage(&params)
	if err != nil {
		log.Panic("failed to create new message: %s", err)
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
	var sender common.Address
	if msg.Src != nil {
		sender = crypto.PubkeyToAddress(*msg.Src)
	}
	if whisperv5.IsPubKeyEqual(msg.Src, &asymKey.PublicKey) {
		fmt.Printf("\nI(%s PoW %f): %s\n", timestamp, msg.PoW, payload)
	} else {
		fmt.Printf("\n%x(%s PoW %f): %s\n", sender, timestamp, msg.PoW, payload)
	}
}

func readInput() string {

        if !hasPeerConnected {
             fmt.Print(">>[nopeer]")
        }else {
             fmt.Print(">>")
        }

	f := bufio.NewReader(os.Stdin)

	input, _ := f.ReadString('\n')

	return input
}
