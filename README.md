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
		Ip:       "172.16.0.0",
		UserName: "admin",
		Password: `admin`,
	}

	//Initiate the ssh connection.
	if err := node.Connect(); err != nil {
		log.Fatalln(err)
	}

	defer node.Disconnect()

	//Execute cli commands. make sure the first parameter is "cli" or "gmre" to epecify the execution environment.
	//Commands in the same environment (cli or gmre) can be sent together as below.
	//res is a map[string]string which contains commands as key and their result as value.
	res, err := node.Run("cli", "show slot *", "show version")
	if err != nil {
		log.Fatalln(err)
	}

	//Print the result.
	fmt.Printf("%+v", res)

	//Execute gmre commands. make sure the first parameter is "gmre" to epecify the execution environment.
	//Commands in the same environment (cli or gmre) can be sent together as below.
	//res is a map[string]string which contains commands as key and their result as value.
	res, err = node.Run("gmre", "show lsp", "show interfaces", "show node", "show subnode")
	if err != nil {
		log.Fatalln(err)
	}

	//Print the result.
	fmt.Printf("%+v", res)

}
```

# Features:
```
It supports accessing Nokia 1830PSS (SWDM Portfolio) SSH cli and gmre interfaces (Port 22 for cli and gmre from within cli (tools gmre)).
```

## Enhancement plans

# Batch commands execution: (Already implemented)
```
Currently every gmre command requires gmrelogin and gmrelogout.
The plan is to pack all gmre commands, login to gmre once, execute them ,and logout.
```

# SSH Tunneling feature: 
```
50 %
```