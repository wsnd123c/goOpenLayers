package provider

import "fmt"

func reds() {
	pong, err := rdc.Redis.Ping(ctx).Result()
	fmt.Println("Ping result:", pong, "Error:", err)
}
