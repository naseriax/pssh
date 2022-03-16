# pssh
Nokia 1830PSS cli ssh wrapper in Go!

# Usage:
```
package main

import (
	"fmt"
	"log"

	//import the 1830pss cli module.
	"github.com/naseriax/pssh"
)

func main() {
    //Create the Node. 
	node := pssh.Nokia_1830PSS{
		Ip:       "192.168.10.35",
		UserName: "admin",
		Password: "admin",
		Port:     "22",
		Name:     "",
	}
	//Connect to the node's cli via ssh.
	err := node.Connect()
	if err != nil {
		log.Fatalln(err)
	}
	defer node.Disconnect()

	//execute the cli commands.
	res, err := node.Run("show slot *")
	if err != nil {
		log.Fatalln(err)
	}

	//print the result.
	fmt.Println(res)
}

```