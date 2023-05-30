package main

import (
	"fmt"
	"strings"

	"github.com/openshift/oauth-server/pkg/server/crypto"
)

func main() {
	var token string
	for {
		token = crypto.Random256BitsString()

		if strings.HasPrefix(token, "-") {
			continue
		}

		break
	}

	fmt.Printf("sha256~%s", token)
}
