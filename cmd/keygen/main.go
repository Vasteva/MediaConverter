package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Vasteva/MediaConverter/internal/license"
)

func main() {
	userID := "ADMIN"
	if len(os.Args) > 1 {
		userID = strings.Join(os.Args[1:], "")
	}

	key := license.Generate(userID)
	fmt.Println("Generated License Key:")
	fmt.Println(key)
}
