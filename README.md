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
    //Create the node.
	node := pssh.Nokia_1830PSS{
		Ip:       "172.16.0.1",
		UserName: "admin",
		Password: `admin`,
	}

	//Initiate the ssh connection.
	if err := node.Connect();err != nil {
		log.Fatalln(err)
	}

	defer node.Disconnect()

	//Execute cli commands.
	res, err := node.Run("cli", "show slot *")
	if err != nil {
		log.Fatalln(err)
	}

	//Print the result.
	fmt.Println(res)

	//Execute gmre commands.
	res, err = node.Run("gmre", "show lsp")
	if err != nil {
		log.Fatalln(err)
	}

	//Print the result.
	fmt.Println(res)
}
```

# Features:
```
It supports accessing Nokia 1830PSS (SWDM Portfolio) SSH cli and gmre interfaces (Port 22 for cli and gmre from within cli (tools gmre)).
```

# Enhancement plans:
```
Currently every gmre command requires gmrelogin and gmrelogout.
The plan is to pack all gmre commands, login to gmre once, execute them ,and logout.
```