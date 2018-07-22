// rpc-demo
package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/rpc"
)

type Val struct {
	X int32
}

func main() {
	endpoint := "/root/.ethereum/geth.ipc"

	client, err := rpc.Dial(endpoint)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	fmt.Println("connect succeed")
	var resp int32

	if err = client.Call(&resp, "mytest_getX"); err != nil {
		fmt.Println("get error")
		panic(err)
	}

	fmt.Println(resp)

	if err = client.Call(&resp, "mytest_setX", 10); err != nil {
		fmt.Println("set error")
		panic(err)
	}

	if err = client.Call(&resp, "mytest_getX"); err != nil {
		fmt.Println("get error")
		panic(err)
	}

	fmt.Println(resp)
}
