package main

import (
	"fmt"
	"os"

	_ "github.com/gin-gonic/gin"
	"github.com/go-spatial/tegola/cmd/tegola/cmd"
	_ "github.com/theckman/goconstraint/go1.8/gte"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
