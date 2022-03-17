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

	node := pssh.Nokia_1830PSS{
		Ip:       "127.0.0.1",
		UserName: "admin",
		Password: `admin`,
	}
	//Create the node and initiate the ssh connection.
	err := node.Connect()
	if err != nil {
		log.Fatalln(err)
	}
	defer node.Disconnect()

	//execute cli commands.
	res, err := node.Run("cli", "show slot *")
	if err != nil {
		log.Fatalln(err)
	}

	//print the result.
	fmt.Println(res)

	//execute gmre commands.
	res, err = node.Run("gmre", "show lsp")
	if err != nil {
		log.Fatalln(err)
	}

	//print the result.
	fmt.Println(res)
}
```