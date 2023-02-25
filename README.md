# Usage example:
```
package main

import (
	"fmt"
	"log"

	"github.com/naseriax/pssh"
)

func main() {

	nodeIP := "172.16.0.1"
	log.Printf("connecting to %v", nodeIP)

	// create the node.
	node := pssh.Endpoint{
		Ip:       nodeIP,
		UserName: "admin",
		Password: "admin",
		Port:     "22",
		Kind:     "GMRE", //Accepted values: BASH, PSS, OSE, PSD, GMRE
	}


    // initiate the ssh connection.
	if err := node.Connect(); err != nil {
		log.Println(err)
		return
	}

	// set the logout clause.
	defer func(node pssh.Endpoint) { node.Disconnect(); log.Printf("Closed ssh session for %v", node.Ip) }(node)

    //res is a map[string]string which contains commands as key and their results as value.
	res, err := node.Run(
		"show lsp",
		"show node",
	)

	if err != nil {
		fmt.Println(err)
	}

	//Print the result.
	fmt.Println(node.Ip, res)
}
```
# Features:
```
It supports connecting to 1830PSS cli (Kind = PSS),1830PSS ose (Kind = OSE), 1830PSS gmre (Kind = GMRE), Linux shell (Kind = BASH) and 1830PSD cli (Kind = PSD).
```