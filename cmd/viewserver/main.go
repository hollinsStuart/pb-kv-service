package main

import (
	"flag"
	"fmt"

	"github.com/hollinsStuart/pb-kv-service/viewservice"
)

func main() {
	// Define the port flag, defaulting to 50000
	port := flag.String("port", ":50000", "Port for ViewService to listen on (e.g., :50000)")
	flag.Parse()

	// Call the StartServer function from your server_impl.go
	viewservice.StartServer(*port)

	fmt.Printf("ViewService is running. Press Ctrl+C to exit.\n")

	// Keep the main thread alive indefinitely so the server goroutine can run
	select {}
}
