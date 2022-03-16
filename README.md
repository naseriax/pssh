# pssh
Nokia 1830PSS cli ssh wrapper in Go!


# Usage:
```
package main

import (
	"fmt"
	"log"

	"github.com/naseriax/pssh"
)

func main() {
	//Create the Node object.
	node := pssh.Nokia_1830PSS{
		Ip:       "192.168.10.35",
		UserName: "admin",
		Password: "admin",
	}

	//Connect to the Node cli interface.
	err := node.Connect()
	if err != nil {
		log.Fatalln(err)
	}
	defer node.Disconnect()

	//Run the cli commands.
	res, err := node.Run("show slot *")
	if err != nil {
		log.Fatalln(err)
	}

	//print the result.
	fmt.Println(res)
}
```